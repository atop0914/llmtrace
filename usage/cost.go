package usage

import (
	"sort"
)

// Suggestion represents a cost optimization recommendation.
type Suggestion struct {
	// Type categorizes the suggestion.
	Type SuggestionType
	// Model is the affected model (provider/model).
	Model string
	// CurrentCost is the current total cost.
	CurrentCost float64
	// PotentialSavings is the estimated savings.
	PotentialSavings float64
	// Description explains the suggestion.
	Description string
	// Priority is 1 (highest) to 5 (lowest).
	Priority int
}

// SuggestionType categorizes optimization suggestions.
type SuggestionType int

const (
	// SuggestionHighCostModel suggests switching from an expensive model.
	SuggestionHighCostModel SuggestionType = iota
	// SuggestionHighInputTokens suggests reducing prompt size.
	SuggestionHighInputTokens
	// SuggestionHighLatency suggests investigating slow models.
	SuggestionHighLatency
	// SuggestionLowUtilization suggests consolidating underused models.
	SuggestionLowUtilization
)

func (st SuggestionType) String() string {
	switch st {
	case SuggestionHighCostModel:
		return "high_cost_model"
	case SuggestionHighInputTokens:
		return "high_input_tokens"
	case SuggestionHighLatency:
		return "high_latency"
	case SuggestionLowUtilization:
		return "low_utilization"
	default:
		return "unknown"
	}
}

// CostOptimizationConfig configures suggestion generation.
type CostOptimizationConfig struct {
	// HighCostThreshold is the cost above which a model is flagged. Default: $10.
	HighCostThreshold float64
	// HighInputTokenRatio is the input/output ratio above which we flag. Default: 10.0.
	HighInputTokenRatio float64
	// HighLatencyThreshold is the avg latency above which we flag. Default: 5s.
	HighLatencyThreshold int64 // milliseconds
	// LowUtilizationMinCalls is the minimum calls to not be flagged. Default: 5.
	LowUtilizationMinCalls int
	// ModelPricing maps model names to cost per 1K tokens (input, output).
	ModelPricing map[string][2]float64
}

// DefaultCostOptimizationConfig returns sensible defaults.
func DefaultCostOptimizationConfig() CostOptimizationConfig {
	return CostOptimizationConfig{
		HighCostThreshold:      10.0,
		HighInputTokenRatio:    10.0,
		HighLatencyThreshold:   5000,
		LowUtilizationMinCalls: 5,
	}
}

// SuggestOptimizations analyzes the report and generates cost-saving suggestions.
func SuggestOptimizations(r Report, cfg CostOptimizationConfig) []Suggestion {
	if cfg.HighCostThreshold == 0 {
		cfg = DefaultCostOptimizationConfig()
	}

	var suggestions []Suggestion

	for _, m := range r.Models {
		// High cost model
		if m.TotalCost > cfg.HighCostThreshold {
			suggestions = append(suggestions, Suggestion{
				Type:            SuggestionHighCostModel,
				Model:           m.Provider + "/" + m.Model,
				CurrentCost:     m.TotalCost,
				PotentialSavings: m.TotalCost * 0.5, // estimate 50% savings with cheaper model
				Description:     "consider using a smaller model for non-critical requests",
				Priority:        1,
			})
		}

		// High input token ratio (prompt-heavy)
		if m.OutputTokens > 0 {
			ratio := float64(m.InputTokens) / float64(m.OutputTokens)
			if ratio > cfg.HighInputTokenRatio {
				suggestions = append(suggestions, Suggestion{
					Type:            SuggestionHighInputTokens,
					Model:           m.Provider + "/" + m.Model,
					CurrentCost:     m.TotalCost,
					PotentialSavings: m.TotalCost * (1.0 - 1.0/ratio) * 0.3,
					Description:     "input tokens are %.1fx output tokens; consider prompt compression",
					Priority:        2,
				})
			}
		}

		// High latency
		if cfg.HighLatencyThreshold > 0 && m.AvgLatency.Milliseconds() > cfg.HighLatencyThreshold {
			suggestions = append(suggestions, Suggestion{
				Type:        SuggestionHighLatency,
				Model:       m.Provider + "/" + m.Model,
				CurrentCost: m.TotalCost,
				Description: "average latency is %s; consider caching or a faster model",
				Priority:    3,
			})
		}

		// Low utilization
		if cfg.LowUtilizationMinCalls > 0 && m.Calls < cfg.LowUtilizationMinCalls {
			suggestions = append(suggestions, Suggestion{
				Type:        SuggestionLowUtilization,
				Model:       m.Provider + "/" + m.Model,
				CurrentCost: m.TotalCost,
				Description: "only %d calls; consider consolidating to reduce complexity",
				Priority:    4,
			})
		}
	}

	// Sort by priority (ascending)
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Priority < suggestions[j].Priority
	})

	return suggestions
}

// EstimateCost calculates cost based on model pricing and token counts.
// Returns 0 if the model is not in the pricing map.
func EstimateCost(model string, inputTokens, outputTokens int, pricing map[string][2]float64) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1000.0 * p[0]
	outputCost := float64(outputTokens) / 1000.0 * p[1]
	return inputCost + outputCost
}
