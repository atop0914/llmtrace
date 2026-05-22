package llmtrace

import "sync"

// CostCalculator calculates the USD cost of LLM calls based on model pricing.
type CostCalculator struct {
	mu     sync.RWMutex
	prices map[string]CostEntry
}

// NewCostCalculator creates a cost calculator with default pricing.
func NewCostCalculator() *CostCalculator {
	c := &CostCalculator{
		prices: make(map[string]CostEntry),
	}
	c.loadDefaults()
	return c
}

// SetPrice sets the pricing for a specific model.
func (c *CostCalculator) SetPrice(model string, entry CostEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prices[model] = entry
}

// Calculate returns the USD cost for the given model and usage.
// Returns 0 if the model is not in the pricing table.
func (c *CostCalculator) Calculate(model string, usage Usage) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	price, ok := c.prices[model]
	if !ok {
		return 0
	}

	inputCost := float64(usage.InputTokens) / 1000.0 * price.InputCostPer1K
	outputCost := float64(usage.OutputTokens) / 1000.0 * price.OutputCostPer1K
	return inputCost + outputCost
}

// loadDefaults populates pricing for common models.
// Prices are per 1000 tokens in USD (as of 2026-Q2).
func (c *CostCalculator) loadDefaults() {
	defaults := map[string]CostEntry{
		// OpenAI GPT-4 family
		"gpt-4o":        {InputCostPer1K: 0.0025, OutputCostPer1K: 0.01},
		"gpt-4o-mini":   {InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006},
		"gpt-4-turbo":   {InputCostPer1K: 0.01, OutputCostPer1K: 0.03},
		"gpt-4":         {InputCostPer1K: 0.03, OutputCostPer1K: 0.06},
		"gpt-3.5-turbo": {InputCostPer1K: 0.0005, OutputCostPer1K: 0.0015},

		// OpenAI o-series
		"o1":      {InputCostPer1K: 0.015, OutputCostPer1K: 0.06},
		"o1-mini": {InputCostPer1K: 0.003, OutputCostPer1K: 0.012},
		"o3-mini": {InputCostPer1K: 0.0011, OutputCostPer1K: 0.0044},

		// Anthropic Claude family
		"claude-sonnet-4-20250514":   {InputCostPer1K: 0.003, OutputCostPer1K: 0.015},
		"claude-3-5-sonnet-20241022": {InputCostPer1K: 0.003, OutputCostPer1K: 0.015},
		"claude-3-5-haiku-20241022":  {InputCostPer1K: 0.0008, OutputCostPer1K: 0.004},
		"claude-3-opus-20240229":     {InputCostPer1K: 0.015, OutputCostPer1K: 0.075},

		// Google Gemini
		"gemini-2.5-pro":   {InputCostPer1K: 0.00125, OutputCostPer1K: 0.01},
		"gemini-2.5-flash": {InputCostPer1K: 0.000075, OutputCostPer1K: 0.0003},
		"gemini-2.0-flash": {InputCostPer1K: 0.0001, OutputCostPer1K: 0.0004},
	}

	for model, entry := range defaults {
		c.prices[model] = entry
	}
}
