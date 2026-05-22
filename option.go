package llmtrace

// Option configures a Tracer.
type Option func(*Tracer)

// WithProvider sets the LLM provider name (e.g. "openai", "anthropic").
func WithProvider(provider string) Option {
	return func(t *Tracer) {
		t.provider = provider
	}
}

// WithCostCalculator sets the cost calculator for automatic cost tracking.
func WithCostCalculator(calc *CostCalculator) Option {
	return func(t *Tracer) {
		t.costCalc = calc
	}
}
