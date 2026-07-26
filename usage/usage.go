// Package usage provides aggregated usage analytics for LLM applications.
//
// It collects and aggregates token usage, cost, and latency data across
// providers and models, offering actionable insights such as:
//   - Cost breakdown by provider, model, and time period
//   - Usage trend detection (increasing, decreasing, stable)
//   - Anomaly detection for sudden usage spikes
//   - Top-N queries for expensive models and endpoints
//
// Usage:
//
//	tracker := usage.NewTracker(usage.Config{
//	    Window: usage.WindowDaily,
//	})
//	tracker.Record(usage.Record{
//	    Provider:  "openai",
//	    Model:     "gpt-4o",
//	    InputTok:  500,
//	    OutputTok: 200,
//	    Cost:      0.005,
//	    Latency:   320 * time.Millisecond,
//	})
//	report := tracker.Report()
package usage

import (
	"sort"
	"sync"
	"time"
)

// Window defines the aggregation time window.
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

// Config configures a Tracker.
type Config struct {
	// Window is the aggregation window. Default: WindowDaily.
	Window Window
	// Retention is how long to keep records. Default: 30 days.
	Retention time.Duration
	// AnomalyThreshold is the z-score threshold for anomaly detection. Default: 2.5.
	AnomalyThreshold float64
}

// Record represents a single LLM call's usage data.
type Record struct {
	Provider  string
	Model     string
	InputTok  int
	OutputTok int
	Cost      float64
	Latency   time.Duration
	Timestamp time.Time
	Endpoint  string // e.g., "/v1/chat/completions"
	UserID    string // optional user identifier
	Metadata  map[string]string
}

// Key returns the aggregation key for this record.
func (r Record) Key() string {
	return r.Provider + "/" + r.Model
}

// Tracker collects and aggregates usage records.
type Tracker struct {
	mu      sync.RWMutex
	cfg     Config
	records []Record
}

// NewTracker creates a new usage Tracker.
func NewTracker(cfg Config) *Tracker {
	if cfg.Window == 0 {
		cfg.Window = WindowDaily
	}
	if cfg.Retention == 0 {
		cfg.Retention = 30 * 24 * time.Hour
	}
	if cfg.AnomalyThreshold == 0 {
		cfg.AnomalyThreshold = 2.5
	}
	return &Tracker{cfg: cfg}
}

// Record adds a usage record to the tracker.
func (t *Tracker) Record(r Record) {
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}
	t.mu.Lock()
	t.records = append(t.records, r)
	t.mu.Unlock()
}

// RecordBatch adds multiple usage records.
func (t *Tracker) RecordBatch(records []Record) {
	now := time.Now()
	t.mu.Lock()
	for _, r := range records {
		if r.Timestamp.IsZero() {
			r.Timestamp = now
		}
		t.records = append(t.records, r)
	}
	t.mu.Unlock()
}

// Count returns the total number of records.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.records)
}

// Prune removes records older than the retention period.
func (t *Tracker) Prune() int {
	cutoff := time.Now().Add(-t.cfg.Retention)
	t.mu.Lock()
	before := len(t.records)
	kept := make([]Record, 0, len(t.records))
	for _, r := range t.records {
		if r.Timestamp.After(cutoff) {
			kept = append(kept, r)
		}
	}
	t.records = kept
	t.mu.Unlock()
	return before - len(kept)
}

// Report generates an aggregated usage report.
func (t *Tracker) Report() Report {
	t.mu.RLock()
	defer t.mu.RUnlock()

	r := Report{
		GeneratedAt: time.Now(),
		TotalCalls:  len(t.records),
	}

	if len(t.records) == 0 {
		return r
	}

	// Aggregate by provider/model
	byKey := make(map[string]*ModelUsage)
	byProvider := make(map[string]*ProviderUsage)

	for _, rec := range t.records {
		key := rec.Key()
		totalTok := rec.InputTok + rec.OutputTok

		// Per model
		mu, ok := byKey[key]
		if !ok {
			mu = &ModelUsage{
				Provider: rec.Provider,
				Model:    rec.Model,
			}
			byKey[key] = mu
		}
		mu.Calls++
		mu.InputTokens += int64(rec.InputTok)
		mu.OutputTokens += int64(rec.OutputTok)
		mu.TotalTokens += int64(totalTok)
		mu.TotalCost += rec.Cost
		mu.TotalLatency += rec.Latency
		if rec.Latency > mu.MaxLatency {
			mu.MaxLatency = rec.Latency
		}
		if mu.MinLatency == 0 || rec.Latency < mu.MinLatency {
			mu.MinLatency = rec.Latency
		}

		// Per provider
		pu, ok := byProvider[rec.Provider]
		if !ok {
			pu = &ProviderUsage{Provider: rec.Provider}
			byProvider[rec.Provider] = pu
		}
		pu.Calls++
		pu.InputTokens += int64(rec.InputTok)
		pu.OutputTokens += int64(rec.OutputTok)
		pu.TotalCost += rec.Cost

		// Totals
		r.TotalInputTokens += int64(rec.InputTok)
		r.TotalOutputTokens += int64(rec.OutputTok)
		r.TotalCost += rec.Cost
		r.TotalLatency += rec.Latency
	}

	// Compute averages for models
	for _, mu := range byKey {
		if mu.Calls > 0 {
			mu.AvgLatency = time.Duration(int64(mu.TotalLatency) / int64(mu.Calls))
			mu.AvgCostPerCall = mu.TotalCost / float64(mu.Calls)
		}
	}

	// Build sorted model list
	r.Models = make([]ModelUsage, 0, len(byKey))
	for _, mu := range byKey {
		r.Models = append(r.Models, *mu)
	}
	sort.Slice(r.Models, func(i, j int) bool {
		return r.Models[i].TotalCost > r.Models[j].TotalCost
	})

	// Build sorted provider list
	r.Providers = make([]ProviderUsage, 0, len(byProvider))
	for _, pu := range byProvider {
		r.Providers = append(r.Providers, *pu)
	}
	sort.Slice(r.Providers, func(i, j int) bool {
		return r.Providers[i].TotalCost > r.Providers[j].TotalCost
	})

	// Average latency
	if r.TotalCalls > 0 {
		r.AvgLatency = time.Duration(int64(r.TotalLatency) / int64(r.TotalCalls))
		r.AvgCostPerCall = r.TotalCost / float64(r.TotalCalls)
	}

	return r
}

// Report contains aggregated usage analytics.
type Report struct {
	GeneratedAt       time.Time
	TotalCalls        int
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCost         float64
	AvgCostPerCall    float64
	TotalLatency      time.Duration
	AvgLatency        time.Duration
	Models            []ModelUsage
	Providers         []ProviderUsage
}

// ModelUsage contains usage stats for a specific model.
type ModelUsage struct {
	Provider       string
	Model          string
	Calls          int
	InputTokens    int64
	OutputTokens   int64
	TotalTokens    int64
	TotalCost      float64
	AvgCostPerCall float64
	TotalLatency   time.Duration
	AvgLatency     time.Duration
	MinLatency     time.Duration
	MaxLatency     time.Duration
}

// ProviderUsage contains usage stats for a provider.
type ProviderUsage struct {
	Provider     string
	Calls        int
	InputTokens  int64
	OutputTokens int64
	TotalCost    float64
}

// TopModels returns the top N models by cost.
func (r Report) TopModels(n int) []ModelUsage {
	if n > len(r.Models) {
		n = len(r.Models)
	}
	return r.Models[:n]
}

// CostByProvider returns a map of provider -> total cost.
func (r Report) CostByProvider() map[string]float64 {
	m := make(map[string]float64, len(r.Providers))
	for _, p := range r.Providers {
		m[p.Provider] = p.TotalCost
	}
	return m
}
