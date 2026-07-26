package usage

import (
	"math"
	"testing"
	"time"
)

func TestTracker_Record(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})

	if tr.Count() != 0 {
		t.Fatal("expected 0 records")
	}

	tr.Record(Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		InputTok:  100,
		OutputTok: 50,
		Cost:      0.002,
		Latency:   200 * time.Millisecond,
	})

	if tr.Count() != 1 {
		t.Fatalf("expected 1 record, got %d", tr.Count())
	}
}

func TestTracker_RecordBatch(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", InputTok: 100, OutputTok: 50, Cost: 0.002},
		{Provider: "openai", Model: "gpt-4o", InputTok: 200, OutputTok: 100, Cost: 0.004},
		{Provider: "anthropic", Model: "claude-3", InputTok: 150, OutputTok: 80, Cost: 0.003},
	})

	if tr.Count() != 3 {
		t.Fatalf("expected 3 records, got %d", tr.Count())
	}
}

func TestReport_Empty(t *testing.T) {
	tr := NewTracker(Config{})
	r := tr.Report()

	if r.TotalCalls != 0 {
		t.Errorf("expected 0 calls, got %d", r.TotalCalls)
	}
	if r.TotalCost != 0 {
		t.Errorf("expected 0 cost, got %f", r.TotalCost)
	}
}

func TestReport_Aggregation(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", InputTok: 1000, OutputTok: 500, Cost: 0.01, Latency: 300 * time.Millisecond},
		{Provider: "openai", Model: "gpt-4o", InputTok: 2000, OutputTok: 1000, Cost: 0.02, Latency: 400 * time.Millisecond},
		{Provider: "anthropic", Model: "claude-3", InputTok: 500, OutputTok: 200, Cost: 0.005, Latency: 250 * time.Millisecond},
	})

	r := tr.Report()

	if r.TotalCalls != 3 {
		t.Errorf("expected 3 calls, got %d", r.TotalCalls)
	}
	if r.TotalInputTokens != 3500 {
		t.Errorf("expected 3500 input tokens, got %d", r.TotalInputTokens)
	}
	if r.TotalOutputTokens != 1700 {
		t.Errorf("expected 1700 output tokens, got %d", r.TotalOutputTokens)
	}
	if diff := r.TotalCost - 0.035; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("expected 0.035 cost, got %f", r.TotalCost)
	}

	if len(r.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(r.Models))
	}

	// Models sorted by cost descending
	if r.Models[0].Model != "gpt-4o" {
		t.Errorf("expected first model gpt-4o, got %s", r.Models[0].Model)
	}
	if r.Models[0].Calls != 2 {
		t.Errorf("expected 2 calls for gpt-4o, got %d", r.Models[0].Calls)
	}
	if r.Models[0].TotalCost != 0.03 {
		t.Errorf("expected 0.03 cost for gpt-4o, got %f", r.Models[0].TotalCost)
	}
	if r.Models[0].TotalTokens != 4500 {
		t.Errorf("expected 4500 total tokens for gpt-4o, got %d", r.Models[0].TotalTokens)
	}
}

func TestReport_Providers(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", InputTok: 100, OutputTok: 50, Cost: 0.01},
		{Provider: "anthropic", Model: "claude-3", InputTok: 100, OutputTok: 50, Cost: 0.005},
		{Provider: "openai", Model: "gpt-3.5", InputTok: 100, OutputTok: 50, Cost: 0.001},
	})

	r := tr.Report()

	if len(r.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(r.Providers))
	}

	// Sorted by cost
	if r.Providers[0].Provider != "openai" {
		t.Errorf("expected first provider openai, got %s", r.Providers[0].Provider)
	}
	if r.Providers[0].Calls != 2 {
		t.Errorf("expected 2 calls for openai, got %d", r.Providers[0].Calls)
	}
}

func TestReport_TopModels(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", Cost: 0.10},
		{Provider: "anthropic", Model: "claude-3", Cost: 0.05},
		{Provider: "openai", Model: "gpt-3.5", Cost: 0.01},
	})

	r := tr.Report()
	top := r.TopModels(2)

	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
	if top[0].Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", top[0].Model)
	}
	if top[1].Model != "claude-3" {
		t.Errorf("expected claude-3, got %s", top[1].Model)
	}
}

func TestReport_CostByProvider(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", Cost: 0.10},
		{Provider: "anthropic", Model: "claude-3", Cost: 0.05},
	})

	r := tr.Report()
	cbp := r.CostByProvider()

	if math.Abs(cbp["openai"]-0.10) > 1e-9 {
		t.Errorf("expected openai cost 0.10, got %f", cbp["openai"])
	}
	if math.Abs(cbp["anthropic"]-0.05) > 1e-9 {
		t.Errorf("expected anthropic cost 0.05, got %f", cbp["anthropic"])
	}
}

func TestReport_AvgLatency(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.RecordBatch([]Record{
		{Provider: "openai", Model: "gpt-4o", Cost: 0.01, Latency: 200 * time.Millisecond},
		{Provider: "openai", Model: "gpt-4o", Cost: 0.01, Latency: 400 * time.Millisecond},
	})

	r := tr.Report()

	expected := 300 * time.Millisecond
	if r.AvgLatency != expected {
		t.Errorf("expected avg latency %v, got %v", expected, r.AvgLatency)
	}
	if r.Models[0].MinLatency != 200*time.Millisecond {
		t.Errorf("expected min latency 200ms, got %v", r.Models[0].MinLatency)
	}
	if r.Models[0].MaxLatency != 400*time.Millisecond {
		t.Errorf("expected max latency 400ms, got %v", r.Models[0].MaxLatency)
	}
}

func TestTracker_Prune(t *testing.T) {
	tr := NewTracker(Config{
		Window:    WindowDaily,
		Retention: 24 * time.Hour,
	})

	tr.Record(Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		Cost:      0.01,
		Timestamp: time.Now().Add(-48 * time.Hour), // old
	})
	tr.Record(Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		Cost:      0.01,
		Timestamp: time.Now(), // recent
	})

	if tr.Count() != 2 {
		t.Fatalf("expected 2, got %d", tr.Count())
	}

	pruned := tr.Prune()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if tr.Count() != 1 {
		t.Errorf("expected 1 remaining, got %d", tr.Count())
	}
}

func TestDetectTrend_Increasing(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	base := time.Now().Add(-7 * 24 * time.Hour)

	// Increasing cost over 7 days
	for i := 0; i < 7; i++ {
		for j := 0; j < 5; j++ {
			tr.Record(Record{
				Provider:  "openai",
				Model:     "gpt-4o",
				Cost:      float64(i+1) * 0.01,
				Timestamp: base.Add(time.Duration(i) * 24 * time.Hour),
			})
		}
	}

	result := tr.DetectTrend()
	if result.Trend != TrendIncreasing {
		t.Errorf("expected increasing trend, got %s", result.Trend)
	}
	if result.Confidence < 0.5 {
		t.Errorf("expected confidence > 0.5, got %f", result.Confidence)
	}
}

func TestDetectTrend_Stable(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	base := time.Now().Add(-5 * 24 * time.Hour)

	// Flat cost
	for i := 0; i < 5; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			Cost:      0.01,
			Timestamp: base.Add(time.Duration(i) * 24 * time.Hour),
		})
	}

	result := tr.DetectTrend()
	if result.Trend != TrendStable {
		t.Errorf("expected stable trend, got %s", result.Trend)
	}
}

func TestDetectTrend_InsufficientData(t *testing.T) {
	tr := NewTracker(Config{Window: WindowDaily})
	tr.Record(Record{Provider: "openai", Model: "gpt-4o", Cost: 0.01})

	result := tr.DetectTrend()
	if result.Trend != TrendStable {
		t.Errorf("expected stable for insufficient data, got %s", result.Trend)
	}
}

func TestDetectAnomalies(t *testing.T) {
	tr := NewTracker(Config{
		Window:           WindowDaily,
		AnomalyThreshold: 2.0,
	})
	base := time.Now()

	// Normal costs
	for i := 0; i < 20; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			Cost:      0.01,
			Timestamp: base.Add(time.Duration(i) * time.Hour),
		})
	}

	// Anomaly
	tr.Record(Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		Cost:      1.0,
		Timestamp: base.Add(21 * time.Hour),
	})

	anomalies := tr.DetectAnomalies()
	if len(anomalies) == 0 {
		t.Fatal("expected at least 1 anomaly")
	}

	found := false
	for _, a := range anomalies {
		if a.Cost == 1.0 {
			found = true
			if a.ZScore < 2.0 {
				t.Errorf("expected z-score >= 2.0, got %f", a.ZScore)
			}
		}
	}
	if !found {
		t.Error("expected to find the $1.0 anomaly")
	}
}

func TestDetectAnomalies_NoAnomalies(t *testing.T) {
	tr := NewTracker(Config{
		Window:           WindowDaily,
		AnomalyThreshold: 3.0,
	})

	// All similar costs
	for i := 0; i < 20; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			Cost:      0.01,
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
		})
	}

	anomalies := tr.DetectAnomalies()
	if len(anomalies) != 0 {
		t.Errorf("expected 0 anomalies, got %d", len(anomalies))
	}
}

func TestSuggestOptimizations_HighCost(t *testing.T) {
	r := Report{
		TotalCalls: 100,
		TotalCost:  50.0,
		Models: []ModelUsage{
			{Provider: "openai", Model: "gpt-4o", Calls: 100, TotalCost: 50.0, InputTokens: 100000, OutputTokens: 50000},
		},
	}

	suggestions := SuggestOptimizations(r, CostOptimizationConfig{
		HighCostThreshold: 10.0,
	})

	if len(suggestions) == 0 {
		t.Fatal("expected suggestions")
	}

	found := false
	for _, s := range suggestions {
		if s.Type == SuggestionHighCostModel {
			found = true
		}
	}
	if !found {
		t.Error("expected high cost model suggestion")
	}
}

func TestSuggestOptimizations_HighInputTokens(t *testing.T) {
	r := Report{
		TotalCalls: 100,
		Models: []ModelUsage{
			{Provider: "openai", Model: "gpt-4o", Calls: 100, TotalCost: 5.0, InputTokens: 100000, OutputTokens: 1000},
		},
	}

	suggestions := SuggestOptimizations(r, CostOptimizationConfig{
		HighCostThreshold:   100.0, // not triggered
		HighInputTokenRatio: 10.0,
	})

	found := false
	for _, s := range suggestions {
		if s.Type == SuggestionHighInputTokens {
			found = true
		}
	}
	if !found {
		t.Error("expected high input tokens suggestion")
	}
}

func TestSuggestOptimizations_LowUtilization(t *testing.T) {
	r := Report{
		TotalCalls: 2,
		Models: []ModelUsage{
			{Provider: "openai", Model: "gpt-4o", Calls: 2, TotalCost: 0.01, InputTokens: 100, OutputTokens: 50},
		},
	}

	suggestions := SuggestOptimizations(r, CostOptimizationConfig{
		HighCostThreshold:      100.0,
		HighInputTokenRatio:    100.0,
		HighLatencyThreshold:   10000,
		LowUtilizationMinCalls: 5,
	})

	found := false
	for _, s := range suggestions {
		if s.Type == SuggestionLowUtilization {
			found = true
		}
	}
	if !found {
		t.Error("expected low utilization suggestion")
	}
}

func TestEstimateCost(t *testing.T) {
	pricing := map[string][2]float64{
		"gpt-4o": {0.005, 0.015},
	}

	cost := EstimateCost("gpt-4o", 1000, 500, pricing)
	expected := 1000.0/1000.0*0.005 + 500.0/1000.0*0.015
	if math.Abs(cost-expected) > 1e-9 {
		t.Errorf("expected %f, got %f", expected, cost)
	}

	// Unknown model
	cost = EstimateCost("unknown", 1000, 500, pricing)
	if cost != 0 {
		t.Errorf("expected 0 for unknown model, got %f", cost)
	}
}

func TestTrendResult_String(t *testing.T) {
	tests := []struct {
		trend Trend
		want  string
	}{
		{TrendStable, "stable"},
		{TrendIncreasing, "increasing"},
		{TrendDecreasing, "decreasing"},
	}
	for _, tt := range tests {
		if got := tt.trend.String(); got != tt.want {
			t.Errorf("Trend(%d).String() = %q, want %q", tt.trend, got, tt.want)
		}
	}
}

func TestSuggestionType_String(t *testing.T) {
	tests := []struct {
		st   SuggestionType
		want string
	}{
		{SuggestionHighCostModel, "high_cost_model"},
		{SuggestionHighInputTokens, "high_input_tokens"},
		{SuggestionHighLatency, "high_latency"},
		{SuggestionLowUtilization, "low_utilization"},
		{SuggestionType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.st.String(); got != tt.want {
			t.Errorf("SuggestionType(%d).String() = %q, want %q", tt.st, got, tt.want)
		}
	}
}

func TestTracker_TimestampDefault(t *testing.T) {
	tr := NewTracker(Config{})
	before := time.Now()
	tr.Record(Record{Provider: "openai", Model: "gpt-4o", Cost: 0.01})
	after := time.Now()

	tr.mu.RLock()
	ts := tr.records[0].Timestamp
	tr.mu.RUnlock()

	if ts.Before(before) || ts.After(after) {
		t.Errorf("expected timestamp between %v and %v, got %v", before, after, ts)
	}
}

func TestReport_ZeroLatencyAverage(t *testing.T) {
	tr := NewTracker(Config{})
	tr.Record(Record{Provider: "openai", Model: "gpt-4o", Cost: 0.01, Latency: 0})

	r := tr.Report()
	if r.AvgLatency != 0 {
		t.Errorf("expected 0 avg latency, got %v", r.AvgLatency)
	}
}
