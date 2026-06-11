package tokenreport

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Report is a serializable token usage report.
type Report struct {
	// GeneratedAt is when the report was built.
	GeneratedAt time.Time `json:"generated_at"`
	// Window is the aggregation window used.
	Window string `json:"window"`
	// TimeRange covers the data period.
	TimeRange TimeRange `json:"time_range"`
	// Total is the overall aggregate.
	Total DimensionAccum `json:"total"`
	// ByProvider breaks down totals by provider.
	ByProvider []ProviderEntry `json:"by_provider"`
	// ByModel breaks down totals by model.
	ByModel []ModelEntry `json:"by_model"`
	// TimeSeries is the bucketed data over time.
	TimeSeries []Bucket `json:"time_series"`
	// TopModelsByTokens lists the top N models by token usage.
	TopModelsByTokens []ModelEntry `json:"top_models_by_tokens,omitempty"`
	// TopModelsByCost lists the top N models by cost.
	TopModelsByCost []ModelEntry `json:"top_models_by_cost,omitempty"`
}

// TimeRange describes the data coverage period.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ProviderEntry is a provider-level aggregate in a report.
type ProviderEntry struct {
	Provider string `json:"provider"`
	DimensionAccum
}

// ModelEntry is a model-level aggregate in a report.
type ModelEntry struct {
	Model string `json:"model"` // "provider/model" format
	DimensionAccum
}

// ReportConfig configures report generation.
type ReportConfig struct {
	// TopN is the number of top models to include (default: 5).
	TopN int
	// IncludeTimeSeries controls whether time-series data is included.
	IncludeTimeSeries bool
}

// BuildReport generates a Report from the aggregator's current state.
func (a *Aggregator) BuildReport(cfg ReportConfig) Report {
	if cfg.TopN <= 0 {
		cfg.TopN = 5
	}

	earliest, latest := a.TimeRange()

	rpt := Report{
		GeneratedAt: time.Now().UTC(),
		Window:      windowName(a.window),
		TimeRange:   TimeRange{Start: earliest, End: latest},
		Total:       a.Total(),
	}

	// Build provider entries
	byProv := a.ByProvider()
	rpt.ByProvider = make([]ProviderEntry, 0, len(byProv))
	for name, acc := range byProv {
		rpt.ByProvider = append(rpt.ByProvider, ProviderEntry{
			Provider:       name,
			DimensionAccum: acc,
		})
	}
	sort.Slice(rpt.ByProvider, func(i, j int) bool {
		return rpt.ByProvider[i].TotalTokens > rpt.ByProvider[j].TotalTokens
	})

	// Build model entries
	byModel := a.ByModel()
	rpt.ByModel = make([]ModelEntry, 0, len(byModel))
	for name, acc := range byModel {
		rpt.ByModel = append(rpt.ByModel, ModelEntry{
			Model:          name,
			DimensionAccum: acc,
		})
	}
	sort.Slice(rpt.ByModel, func(i, j int) bool {
		return rpt.ByModel[i].TotalTokens > rpt.ByModel[j].TotalTokens
	})

	// Top models by tokens
	topN := cfg.TopN
	if topN > len(rpt.ByModel) {
		topN = len(rpt.ByModel)
	}
	rpt.TopModelsByTokens = make([]ModelEntry, topN)
	copy(rpt.TopModelsByTokens, rpt.ByModel[:topN])

	// Top models by cost
	byCost := make([]ModelEntry, len(rpt.ByModel))
	copy(byCost, rpt.ByModel)
	sort.Slice(byCost, func(i, j int) bool {
		return byCost[i].CostUSD > byCost[j].CostUSD
	})
	if topN > len(byCost) {
		topN = len(byCost)
	}
	rpt.TopModelsByCost = make([]ModelEntry, topN)
	copy(rpt.TopModelsByCost, byCost[:topN])

	// Time series
	if cfg.IncludeTimeSeries {
		rpt.TimeSeries = a.Buckets()
	}

	return rpt
}

// windowName returns a human-readable window name.
func windowName(w Window) string {
	switch w {
	case WindowHourly:
		return "hourly"
	case WindowDaily:
		return "daily"
	case WindowWeekly:
		return "weekly"
	case WindowMonthly:
		return "monthly"
	default:
		return "unknown"
	}
}

// WriteJSON writes the report as indented JSON.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// String returns a human-readable text summary of the report.
func (r Report) String() string {
	var sb strings.Builder

	sb.WriteString("=== LLM Token Usage Report ===\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n", r.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Window:    %s\n", r.Window))
	if !r.TimeRange.Start.IsZero() {
		sb.WriteString(fmt.Sprintf("Period:    %s → %s\n",
			r.TimeRange.Start.Format("2006-01-02"),
			r.TimeRange.End.Format("2006-01-02"),
		))
	}
	sb.WriteString("\n")

	// Totals
	sb.WriteString("--- Totals ---\n")
	sb.WriteString(fmt.Sprintf("Requests:      %d\n", r.Total.Requests))
	sb.WriteString(fmt.Sprintf("Successes:     %d\n", r.Total.Successes))
	sb.WriteString(fmt.Sprintf("Errors:        %d\n", r.Total.Errors))
	sb.WriteString(fmt.Sprintf("Input Tokens:  %d\n", r.Total.InputTokens))
	sb.WriteString(fmt.Sprintf("Output Tokens: %d\n", r.Total.OutputTokens))
	sb.WriteString(fmt.Sprintf("Total Tokens:  %d\n", r.Total.TotalTokens))
	sb.WriteString(fmt.Sprintf("Cost (USD):    $%.4f\n", r.Total.CostUSD))
	if r.Total.LatencyCount > 0 {
		sb.WriteString(fmt.Sprintf("Avg Latency:   %.1f ms\n", r.Total.AvgLatencyMS()))
	}
	sb.WriteString("\n")

	// By Provider
	if len(r.ByProvider) > 0 {
		sb.WriteString("--- By Provider ---\n")
		for _, p := range r.ByProvider {
			sb.WriteString(fmt.Sprintf("  %-15s  reqs=%d  tokens=%d  cost=$%.4f  errors=%d\n",
				p.Provider, p.Requests, p.TotalTokens, p.CostUSD, p.Errors))
		}
		sb.WriteString("\n")
	}

	// Top Models by Tokens
	if len(r.TopModelsByTokens) > 0 {
		sb.WriteString("--- Top Models by Tokens ---\n")
		for i, m := range r.TopModelsByTokens {
			sb.WriteString(fmt.Sprintf("  %d. %-30s  tokens=%d  reqs=%d  avg_latency=%.1fms\n",
				i+1, m.Model, m.TotalTokens, m.Requests, m.AvgLatencyMS()))
		}
		sb.WriteString("\n")
	}

	// Top Models by Cost
	if len(r.TopModelsByCost) > 0 {
		sb.WriteString("--- Top Models by Cost ---\n")
		for i, m := range r.TopModelsByCost {
			sb.WriteString(fmt.Sprintf("  %d. %-30s  cost=$%.4f  tokens=%d\n",
				i+1, m.Model, m.CostUSD, m.TotalTokens))
		}
		sb.WriteString("\n")
	}

	// Time series summary
	if len(r.TimeSeries) > 0 {
		sb.WriteString(fmt.Sprintf("--- Time Series (%d windows) ---\n", len(r.TimeSeries)))
		for _, b := range r.TimeSeries {
			sb.WriteString(fmt.Sprintf("  %s  reqs=%d  tokens=%d  cost=$%.4f\n",
				b.Start.Format("2006-01-02 15:04"), b.Requests, b.TotalTokens, b.CostUSD))
		}
	}

	return sb.String()
}
