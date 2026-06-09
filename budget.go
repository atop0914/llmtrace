package llmtrace

import (
	"context"
	"sync"
	"time"
)

// BudgetWindow defines the time window for budget tracking and reset.
type BudgetWindow int

const (
	// BudgetWindowDaily resets spending at midnight UTC.
	BudgetWindowDaily BudgetWindow = iota
	// BudgetWindowWeekly resets spending every Monday at midnight UTC.
	BudgetWindowWeekly
	// BudgetWindowMonthly resets spending on the 1st of each month at midnight UTC.
	BudgetWindowMonthly
	// BudgetWindowTotal never resets — lifetime spending limit.
	BudgetWindowTotal
)

// String returns the human-readable name of the budget window.
func (w BudgetWindow) String() string {
	switch w {
	case BudgetWindowDaily:
		return "daily"
	case BudgetWindowWeekly:
		return "weekly"
	case BudgetWindowMonthly:
		return "monthly"
	case BudgetWindowTotal:
		return "total"
	default:
		return "unknown"
	}
}

// Budget defines a spending limit with a time window and optional scope.
type Budget struct {
	// Name is a unique identifier for this budget.
	Name string

	// Amount is the maximum spending in USD for the window.
	Amount float64

	// Window is the time period after which spending resets.
	Window BudgetWindow

	// Provider filters spending to a specific provider (empty = all providers).
	Provider string

	// Model filters spending to a specific model (empty = all models).
	Model string
}

// AlertSeverity indicates how critical the budget alert is.
type AlertSeverity int

const (
	// AlertWarning fires when spending crosses a threshold below the limit.
	AlertWarning AlertSeverity = iota
	// AlertCritical fires when spending is very close to or at the limit.
	AlertCritical
	// AlertExceeded fires when spending exceeds the budget.
	AlertExceeded
)

// String returns the human-readable severity level.
func (s AlertSeverity) String() string {
	switch s {
	case AlertWarning:
		return "warning"
	case AlertCritical:
		return "critical"
	case AlertExceeded:
		return "exceeded"
	default:
		return "unknown"
	}
}

// Alert represents a budget alert event.
type Alert struct {
	// Budget is the budget that triggered this alert.
	Budget Budget

	// Spent is the current total spending in the window.
	Spent float64

	// Remaining is Amount - Spent (can be negative if exceeded).
	Remaining float64

	// Percent is the spending as a percentage of the budget (0-100+).
	Percent float64

	// Severity indicates the alert level.
	Severity AlertSeverity

	// WindowStart is when the current tracking window started.
	WindowStart time.Time

	// Timestamp is when the alert was generated.
	Timestamp time.Time
}

// AlertThreshold defines a spending percentage that triggers an alert callback.
type AlertThreshold struct {
	// Percent is the spending percentage (0-100) that triggers the alert.
	// Values above 100 are valid and trigger on exceed.
	Percent float64

	// Callback is called when the threshold is crossed.
	// Must be safe for concurrent use.
	Callback func(Alert)
}

// BudgetTracker tracks spending against budgets and fires alerts.
// It is safe for concurrent use.
type BudgetTracker struct {
	mu      sync.RWMutex
	budgets []*trackedBudget
	clock   func() time.Time // for testing; defaults to time.Now
}

type trackedBudget struct {
	mu          sync.Mutex
	budget      Budget
	thresholds  []AlertThreshold
	amount      float64
	windowStart time.Time
	firedAlert  map[float64]bool // percent -> already fired in this window
	clock       func() time.Time
}

// NewBudgetTracker creates a new BudgetTracker.
func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		clock: time.Now,
	}
}

// AddBudget adds a budget with alert thresholds.
// Returns the tracker for chaining.
func (bt *BudgetTracker) AddBudget(budget Budget, thresholds ...AlertThreshold) *BudgetTracker {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := bt.clock()
	tb := &trackedBudget{
		budget:      budget,
		thresholds:  thresholds,
		windowStart: budgetWindowStart(budget.Window, now),
		firedAlert:  make(map[float64]bool),
		clock:       bt.clock,
	}
	bt.budgets = append(bt.budgets, tb)
	return bt
}

// Record records a cost event and checks all matching budgets for alerts.
// This is typically called from middleware after computing the cost.
func (bt *BudgetTracker) Record(provider, model string, cost float64) []Alert {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	var alerts []Alert
	for _, tb := range bt.budgets {
		if !tb.matches(provider, model) {
			continue
		}
		alerts = append(alerts, tb.record(cost)...)
	}
	return alerts
}

// Snapshot returns the current spending status for all budgets.
func (bt *BudgetTracker) Snapshot() []BudgetStatus {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	statuses := make([]BudgetStatus, 0, len(bt.budgets))
	for _, tb := range bt.budgets {
		tb.mu.Lock()
		tb.checkReset()
		statuses = append(statuses, BudgetStatus{
			Budget:      tb.budget,
			Spent:       tb.amount,
			Remaining:   tb.budget.Amount - tb.amount,
			Percent:     percentOf(tb.amount, tb.budget.Amount),
			WindowStart: tb.windowStart,
		})
		tb.mu.Unlock()
	}
	return statuses
}

// BudgetStatus is the current state of a single budget.
type BudgetStatus struct {
	Budget      Budget
	Spent       float64
	Remaining   float64
	Percent     float64
	WindowStart time.Time
}

// matches checks if a provider/model pair matches the budget scope.
func (tb *trackedBudget) matches(provider, model string) bool {
	if tb.budget.Provider != "" && tb.budget.Provider != provider {
		return false
	}
	if tb.budget.Model != "" && tb.budget.Model != model {
		return false
	}
	return true
}

// record adds cost and checks for alert threshold crossings.
// Caller must hold bt.mu (read lock is sufficient for matches,
// but trackedBudget.mu handles its own state).
func (tb *trackedBudget) record(cost float64) []Alert {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.checkReset()
	tb.amount += cost
	pct := percentOf(tb.amount, tb.budget.Amount)

	var alerts []Alert
	for _, threshold := range tb.thresholds {
		if pct >= threshold.Percent && !tb.firedAlert[threshold.Percent] {
			tb.firedAlert[threshold.Percent] = true

			alert := Alert{
				Budget:      tb.budget,
				Spent:       tb.amount,
				Remaining:   tb.budget.Amount - tb.amount,
				Percent:     pct,
				Severity:    classifySeverity(pct),
				WindowStart: tb.windowStart,
				Timestamp:   tb.clock(),
			}
			alerts = append(alerts, alert)

			if threshold.Callback != nil {
				threshold.Callback(alert)
			}
		}
	}

	return alerts
}

// checkReset resets spending if the window has elapsed.
// Caller must hold tb.mu.
func (tb *trackedBudget) checkReset() {
	now := tb.clock()
	newStart := budgetWindowStart(tb.budget.Window, now)
	if newStart.After(tb.windowStart) {
		tb.amount = 0
		tb.windowStart = newStart
		// Clear fired alerts for the new window
		for k := range tb.firedAlert {
			delete(tb.firedAlert, k)
		}
	}
}

// budgetWindowStart calculates the start of the window containing t.
func budgetWindowStart(window BudgetWindow, t time.Time) time.Time {
	switch window {
	case BudgetWindowDaily:
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	case BudgetWindowWeekly:
		y, m, d := t.Date()
		weekday := t.Weekday()
		// (weekday + 6) % 7 gives days since Monday:
		// Mon=0, Tue=1, ..., Sat=5, Sun=6
		daysSinceMonday := (int(weekday) + 6) % 7
		monday := time.Date(y, m, d-daysSinceMonday, 0, 0, 0, 0, time.UTC)
		return monday
	case BudgetWindowMonthly:
		y, m, _ := t.Date()
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	case BudgetWindowTotal:
		return time.Time{} // epoch — never resets
	default:
		return time.Time{}
	}
}

// Reset resets spending for all budgets (useful for testing).
func (bt *BudgetTracker) Reset() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	now := bt.clock()
	for _, tb := range bt.budgets {
		tb.mu.Lock()
		tb.amount = 0
		tb.windowStart = budgetWindowStart(tb.budget.Window, now)
		for k := range tb.firedAlert {
			delete(tb.firedAlert, k)
		}
		tb.mu.Unlock()
	}
}

// percentOf computes the percentage of spent vs budget.
func percentOf(spent, budget float64) float64 {
	if budget <= 0 {
		return 0
	}
	return (spent / budget) * 100
}

// classifySeverity maps a spending percentage to an alert severity.
func classifySeverity(pct float64) AlertSeverity {
	switch {
	case pct >= 100:
		return AlertExceeded
	case pct >= 90:
		return AlertCritical
	default:
		return AlertWarning
	}
}

// BudgetMiddleware returns a Middleware that tracks costs against budgets.
// It uses the CostCalculator to compute the cost of each request and records
// it with the BudgetTracker.
func BudgetMiddleware(tracker *BudgetTracker, calc *CostCalculator) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			resp, err := next(ctx, req)
			if err == nil && resp != nil {
				cost := calc.Calculate(resp.Model, resp.Usage)
				if cost > 0 {
					tracker.Record(providerFromResponse(ctx, resp), resp.Model, cost)
				}
			}
			return resp, err
		}
	}
}

// StreamBudgetMiddleware returns a StreamMiddleware that tracks costs against budgets.
func StreamBudgetMiddleware(tracker *BudgetTracker, calc *CostCalculator) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			ch, err := next(ctx, req)
			if err != nil {
				return nil, err
			}

			out := make(chan StreamChunk)
			go func() {
				defer close(out)
				var lastUsage *Usage
				for chunk := range ch {
					if chunk.Usage != nil {
						lastUsage = chunk.Usage
					}
					out <- chunk
				}
				if lastUsage != nil {
					cost := calc.Calculate(req.Model, *lastUsage)
					if cost > 0 {
						tracker.Record(providerFromContext(ctx), req.Model, cost)
					}
				}
			}()

			return out, nil
		}
	}
}

// providerFromResponse extracts provider from response or context.
func providerFromResponse(ctx context.Context, resp *Response) string {
	if resp != nil && resp.Provider != "" {
		return resp.Provider
	}
	return providerFromContext(ctx)
}

// budgetProviderKey is the context key for provider name in the root package.
type budgetProviderKey struct{}

// BudgetContextWithProvider returns a new context with the provider name,
// usable by BudgetMiddleware and StreamBudgetMiddleware.
func BudgetContextWithProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, budgetProviderKey{}, provider)
}

// providerFromContext extracts provider name from context.
func providerFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(budgetProviderKey{}).(string); ok {
		return v
	}
	return "unknown"
}
