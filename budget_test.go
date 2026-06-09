package llmtrace

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- BudgetWindow String ---

func TestBudgetWindowString(t *testing.T) {
	tests := []struct {
		w    BudgetWindow
		want string
	}{
		{BudgetWindowDaily, "daily"},
		{BudgetWindowWeekly, "weekly"},
		{BudgetWindowMonthly, "monthly"},
		{BudgetWindowTotal, "total"},
		{BudgetWindow(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.w.String(); got != tt.want {
			t.Errorf("BudgetWindow(%d).String() = %q, want %q", tt.w, got, tt.want)
		}
	}
}

// --- AlertSeverity String ---

func TestAlertSeverityString(t *testing.T) {
	tests := []struct {
		s    AlertSeverity
		want string
	}{
		{AlertWarning, "warning"},
		{AlertCritical, "critical"},
		{AlertExceeded, "exceeded"},
		{AlertSeverity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("AlertSeverity(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// --- BudgetWindowStart ---

func TestBudgetWindowStart(t *testing.T) {
	// A Wednesday: 2026-06-10
	wed := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)

	// Daily: midnight of that day
	daily := budgetWindowStart(BudgetWindowDaily, wed)
	wantDaily := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	if !daily.Equal(wantDaily) {
		t.Errorf("daily window start = %v, want %v", daily, wantDaily)
	}

	// Weekly: Monday of that week (June 8)
	weekly := budgetWindowStart(BudgetWindowWeekly, wed)
	wantWeekly := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	if !weekly.Equal(wantWeekly) {
		t.Errorf("weekly window start = %v, want %v", weekly, wantWeekly)
	}

	// Monthly: June 1
	monthly := budgetWindowStart(BudgetWindowMonthly, wed)
	wantMonthly := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !monthly.Equal(wantMonthly) {
		t.Errorf("monthly window start = %v, want %v", monthly, wantMonthly)
	}

	// Total: epoch
	total := budgetWindowStart(BudgetWindowTotal, wed)
	if !total.Equal(time.Time{}) {
		t.Errorf("total window start = %v, want epoch", total)
	}

	// Weekly: Sunday should give previous Monday
	sun := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	weeklySun := budgetWindowStart(BudgetWindowWeekly, sun)
	wantSunMonday := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	if !weeklySun.Equal(wantSunMonday) {
		t.Errorf("weekly window start for Sunday = %v, want %v", weeklySun, wantSunMonday)
	}

	// Weekly: Monday itself
	mon := time.Date(2026, 6, 8, 5, 0, 0, 0, time.UTC)
	weeklyMon := budgetWindowStart(BudgetWindowWeekly, mon)
	if !weeklyMon.Equal(wantSunMonday) {
		t.Errorf("weekly window start for Monday = %v, want %v", weeklyMon, wantSunMonday)
	}
}

// --- Basic Tracker ---

func TestBudgetTrackerBasic(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	tracker.AddBudget(Budget{
		Name:   "daily-limit",
		Amount: 10.0,
		Window: BudgetWindowDaily,
	})

	// Record some spending
	tracker.Record("openai", "gpt-4o", 3.0)
	tracker.Record("openai", "gpt-4o", 2.0)

	statuses := tracker.Snapshot()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Spent != 5.0 {
		t.Errorf("spent = %f, want 5.0", s.Spent)
	}
	if s.Remaining != 5.0 {
		t.Errorf("remaining = %f, want 5.0", s.Remaining)
	}
	if s.Percent != 50.0 {
		t.Errorf("percent = %f, want 50.0", s.Percent)
	}
	if s.Budget.Name != "daily-limit" {
		t.Errorf("budget name = %q, want %q", s.Budget.Name, "daily-limit")
	}
}

// --- Threshold Alerts ---

func TestBudgetTrackerThreshold(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	var alerted atomic.Bool
	tracker.AddBudget(
		Budget{
			Name:   "test",
			Amount: 100.0,
			Window: BudgetWindowDaily,
		},
		AlertThreshold{
			Percent: 80,
			Callback: func(a Alert) {
				alerted.Store(true)
			},
		},
	)

	// Spend 79 — below threshold
	tracker.Record("openai", "gpt-4o", 79.0)
	if alerted.Load() {
		t.Error("alert fired prematurely at 79%")
	}

	// Spend 2 more to cross 80%
	alerts := tracker.Record("openai", "gpt-4o", 2.0)
	if !alerted.Load() {
		t.Error("alert should have fired at 81%")
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Severity != AlertWarning {
		t.Errorf("severity = %v, want warning", a.Severity)
	}
	if a.Spent != 81.0 {
		t.Errorf("spent = %f, want 81.0", a.Spent)
	}
	if a.Percent != 81.0 {
		t.Errorf("percent = %f, want 81.0", a.Percent)
	}
	if a.Budget.Name != "test" {
		t.Errorf("budget name = %q, want %q", a.Budget.Name, "test")
	}

	// Recording again should NOT re-fire (already fired for 80%)
	alerts2 := tracker.Record("openai", "gpt-4o", 5.0)
	if len(alerts2) != 0 {
		t.Errorf("expected no new alerts, got %d", len(alerts2))
	}
}

// --- Multiple Thresholds ---

func TestBudgetTrackerMultipleThresholds(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	var alerts []Alert
	tracker.AddBudget(
		Budget{Name: "multi", Amount: 100.0, Window: BudgetWindowDaily},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alerts = append(alerts, a) }},
		AlertThreshold{Percent: 80, Callback: func(a Alert) { alerts = append(alerts, a) }},
		AlertThreshold{Percent: 100, Callback: func(a Alert) { alerts = append(alerts, a) }},
	)

	// Cross 50%
	tracker.Record("openai", "gpt-4o", 55.0)
	if len(alerts) != 1 || alerts[0].Severity != AlertWarning {
		t.Errorf("expected 1 warning alert, got %d alerts", len(alerts))
	}

	// Cross 80%
	tracker.Record("openai", "gpt-4o", 30.0)
	if len(alerts) != 2 {
		t.Errorf("expected 2 alerts, got %d", len(alerts))
	}
	if alerts[1].Severity != AlertWarning { // 85% is still warning (<90)
		t.Errorf("second alert severity = %v, want warning", alerts[1].Severity)
	}

	// Cross 100%
	tracker.Record("openai", "gpt-4o", 20.0)
	if len(alerts) != 3 {
		t.Errorf("expected 3 alerts, got %d", len(alerts))
	}
	if alerts[2].Severity != AlertExceeded {
		t.Errorf("third alert severity = %v, want exceeded", alerts[2].Severity)
	}
	if alerts[2].Remaining != -5.0 {
		t.Errorf("remaining = %f, want -5.0", alerts[2].Remaining)
	}
}

// --- Window Reset ---

func TestBudgetTrackerWindowReset(t *testing.T) {
	day1 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	mu := sync.Mutex{}
	currentTime := day1

	tracker := &BudgetTracker{
		clock: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		},
	}

	var alertCount int
	tracker.AddBudget(
		Budget{Name: "daily", Amount: 100.0, Window: BudgetWindowDaily},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alertCount++ }},
	)

	// Day 1: spend 60 -> triggers alert
	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 1 {
		t.Fatalf("expected 1 alert, got %d", alertCount)
	}

	// Day 2: window resets
	day2 := time.Date(2026, 6, 11, 1, 0, 0, 0, time.UTC)
	mu.Lock()
	currentTime = day2
	mu.Unlock()

	// Spend 60 again — should trigger new alert (window reset)
	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 2 {
		t.Errorf("expected 2 alerts after window reset, got %d", alertCount)
	}

	// Verify snapshot shows only day 2 spending
	statuses := tracker.Snapshot()
	if statuses[0].Spent != 60.0 {
		t.Errorf("after reset, spent = %f, want 60.0", statuses[0].Spent)
	}
}

// --- Scope Filtering ---

func TestBudgetTrackerScope(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	// Provider-scoped budget
	tracker.AddBudget(Budget{
		Name:     "openai-only",
		Amount:   100.0,
		Window:   BudgetWindowDaily,
		Provider: "openai",
	})

	// Model-scoped budget
	tracker.AddBudget(Budget{
		Name:   "gpt4-budget",
		Amount: 50.0,
		Window: BudgetWindowDaily,
		Model:  "gpt-4",
	})

	// Record spending for different providers/models
	tracker.Record("openai", "gpt-4o", 30.0)
	tracker.Record("anthropic", "claude-3", 999.0) // Should not affect either budget
	tracker.Record("openai", "gpt-4", 20.0)

	statuses := tracker.Snapshot()

	// openai-only should have 50 (30 + 20)
	if statuses[0].Spent != 50.0 {
		t.Errorf("openai-only spent = %f, want 50.0", statuses[0].Spent)
	}

	// gpt4-budget should have 20 (only gpt-4 model)
	if statuses[1].Spent != 20.0 {
		t.Errorf("gpt4-budget spent = %f, want 20.0", statuses[1].Spent)
	}
}

// --- Severity Classification ---

func TestClassifySeverity(t *testing.T) {
	tests := []struct {
		pct  float64
		want AlertSeverity
	}{
		{0, AlertWarning},
		{50, AlertWarning},
		{89.9, AlertWarning},
		{90, AlertCritical},
		{95, AlertCritical},
		{99.9, AlertCritical},
		{100, AlertExceeded},
		{150, AlertExceeded},
	}
	for _, tt := range tests {
		got := classifySeverity(tt.pct)
		if got != tt.want {
			t.Errorf("classifySeverity(%f) = %v, want %v", tt.pct, got, tt.want)
		}
	}
}

// --- PercentOf ---

func TestPercentOf(t *testing.T) {
	tests := []struct {
		spent, budget, want float64
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{150, 100, 150},
		{0, 0, 0},       // zero budget
		{50, 0, 0},      // zero budget with spending
	}
	for _, tt := range tests {
		got := percentOf(tt.spent, tt.budget)
		if diff := got - tt.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("percentOf(%f, %f) = %f, want %f", tt.spent, tt.budget, got, tt.want)
		}
	}
}

// --- Reset ---

func TestBudgetTrackerManualReset(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	var alertCount int
	tracker.AddBudget(
		Budget{Name: "test", Amount: 100.0, Window: BudgetWindowDaily},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alertCount++ }},
	)

	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 1 {
		t.Fatalf("expected 1 alert, got %d", alertCount)
	}

	tracker.Reset()

	// After reset, spending 60 should trigger alert again
	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 2 {
		t.Errorf("expected 2 alerts after reset, got %d", alertCount)
	}

	statuses := tracker.Snapshot()
	if statuses[0].Spent != 60.0 {
		t.Errorf("after reset and re-spend, spent = %f, want 60.0", statuses[0].Spent)
	}
}

// --- AddBudget Chaining ---

func TestBudgetTrackerChaining(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.
		AddBudget(Budget{Name: "a", Amount: 100, Window: BudgetWindowDaily}).
		AddBudget(Budget{Name: "b", Amount: 200, Window: BudgetWindowWeekly}).
		AddBudget(Budget{Name: "c", Amount: 300, Window: BudgetWindowMonthly})

	if len(tracker.budgets) != 3 {
		t.Errorf("expected 3 budgets, got %d", len(tracker.budgets))
	}
}

// --- BudgetMiddleware ---

func TestBudgetMiddleware(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	var exceeded atomic.Bool
	tracker.AddBudget(
		Budget{Name: "daily", Amount: 0.10, Window: BudgetWindowDaily},
		AlertThreshold{Percent: 100, Callback: func(a Alert) {
			exceeded.Store(true)
		}},
	)

	calc := NewCostCalculator()
	mw := BudgetMiddleware(tracker, calc)

	// Simulate a call that costs money
	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:    "gpt-4o",
			Provider: "openai",
			Usage: Usage{
				InputTokens:  10000,
				OutputTokens: 5000,
				TotalTokens:  15000,
			},
		}, nil
	}

	wrapped := mw(inner)
	resp, err := wrapped(context.Background(), &Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// gpt-4o: 10000 * 0.0025/1000 + 5000 * 0.01/1000 = 0.025 + 0.05 = 0.075
	expectedCost := 0.075
	_ = resp

	statuses := tracker.Snapshot()
	if diff := statuses[0].Spent - expectedCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("middleware recorded cost = %f, want %f", statuses[0].Spent, expectedCost)
	}
	if exceeded.Load() {
		t.Error("should not have exceeded $0.10 budget yet")
	}

	// Second call should exceed
	wrapped(context.Background(), &Request{Model: "gpt-4o"})
	if !exceeded.Load() {
		t.Error("should have exceeded budget after 2 calls")
	}
}

// --- StreamBudgetMiddleware ---

func TestStreamBudgetMiddleware(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}
	tracker.AddBudget(Budget{Name: "stream", Amount: 100.0, Window: BudgetWindowDaily})

	calc := NewCostCalculator()
	mw := StreamBudgetMiddleware(tracker, calc)

	inner := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 3)
		ch <- StreamChunk{Content: "Hello"}
		ch <- StreamChunk{Content: " world"}
		ch <- StreamChunk{
			Content: "!",
			Usage: &Usage{
				InputTokens:  5000,
				OutputTokens: 2000,
				TotalTokens:  7000,
			},
		}
		close(ch)
		return ch, nil
	}

	wrapped := mw(inner)
	ch, err := wrapped(context.Background(), &Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	// Give goroutine time to finish
	time.Sleep(20 * time.Millisecond)

	// gpt-4o: 5000 * 0.0025/1000 + 2000 * 0.01/1000 = 0.0125 + 0.02 = 0.0325
	statuses := tracker.Snapshot()
	expectedCost := 0.0325
	if diff := statuses[0].Spent - expectedCost; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("stream middleware recorded cost = %f, want %f", statuses[0].Spent, expectedCost)
	}
}

// --- Concurrent Access ---

func TestBudgetTrackerConcurrency(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.AddBudget(
		Budget{Name: "concurrent", Amount: 10000.0, Window: BudgetWindowDaily},
		AlertThreshold{Percent: 50, Callback: func(a Alert) {}},
	)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Record("openai", "gpt-4o", 1.0)
			tracker.Snapshot()
		}()
	}
	wg.Wait()

	statuses := tracker.Snapshot()
	if statuses[0].Spent != 100.0 {
		t.Errorf("concurrent spent = %f, want 100.0", statuses[0].Spent)
	}
}

// --- Weekly Window ---

func TestBudgetTrackerWeeklyWindow(t *testing.T) {
	// Start on a Wednesday
	wed := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	mu := sync.Mutex{}
	currentTime := wed

	tracker := &BudgetTracker{
		clock: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		},
	}

	var alertCount int
	tracker.AddBudget(
		Budget{Name: "weekly", Amount: 100.0, Window: BudgetWindowWeekly},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alertCount++ }},
	)

	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 1 {
		t.Fatalf("expected 1 alert, got %d", alertCount)
	}

	// Next Monday — same week (June 15 is Sunday, June 16 is Monday)
	nextMon := time.Date(2026, 6, 15, 1, 0, 0, 0, time.UTC) // Monday
	mu.Lock()
	currentTime = nextMon
	mu.Unlock()

	// Same week (Mon June 8 - Sun June 14) vs Mon June 15 = new week
	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 2 {
		t.Errorf("expected 2 alerts after week reset, got %d", alertCount)
	}
}

// --- Monthly Window ---

func TestBudgetTrackerMonthlyWindow(t *testing.T) {
	june15 := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	mu := sync.Mutex{}
	currentTime := june15

	tracker := &BudgetTracker{
		clock: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		},
	}

	var alertCount int
	tracker.AddBudget(
		Budget{Name: "monthly", Amount: 100.0, Window: BudgetWindowMonthly},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alertCount++ }},
	)

	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 1 {
		t.Fatalf("expected 1 alert, got %d", alertCount)
	}

	// July 1 — new month
	july1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mu.Lock()
	currentTime = july1
	mu.Unlock()

	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 2 {
		t.Errorf("expected 2 alerts after month reset, got %d", alertCount)
	}
}

// --- Total Window (Never Reset) ---

func TestBudgetTrackerTotalWindow(t *testing.T) {
	day1 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	mu := sync.Mutex{}
	currentTime := day1

	tracker := &BudgetTracker{
		clock: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		},
	}

	var alertCount int
	tracker.AddBudget(
		Budget{Name: "lifetime", Amount: 100.0, Window: BudgetWindowTotal},
		AlertThreshold{Percent: 50, Callback: func(a Alert) { alertCount++ }},
		AlertThreshold{Percent: 100, Callback: func(a Alert) { alertCount++ }},
	)

	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 1 {
		t.Fatalf("expected 1 alert, got %d", alertCount)
	}

	// Far in the future — should NOT reset
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	mu.Lock()
	currentTime = future
	mu.Unlock()

	// Spend 60 more — total is now 120, should trigger exceeded
	tracker.Record("openai", "gpt-4o", 60.0)
	if alertCount != 2 {
		t.Errorf("expected 2 alerts (no reset for total), got %d", alertCount)
	}

	statuses := tracker.Snapshot()
	if statuses[0].Spent != 120.0 {
		t.Errorf("total window spent = %f, want 120.0", statuses[0].Spent)
	}
}

// --- Exceeded Alert ---

func TestBudgetTrackerExceededAlert(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	var exceededAlert *Alert
	tracker.AddBudget(
		Budget{Name: "strict", Amount: 10.0, Window: BudgetWindowDaily},
		AlertThreshold{
			Percent: 100,
			Callback: func(a Alert) {
				exceededAlert = &a
			},
		},
	)

	// Spend more than budget
	tracker.Record("openai", "gpt-4o", 15.0)

	if exceededAlert == nil {
		t.Fatal("expected exceeded alert")
	}
	if exceededAlert.Severity != AlertExceeded {
		t.Errorf("severity = %v, want exceeded", exceededAlert.Severity)
	}
	if exceededAlert.Spent != 15.0 {
		t.Errorf("spent = %f, want 15.0", exceededAlert.Spent)
	}
	if exceededAlert.Remaining != -5.0 {
		t.Errorf("remaining = %f, want -5.0", exceededAlert.Remaining)
	}
	if exceededAlert.Percent != 150.0 {
		t.Errorf("percent = %f, want 150.0", exceededAlert.Percent)
	}
}

// --- Snapshot With No Spending ---

func TestBudgetTrackerSnapshotEmpty(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.AddBudget(Budget{Name: "empty", Amount: 100.0, Window: BudgetWindowDaily})

	statuses := tracker.Snapshot()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Spent != 0 {
		t.Errorf("spent = %f, want 0", statuses[0].Spent)
	}
	if statuses[0].Remaining != 100.0 {
		t.Errorf("remaining = %f, want 100.0", statuses[0].Remaining)
	}
	if statuses[0].Percent != 0 {
		t.Errorf("percent = %f, want 0", statuses[0].Percent)
	}
}

// --- BudgetContextWithProvider ---

func TestBudgetContextWithProvider(t *testing.T) {
	ctx := context.Background()

	// No provider set
	if got := providerFromContext(ctx); got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}

	// Set provider
	ctx = BudgetContextWithProvider(ctx, "anthropic")
	if got := providerFromContext(ctx); got != "anthropic" {
		t.Errorf("expected anthropic, got %q", got)
	}
}

// --- No Match Means No Spending ---

func TestBudgetTrackerNoMatch(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	tracker := &BudgetTracker{
		clock: func() time.Time { return now },
	}

	tracker.AddBudget(Budget{
		Name:     "openai-only",
		Amount:   100.0,
		Window:   BudgetWindowDaily,
		Provider: "openai",
	})

	// Record for different provider
	tracker.Record("anthropic", "claude-3", 50.0)

	statuses := tracker.Snapshot()
	if statuses[0].Spent != 0 {
		t.Errorf("non-matching provider should not record spending, got %f", statuses[0].Spent)
	}
}
