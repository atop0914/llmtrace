package tokenreport

import (
	"sync"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func makeRecord(provider, model string, inputTok, outputTok int, cost float64, latencyMS float64, status string, ts time.Time) llmtrace.TraceRecord {
	return llmtrace.TraceRecord{
		Provider:     provider,
		Model:        model,
		Status:       status,
		InputTokens:  inputTok,
		OutputTokens: outputTok,
		TotalTokens:  inputTok + outputTok,
		CostUSD:      cost,
		LatencyMS:    latencyMS,
		StartedAt:    ts,
		CompletedAt:  ts.Add(time.Duration(latencyMS) * time.Millisecond),
	}
}

func TestAggregatorIngest(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 14, 30, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", now.Add(time.Hour)),
		makeRecord("anthropic", "claude-3", 150, 60, 0.008, 250, "success", now),
	)

	if agg.Count() != 1 {
		t.Errorf("expected 1 daily bucket, got %d", agg.Count())
	}

	total := agg.Total()
	if total.Requests != 3 {
		t.Errorf("expected 3 requests, got %d", total.Requests)
	}
	if total.Successes != 3 {
		t.Errorf("expected 3 successes, got %d", total.Successes)
	}
	if total.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", total.Errors)
	}
	if total.InputTokens != 450 {
		t.Errorf("expected 450 input tokens, got %d", total.InputTokens)
	}
	if total.OutputTokens != 190 {
		t.Errorf("expected 190 output tokens, got %d", total.OutputTokens)
	}
	if total.TotalTokens != 640 {
		t.Errorf("expected 640 total tokens, got %d", total.TotalTokens)
	}

	// Check by provider
	byProv := agg.ByProvider()
	if len(byProv) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(byProv))
	}
	if byProv["openai"].Requests != 2 {
		t.Errorf("expected 2 openai requests, got %d", byProv["openai"].Requests)
	}
	if byProv["anthropic"].Requests != 1 {
		t.Errorf("expected 1 anthropic request, got %d", byProv["anthropic"].Requests)
	}

	// Check by model
	byModel := agg.ByModel()
	if len(byModel) != 2 {
		t.Fatalf("expected 2 models, got %d", len(byModel))
	}
	if byModel["openai/gpt-4o"].TotalTokens != 430 {
		t.Errorf("expected 430 openai/gpt-4o tokens, got %d", byModel["openai/gpt-4o"].TotalTokens)
	}
	if byModel["anthropic/claude-3"].TotalTokens != 210 {
		t.Errorf("expected 210 anthropic/claude-3 tokens, got %d", byModel["anthropic/claude-3"].TotalTokens)
	}
}

func TestAggregatorMultipleDays(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	day1 := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 11, 10, 0, 0, 0, time.UTC)
	day3 := time.Date(2025, 6, 12, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", day1),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", day2),
		makeRecord("anthropic", "claude-3", 150, 60, 0.008, 250, "success", day3),
	)

	if agg.Count() != 3 {
		t.Errorf("expected 3 daily buckets, got %d", agg.Count())
	}

	buckets := agg.Buckets()
	if len(buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(buckets))
	}

	// Buckets should be sorted by time
	if !buckets[0].Start.Before(buckets[1].Start) {
		t.Error("buckets not sorted correctly")
	}

	// Each bucket should have 1 request
	for i, b := range buckets {
		if b.Requests != 1 {
			t.Errorf("bucket %d: expected 1 request, got %d", i, b.Requests)
		}
	}
}

func TestAggregatorHourly(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowHourly})
	base := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", base),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", base.Add(30*time.Minute)),
		makeRecord("openai", "gpt-4o", 150, 60, 0.008, 250, "success", base.Add(2*time.Hour)),
	)

	if agg.Count() != 2 {
		t.Errorf("expected 2 hourly buckets, got %d", agg.Count())
	}

	buckets := agg.Buckets()
	// First hour should have 2 requests
	if buckets[0].Requests != 2 {
		t.Errorf("expected 2 requests in first hour, got %d", buckets[0].Requests)
	}
	if buckets[1].Requests != 1 {
		t.Errorf("expected 1 request in second hour, got %d", buckets[1].Requests)
	}
}

func TestAggregatorWeekly(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowWeekly})
	// Wednesday and Thursday of the same week
	wed := time.Date(2025, 6, 11, 10, 0, 0, 0, time.UTC) // Wednesday
	thu := time.Date(2025, 6, 12, 10, 0, 0, 0, time.UTC) // Thursday
	// Next Monday
	mon := time.Date(2025, 6, 16, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", wed),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", thu),
		makeRecord("anthropic", "claude-3", 150, 60, 0.008, 250, "success", mon),
	)

	if agg.Count() != 2 {
		t.Errorf("expected 2 weekly buckets, got %d", agg.Count())
	}

	buckets := agg.Buckets()
	// First week should have 2 requests
	if buckets[0].Requests != 2 {
		t.Errorf("expected 2 requests in first week, got %d", buckets[0].Requests)
	}
	if buckets[1].Requests != 1 {
		t.Errorf("expected 1 request in second week, got %d", buckets[1].Requests)
	}

	// Verify bucket start is a Monday
	if buckets[0].Start.Weekday() != time.Monday {
		t.Errorf("expected first bucket to start on Monday, got %s", buckets[0].Start.Weekday())
	}
}

func TestAggregatorMonthly(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowMonthly})
	june := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	july := time.Date(2025, 7, 5, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", june),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", july),
	)

	if agg.Count() != 2 {
		t.Errorf("expected 2 monthly buckets, got %d", agg.Count())
	}

	buckets := agg.Buckets()
	if buckets[0].Start.Month() != time.June {
		t.Errorf("expected first bucket in June, got %s", buckets[0].Start.Month())
	}
	if buckets[1].Start.Month() != time.July {
		t.Errorf("expected second bucket in July, got %s", buckets[1].Start.Month())
	}
}

func TestAggregatorErrors(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now),
		makeRecord("openai", "gpt-4o", 0, 0, 0, 50, "error", now.Add(time.Hour)),
		makeRecord("anthropic", "claude-3", 150, 60, 0.008, 250, "error", now),
	)

	total := agg.Total()
	if total.Requests != 3 {
		t.Errorf("expected 3 requests, got %d", total.Requests)
	}
	if total.Successes != 1 {
		t.Errorf("expected 1 success, got %d", total.Successes)
	}
	if total.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", total.Errors)
	}
}

func TestAggregatorBucketsInRange(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	day1 := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 11, 10, 0, 0, 0, time.UTC)
	day3 := time.Date(2025, 6, 12, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", day1),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", day2),
		makeRecord("openai", "gpt-4o", 150, 60, 0.008, 250, "success", day3),
	)

	// Query range that includes only day2
	buckets := agg.BucketsInRange(day2, day3)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket in range, got %d", len(buckets))
	}
	if buckets[0].Requests != 1 {
		t.Errorf("expected 1 request, got %d", buckets[0].Requests)
	}
}

func TestAggregatorTimeRange(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	t1 := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 12, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", t1),
		makeRecord("openai", "gpt-4o", 200, 80, 0.012, 350, "success", t2),
	)

	earliest, latest := agg.TimeRange()
	if !earliest.Equal(t1) {
		t.Errorf("expected earliest %v, got %v", t1, earliest)
	}
	if !latest.Equal(t2) {
		t.Errorf("expected latest %v, got %v", t2, latest)
	}
}

func TestAggregatorReset(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)

	agg.Ingest(makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now))

	if agg.Count() != 1 {
		t.Fatalf("expected 1 bucket before reset, got %d", agg.Count())
	}

	agg.Reset()

	if agg.Count() != 0 {
		t.Errorf("expected 0 buckets after reset, got %d", agg.Count())
	}
	total := agg.Total()
	if total.Requests != 0 {
		t.Errorf("expected 0 requests after reset, got %d", total.Requests)
	}
}

func TestAggregatorConcurrent(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	base := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agg.Ingest(makeRecord(
				"openai", "gpt-4o",
				100+i, 50+i, float64(i)*0.001, float64(100+i*5),
				"success", base.Add(time.Duration(i)*time.Minute),
			))
		}(i)
	}
	wg.Wait()

	total := agg.Total()
	if total.Requests != 100 {
		t.Errorf("expected 100 requests, got %d", total.Requests)
	}
}

func TestDimensionAccumAdd(t *testing.T) {
	a := DimensionAccum{
		Requests:     10,
		Successes:    8,
		Errors:       2,
		InputTokens:  500,
		OutputTokens: 200,
		TotalTokens:  700,
		CostUSD:      0.05,
		LatencySumMS: 2000,
		LatencyCount: 10,
	}
	b := DimensionAccum{
		Requests:     5,
		Successes:    5,
		Errors:       0,
		InputTokens:  300,
		OutputTokens: 100,
		TotalTokens:  400,
		CostUSD:      0.03,
		LatencySumMS: 1000,
		LatencyCount: 5,
	}
	a.Add(&b)

	if a.Requests != 15 {
		t.Errorf("expected 15 requests, got %d", a.Requests)
	}
	if a.TotalTokens != 1100 {
		t.Errorf("expected 1100 total tokens, got %d", a.TotalTokens)
	}
	if a.AvgLatencyMS() != 200 {
		t.Errorf("expected 200 avg latency, got %f", a.AvgLatencyMS())
	}
}

func TestDimensionAccumAvgLatencyZero(t *testing.T) {
	d := DimensionAccum{}
	if d.AvgLatencyMS() != 0 {
		t.Errorf("expected 0 avg latency for empty accum, got %f", d.AvgLatencyMS())
	}
}

func TestAggregatorBucketProvenance(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now),
		makeRecord("openai", "gpt-4o-mini", 50, 20, 0.001, 100, "success", now),
		makeRecord("anthropic", "claude-3", 150, 60, 0.008, 250, "success", now),
	)

	buckets := agg.Buckets()
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}
	b := buckets[0]

	// By provider in bucket
	if len(b.ByProvider) != 2 {
		t.Errorf("expected 2 providers in bucket, got %d", len(b.ByProvider))
	}
	if b.ByProvider["openai"].Requests != 2 {
		t.Errorf("expected 2 openai requests in bucket, got %d", b.ByProvider["openai"].Requests)
	}

	// By model in bucket
	if len(b.ByModel) != 3 {
		t.Errorf("expected 3 models in bucket, got %d", len(b.ByModel))
	}
}
