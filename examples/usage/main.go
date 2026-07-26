// Command usage demonstrates the usage analytics package for tracking
// LLM token consumption, cost breakdowns, trend detection, and anomaly detection.
//
// This example simulates a week of LLM usage across multiple providers
// and models, then generates a comprehensive analytics report.
//
// Usage:
//
//	go run ./examples/usage
package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/atop0914/llmtrace/usage"
)

func main() {
	// Create a tracker with daily aggregation windows
	tracker := usage.NewTracker(usage.Config{
		Window:           usage.WindowDaily,
		Retention:        30 * 24 * time.Hour,
		AnomalyThreshold: 2.0,
	})

	// Simulate a week of LLM usage
	base := time.Now().Add(-7 * 24 * time.Hour)
	models := []struct {
		provider  string
		model     string
		inputRate float64 // tokens per call
		outputRate float64
		costRate  float64 // cost per call
	}{
		{"openai", "gpt-4o", 800, 300, 0.005},
		{"openai", "gpt-3.5-turbo", 500, 200, 0.0005},
		{"anthropic", "claude-3-sonnet", 600, 250, 0.003},
		{"anthropic", "claude-3-haiku", 400, 150, 0.0008},
	}

	rng := rand.New(rand.NewSource(42))
	for day := 0; day < 7; day++ {
		callsPerDay := 20 + rng.Intn(30) // 20-50 calls per day
		for call := 0; call < callsPerDay; call++ {
			m := models[rng.Intn(len(models))]
			inputTok := int(m.inputRate * (0.5 + rng.Float64()))
			outputTok := int(m.outputRate * (0.5 + rng.Float64()))
			cost := m.costRate * (0.8 + 0.4*rng.Float64())

			tracker.Record(usage.Record{
				Provider:  m.provider,
				Model:     m.model,
				InputTok:  inputTok,
				OutputTok: outputTok,
				Cost:      cost,
				Latency:   time.Duration(100+rng.Intn(500)) * time.Millisecond,
				Timestamp: base.Add(time.Duration(day)*24*time.Hour + time.Duration(call)*time.Hour),
			})
		}
	}

	// Inject one anomaly (very expensive call)
	tracker.Record(usage.Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		InputTok:  50000,
		OutputTok: 10000,
		Cost:      2.5,
		Latency:   5 * time.Second,
		Timestamp: base.Add(3 * 24 * time.Hour),
	})

	fmt.Printf("Total records: %d\n\n", tracker.Count())

	// Generate report
	report := tracker.Report()
	printReport(report)

	// Trend analysis
	fmt.Println("=== Trend Analysis ===")
	trend := tracker.DetectTrend()
	fmt.Printf("  Direction:   %s\n", trend.Trend)
	fmt.Printf("  Slope:       $%.6f/day\n", trend.Slope)
	fmt.Printf("  Confidence:  %.1f%%\n", trend.Confidence*100)
	fmt.Printf("  Current avg: $%.4f\n", trend.CurrentAvg)
	fmt.Printf("  Previous avg: $%.4f\n", trend.PreviousAvg)
	fmt.Printf("  Change:      %+.1f%%\n\n", trend.ChangePercent)

	// Anomaly detection
	fmt.Println("=== Anomalies ===")
	anomalies := tracker.DetectAnomalies()
	if len(anomalies) == 0 {
		fmt.Println("  No anomalies detected.")
	} else {
		for _, a := range anomalies {
			fmt.Printf("  [%.1fσ] %s — $%.4f at %s\n",
				a.ZScore, a.Model, a.Cost, a.Timestamp.Format("Jan 02 15:04"))
		}
	}
	fmt.Println()

	// Cost optimization suggestions
	fmt.Println("=== Optimization Suggestions ===")
	suggestions := usage.SuggestOptimizations(report, usage.CostOptimizationConfig{
		HighCostThreshold:      1.0,
		HighInputTokenRatio:    5.0,
		HighLatencyThreshold:   1000,
		LowUtilizationMinCalls: 5,
	})
	if len(suggestions) == 0 {
		fmt.Println("  No suggestions — usage looks optimal!")
	} else {
		for _, s := range suggestions {
			fmt.Printf("  [P%d] %s: %s\n", s.Priority, s.Type, s.Description)
			fmt.Printf("       Model: %s, Current cost: $%.4f, Potential savings: $%.4f\n",
				s.Model, s.CurrentCost, s.PotentialSavings)
		}
	}
}

func printReport(r usage.Report) {
	fmt.Println("=== Usage Report ===")
	fmt.Printf("  Total calls:        %d\n", r.TotalCalls)
	fmt.Printf("  Total input tokens: %d\n", r.TotalInputTokens)
	fmt.Printf("  Total output tokens: %d\n", r.TotalOutputTokens)
	fmt.Printf("  Total cost:         $%.4f\n", r.TotalCost)
	fmt.Printf("  Avg cost/call:      $%.6f\n", r.AvgCostPerCall)
	fmt.Printf("  Avg latency:        %v\n", r.AvgLatency)
	fmt.Println()

	fmt.Println("  Top models by cost:")
	for i, m := range r.TopModels(5) {
		fmt.Printf("    %d. %s/%s — $%.4f (%d calls, %d tokens)\n",
			i+1, m.Provider, m.Model, m.TotalCost, m.Calls, m.TotalTokens)
	}
	fmt.Println()

	fmt.Println("  Cost by provider:")
	for p, cost := range r.CostByProvider() {
		fmt.Printf("    %s: $%.4f\n", p, cost)
	}
	fmt.Println()
}
