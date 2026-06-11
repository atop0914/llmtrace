// Package tokenreport provides time-windowed aggregation and reporting
// for LLM token usage. It reads from a TraceStore and aggregates token
// consumption by provider, model, and configurable time windows
// (hourly, daily, weekly, monthly).
//
// Usage:
//
//	agg := tokenreport.NewAggregator(tokenreport.Config{
//	    Window: tokenreport.WindowDaily,
//	})
//	agg.Ingest(store.All()...)
//	report := agg.BuildReport()
package tokenreport

import (
	"sort"
	"sync"
	"time"

	"github.com/atop0914/llmtrace"
)

// Window defines the time window for aggregation.
type Window int

const (
	// WindowHourly aggregates by hour.
	WindowHourly Window = iota
	// WindowDaily aggregates by day.
	WindowDaily
	// WindowWeekly aggregates by week (Monday start).
	WindowWeekly
	// WindowMonthly aggregates by month.
	WindowMonthly
)

// Config configures an Aggregator.
type Config struct {
	// Window is the time window for aggregation. Default: WindowDaily.
	Window Window

	// Location for time truncation. Default: time.UTC.
	Location *time.Location
}

// Aggregator processes trace records and maintains time-windowed aggregates.
// All methods are safe for concurrent use.
type Aggregator struct {
	mu       sync.RWMutex
	window   Window
	loc      *time.Location
	buckets  map[string]*Bucket // key is truncated time string
	byProv   map[string]*DimensionAccum
	byModel  map[string]*DimensionAccum
	total    DimensionAccum
	earliest time.Time
	latest   time.Time
}

// Bucket represents an aggregated time window.
type Bucket struct {
	// Start is the beginning of the time window.
	Start time.Time `json:"start"`
	// End is the end of the time window (exclusive).
	End time.Time `json:"end"`

	DimensionAccum

	// ByProvider breaks down this bucket by provider.
	ByProvider map[string]*DimensionAccum `json:"by_provider,omitempty"`
	// ByModel breaks down this bucket by model (provider/model key).
	ByModel map[string]*DimensionAccum `json:"by_model,omitempty"`
}

// DimensionAccum accumulates token and cost metrics for a dimension.
type DimensionAccum struct {
	// Requests is the total number of requests.
	Requests int64 `json:"requests"`
	// Successes is the number of successful requests.
	Successes int64 `json:"successes"`
	// Errors is the number of failed requests.
	Errors int64 `json:"errors"`
	// InputTokens is the total input tokens consumed.
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens is the total output tokens produced.
	OutputTokens int64 `json:"output_tokens"`
	// TotalTokens is the sum of input and output tokens.
	TotalTokens int64 `json:"total_tokens"`
	// CostUSD is the total cost in USD.
	CostUSD float64 `json:"cost_usd"`
	// LatencySumMS is the cumulative latency in milliseconds.
	LatencySumMS float64 `json:"-"`
	// LatencyCount is the number of latency observations.
	LatencyCount int64 `json:"-"`
}

// AvgLatencyMS returns the average latency in milliseconds.
func (d *DimensionAccum) AvgLatencyMS() float64 {
	if d.LatencyCount == 0 {
		return 0
	}
	return d.LatencySumMS / float64(d.LatencyCount)
}

// Add merges another DimensionAccum into this one.
func (d *DimensionAccum) Add(other *DimensionAccum) {
	d.Requests += other.Requests
	d.Successes += other.Successes
	d.Errors += other.Errors
	d.InputTokens += other.InputTokens
	d.OutputTokens += other.OutputTokens
	d.TotalTokens += other.TotalTokens
	d.CostUSD += other.CostUSD
	d.LatencySumMS += other.LatencySumMS
	d.LatencyCount += other.LatencyCount
}

// NewAggregator creates a new Aggregator with the given configuration.
func NewAggregator(cfg Config) *Aggregator {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	return &Aggregator{
		window:  cfg.Window,
		loc:     cfg.Location,
		buckets: make(map[string]*Bucket),
		byProv:  make(map[string]*DimensionAccum),
		byModel: make(map[string]*DimensionAccum),
	}
}

// Ingest processes one or more trace records into the aggregator.
func (a *Aggregator) Ingest(records ...llmtrace.TraceRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, rec := range records {
		a.ingestOne(rec)
	}
}

func (a *Aggregator) ingestOne(rec llmtrace.TraceRecord) {
	ts := rec.StartedAt.In(a.loc)
	key := a.bucketKey(ts)

	bucket, ok := a.buckets[key]
	if !ok {
		bucket = &Bucket{
			Start:      a.truncate(ts),
			End:        a.bucketEnd(ts),
			ByProvider: make(map[string]*DimensionAccum),
			ByModel:    make(map[string]*DimensionAccum),
		}
		a.buckets[key] = bucket
	}

	accum := accumFromRecord(rec)

	// Update bucket totals
	bucket.DimensionAccum.Add(accum)

	// Update bucket by-provider
	bp := bucket.ByProvider[rec.Provider]
	if bp == nil {
		bp = &DimensionAccum{}
		bucket.ByProvider[rec.Provider] = bp
	}
	bp.Add(accum)

	// Update bucket by-model (provider/model composite key)
	modelKey := rec.Provider + "/" + rec.Model
	bm := bucket.ByModel[modelKey]
	if bm == nil {
		bm = &DimensionAccum{}
		bucket.ByModel[modelKey] = bm
	}
	bm.Add(accum)

	// Update global by-provider
	gp := a.byProv[rec.Provider]
	if gp == nil {
		gp = &DimensionAccum{}
		a.byProv[rec.Provider] = gp
	}
	gp.Add(accum)

	// Update global by-model
	gm := a.byModel[modelKey]
	if gm == nil {
		gm = &DimensionAccum{}
		a.byModel[modelKey] = gm
	}
	gm.Add(accum)

	// Update totals
	a.total.Add(accum)

	// Track time range
	if a.earliest.IsZero() || rec.StartedAt.Before(a.earliest) {
		a.earliest = rec.StartedAt
	}
	if rec.StartedAt.After(a.latest) {
		a.latest = rec.StartedAt
	}
}

func accumFromRecord(rec llmtrace.TraceRecord) *DimensionAccum {
	accum := &DimensionAccum{
		Requests:     1,
		InputTokens:  int64(rec.InputTokens),
		OutputTokens: int64(rec.OutputTokens),
		TotalTokens:  int64(rec.TotalTokens),
		CostUSD:      rec.CostUSD,
		LatencySumMS: rec.LatencyMS,
		LatencyCount: 1,
	}
	if rec.Status == "error" {
		accum.Errors = 1
	} else {
		accum.Successes = 1
	}
	return accum
}

// bucketKey returns the map key for a given timestamp.
func (a *Aggregator) bucketKey(t time.Time) string {
	truncated := a.truncate(t)
	return truncated.Format(time.RFC3339)
}

// truncate truncates a time to the configured window boundary.
func (a *Aggregator) truncate(t time.Time) time.Time {
	switch a.window {
	case WindowHourly:
		return t.Truncate(time.Hour)
	case WindowDaily:
		// Truncate to start of day in the configured location
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, a.loc)
	case WindowWeekly:
		// Truncate to Monday of the week
		y, m, d := t.Date()
		dow := t.Weekday()
		mondayOffset := (int(dow) + 6) % 7 // Monday=0, Sunday=6
		monday := time.Date(y, m, d-mondayOffset, 0, 0, 0, 0, a.loc)
		return monday
	case WindowMonthly:
		y, m, _ := t.Date()
		return time.Date(y, m, 1, 0, 0, 0, 0, a.loc)
	default:
		return t.Truncate(time.Hour)
	}
}

// bucketEnd returns the exclusive end time for a bucket.
func (a *Aggregator) bucketEnd(t time.Time) time.Time {
	start := a.truncate(t)
	switch a.window {
	case WindowHourly:
		return start.Add(time.Hour)
	case WindowDaily:
		return start.AddDate(0, 0, 1)
	case WindowWeekly:
		return start.AddDate(0, 0, 7)
	case WindowMonthly:
		return start.AddDate(0, 1, 0)
	default:
		return start.Add(time.Hour)
	}
}

// Buckets returns all aggregated time buckets, sorted by start time.
func (a *Aggregator) Buckets() []Bucket {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]Bucket, 0, len(a.buckets))
	for _, b := range a.buckets {
		result = append(result, *b)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start.Before(result[j].Start)
	})
	return result
}

// BucketsInRange returns buckets within the given time range [since, until).
func (a *Aggregator) BucketsInRange(since, until time.Time) []Bucket {
	all := a.Buckets()
	var result []Bucket
	for _, b := range all {
		if !b.Start.Before(since) && b.Start.Before(until) {
			result = append(result, b)
		}
	}
	return result
}

// Total returns the overall aggregate across all time windows.
func (a *Aggregator) Total() DimensionAccum {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.total
}

// ByProvider returns aggregates keyed by provider name.
func (a *Aggregator) ByProvider() map[string]DimensionAccum {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]DimensionAccum, len(a.byProv))
	for k, v := range a.byProv {
		result[k] = *v
	}
	return result
}

// ByModel returns aggregates keyed by "provider/model".
func (a *Aggregator) ByModel() map[string]DimensionAccum {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]DimensionAccum, len(a.byModel))
	for k, v := range a.byModel {
		result[k] = *v
	}
	return result
}

// TimeRange returns the earliest and latest trace timestamps ingested.
func (a *Aggregator) TimeRange() (earliest, latest time.Time) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.earliest, a.latest
}

// Count returns the number of time buckets.
func (a *Aggregator) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.buckets)
}

// Reset clears all aggregated data.
func (a *Aggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buckets = make(map[string]*Bucket)
	a.byProv = make(map[string]*DimensionAccum)
	a.byModel = make(map[string]*DimensionAccum)
	a.total = DimensionAccum{}
	a.earliest = time.Time{}
	a.latest = time.Time{}
}
