// Package tokencount provides token counting, context window management,
// and cost estimation for LLM API calls.
//
// It maintains a registry of model-specific context limits and pricing,
// estimates token counts from text, and provides pre-call validation
// to prevent context window overflow errors.
//
// Usage:
//
//	mgr := tokencount.NewManager()
//	check := mgr.ValidateRequest("gpt-4", messages, 1000)
//	if !check.FitsContext {
//	    // handle overflow: check.SuggestedMaxTokens
//	}
//	cost := mgr.EstimateCost("gpt-4", inputTokens, outputTokens)
package tokencount

import "math"

// ModelInfo holds context window limits and pricing for a specific model.
type ModelInfo struct {
	// ContextWindow is the maximum total tokens (input + output).
	ContextWindow int

	// MaxOutputTokens is the provider's max output limit.
	MaxOutputTokens int

	// InputCostPer1K is the cost per 1000 input tokens (USD).
	InputCostPer1K float64

	// OutputCostPer1K is the cost per 1000 output tokens (USD).
	OutputCostPer1K float64

	// CharsPerToken is the average characters per token for estimation.
	// Typical values: 4.0 for English, 3.0 for mixed-language, 2.0 for CJK-heavy.
	CharsPerToken float64
}

// DefaultModels returns a ModelRegistry pre-populated with common models.
func DefaultModels() *ModelRegistry {
	r := &ModelRegistry{models: make(map[string]ModelInfo)}
	for name, info := range builtinModels {
		r.models[name] = info
	}
	return r
}

// ModelRegistry stores model-specific configuration.
type ModelRegistry struct {
	models map[string]ModelInfo
}

// Register adds or updates a model in the registry.
func (r *ModelRegistry) Register(name string, info ModelInfo) {
	r.models[name] = info
}

// Get returns model info and whether it was found.
func (r *ModelRegistry) Get(name string) (ModelInfo, bool) {
	info, ok := r.models[name]
	return info, ok
}

// List returns all registered model names.
func (r *ModelRegistry) List() []string {
	names := make([]string, 0, len(r.models))
	for name := range r.models {
		names = append(names, name)
	}
	return names
}

// EstimateTokens estimates the token count for a given text using
// the model's chars-per-token ratio.
func EstimateTokens(text string, charsPerToken float64) int {
	if charsPerToken <= 0 {
		charsPerToken = 4.0
	}
	return int(math.Ceil(float64(len(text)) / charsPerToken))
}

// builtinModels contains common LLM model specifications.
// Prices as of mid-2026 (USD per 1K tokens).
var builtinModels = map[string]ModelInfo{
	// OpenAI
	"gpt-4o": {
		ContextWindow:   128000,
		MaxOutputTokens: 16384,
		InputCostPer1K:  0.0025,
		OutputCostPer1K: 0.01,
		CharsPerToken:   4.0,
	},
	"gpt-4o-mini": {
		ContextWindow:   128000,
		MaxOutputTokens: 16384,
		InputCostPer1K:  0.00015,
		OutputCostPer1K: 0.0006,
		CharsPerToken:   4.0,
	},
	"gpt-4-turbo": {
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
		CharsPerToken:   4.0,
	},
	"o1": {
		ContextWindow:   200000,
		MaxOutputTokens: 100000,
		InputCostPer1K:  0.015,
		OutputCostPer1K: 0.06,
		CharsPerToken:   4.0,
	},
	"o3-mini": {
		ContextWindow:   200000,
		MaxOutputTokens: 100000,
		InputCostPer1K:  0.0011,
		OutputCostPer1K: 0.0044,
		CharsPerToken:   4.0,
	},
	// Anthropic
	"claude-sonnet-4-20250514": {
		ContextWindow:   200000,
		MaxOutputTokens: 16384,
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.015,
		CharsPerToken:   3.8,
	},
	"claude-3-5-sonnet-20241022": {
		ContextWindow:   200000,
		MaxOutputTokens: 8192,
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.015,
		CharsPerToken:   3.8,
	},
	"claude-3-5-haiku-20241022": {
		ContextWindow:   200000,
		MaxOutputTokens: 8192,
		InputCostPer1K:  0.001,
		OutputCostPer1K: 0.005,
		CharsPerToken:   3.8,
	},
	"claude-3-opus-20240229": {
		ContextWindow:   200000,
		MaxOutputTokens: 4096,
		InputCostPer1K:  0.015,
		OutputCostPer1K: 0.075,
		CharsPerToken:   3.8,
	},
	// Google
	"gemini-2.0-flash": {
		ContextWindow:   1048576,
		MaxOutputTokens: 8192,
		InputCostPer1K:  0.0001,
		OutputCostPer1K: 0.0004,
		CharsPerToken:   4.0,
	},
	"gemini-1.5-pro": {
		ContextWindow:   2097152,
		MaxOutputTokens: 8192,
		InputCostPer1K:  0.00125,
		OutputCostPer1K: 0.005,
		CharsPerToken:   4.0,
	},
	// Meta
	"llama-3.1-405b": {
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		InputCostPer1K:  0.003,
		OutputCostPer1K: 0.003,
		CharsPerToken:   4.0,
	},
	"llama-3.1-70b": {
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		InputCostPer1K:  0.0009,
		OutputCostPer1K: 0.0009,
		CharsPerToken:   4.0,
	},
	// Mistral
	"mistral-large-latest": {
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
		InputCostPer1K:  0.002,
		OutputCostPer1K: 0.006,
		CharsPerToken:   4.0,
	},
}
