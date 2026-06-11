package tokenreport

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildReportBasic(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	base := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 1000, 500, 0.05, 200, "success", base),
		makeRecord("openai", "gpt-4o-mini", 500, 200, 0.01, 100, "success", base),
		makeRecord("anthropic", "claude-3", 800, 300, 0.04, 250, "success", base),
		makeRecord("anthropic", "claude-3", 200, 100, 0.01, 150, "error", base.Add(time.Hour)),
	)

	rpt := agg.BuildReport(ReportConfig{
		TopN:              3,
		IncludeTimeSeries: true,
	})

	// Basic checks
	if rpt.Window != "daily" {
		t.Errorf("expected window 'daily', got %q", rpt.Window)
	}
	if rpt.Total.Requests != 4 {
		t.Errorf("expected 4 total requests, got %d", rpt.Total.Requests)
	}
	if rpt.Total.Successes != 3 {
		t.Errorf("expected 3 successes, got %d", rpt.Total.Successes)
	}
	if rpt.Total.Errors != 1 {
		t.Errorf("expected 1 error, got %d", rpt.Total.Errors)
	}

	// Provider entries
	if len(rpt.ByProvider) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(rpt.ByProvider))
	}
	// Sorted by total tokens descending
	if rpt.ByProvider[0].Provider != "openai" {
		t.Errorf("expected first provider 'openai', got %q", rpt.ByProvider[0].Provider)
	}

	// Model entries
	if len(rpt.ByModel) != 3 {
		t.Fatalf("expected 3 models, got %d", len(rpt.ByModel))
	}

	// Top models by tokens
	if len(rpt.TopModelsByTokens) != 3 {
		t.Fatalf("expected 3 top models, got %d", len(rpt.TopModelsByTokens))
	}
	if rpt.TopModelsByTokens[0].Model != "openai/gpt-4o" {
		t.Errorf("expected top model 'openai/gpt-4o', got %q", rpt.TopModelsByTokens[0].Model)
	}

	// Top models by cost
	if len(rpt.TopModelsByCost) != 3 {
		t.Fatalf("expected 3 top models by cost, got %d", len(rpt.TopModelsByCost))
	}
	if rpt.TopModelsByCost[0].Model != "openai/gpt-4o" {
		t.Errorf("expected top cost model 'openai/gpt-4o', got %q", rpt.TopModelsByCost[0].Model)
	}

	// Time series
	if len(rpt.TimeSeries) != 1 {
		t.Fatalf("expected 1 time series bucket, got %d", len(rpt.TimeSeries))
	}
}

func TestBuildReportNoTimeSeries(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	agg.Ingest(makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now))

	rpt := agg.BuildReport(ReportConfig{
		IncludeTimeSeries: false,
	})

	if rpt.TimeSeries != nil {
		t.Errorf("expected nil time series, got %v", rpt.TimeSeries)
	}
}

func TestReportWriteJSON(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	agg.Ingest(
		makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now),
	)

	rpt := agg.BuildReport(ReportConfig{IncludeTimeSeries: true})

	var buf bytes.Buffer
	if err := rpt.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// Check required fields
	if _, ok := parsed["generated_at"]; !ok {
		t.Error("missing generated_at in JSON")
	}
	if _, ok := parsed["total"]; !ok {
		t.Error("missing total in JSON")
	}
	if _, ok := parsed["by_provider"]; !ok {
		t.Error("missing by_provider in JSON")
	}
}

func TestReportString(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	base := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)

	agg.Ingest(
		makeRecord("openai", "gpt-4o", 1000, 500, 0.05, 200, "success", base),
		makeRecord("anthropic", "claude-3", 800, 300, 0.04, 250, "error", base),
	)

	rpt := agg.BuildReport(ReportConfig{
		TopN:              2,
		IncludeTimeSeries: true,
	})

	s := rpt.String()

	// Check key sections are present
	checks := []string{
		"LLM Token Usage Report",
		"Totals",
		"By Provider",
		"Top Models by Tokens",
		"Top Models by Cost",
		"Time Series",
		"openai",
		"anthropic",
		"gpt-4o",
		"claude-3",
	}

	for _, check := range checks {
		if !strings.Contains(s, check) {
			t.Errorf("report string missing %q", check)
		}
	}
}

func TestReportStringEmpty(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	rpt := agg.BuildReport(ReportConfig{})

	s := rpt.String()
	if !strings.Contains(s, "LLM Token Usage Report") {
		t.Error("empty report should still have header")
	}
	if !strings.Contains(s, "Requests:      0") {
		t.Error("empty report should show 0 requests")
	}
}

func TestWindowName(t *testing.T) {
	tests := []struct {
		w    Window
		want string
	}{
		{WindowHourly, "hourly"},
		{WindowDaily, "daily"},
		{WindowWeekly, "weekly"},
		{WindowMonthly, "monthly"},
		{Window(99), "unknown"},
	}
	for _, tt := range tests {
		got := windowName(tt.w)
		if got != tt.want {
			t.Errorf("windowName(%d) = %q, want %q", tt.w, got, tt.want)
		}
	}
}

func TestReportTopNLimit(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	base := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)

	models := []string{"gpt-4o", "gpt-4o-mini", "claude-3", "gemini-pro", "llama-3"}
	for i, m := range models {
		agg.Ingest(makeRecord("provider", m, 100*(i+1), 50*(i+1), 0.01*float64(i+1), 100, "success", base))
	}

	rpt := agg.BuildReport(ReportConfig{TopN: 2})

	if len(rpt.TopModelsByTokens) != 2 {
		t.Errorf("expected 2 top models, got %d", len(rpt.TopModelsByTokens))
	}
	if len(rpt.TopModelsByCost) != 2 {
		t.Errorf("expected 2 top models by cost, got %d", len(rpt.TopModelsByCost))
	}
}

func TestReportDefaultTopN(t *testing.T) {
	agg := NewAggregator(Config{Window: WindowDaily})
	now := time.Date(2025, 6, 10, 10, 0, 0, 0, time.UTC)
	agg.Ingest(makeRecord("openai", "gpt-4o", 100, 50, 0.005, 200, "success", now))

	// TopN=0 should default to 5
	rpt := agg.BuildReport(ReportConfig{TopN: 0})
	if len(rpt.TopModelsByTokens) != 1 { // only 1 model exists
		t.Errorf("expected 1 top model (only 1 exists), got %d", len(rpt.TopModelsByTokens))
	}
}
