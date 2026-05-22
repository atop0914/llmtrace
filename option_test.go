package llmtrace

import "testing"

func TestWithProvider(t *testing.T) {
	tracer := &Tracer{}
	WithProvider("openai")(tracer)
	if tracer.provider != "openai" {
		t.Errorf("provider = %q, want %q", tracer.provider, "openai")
	}
}

func TestWithCostCalculator(t *testing.T) {
	calc := NewCostCalculator()
	tracer := &Tracer{}
	WithCostCalculator(calc)(tracer)
	if tracer.costCalc != calc {
		t.Error("cost calculator not set")
	}
}

func TestOptions_Multiple(t *testing.T) {
	calc := NewCostCalculator()
	tracer := NewTracer("test",
		WithProvider("anthropic"),
		WithCostCalculator(calc),
	)
	if tracer.provider != "anthropic" {
		t.Errorf("provider = %q, want %q", tracer.provider, "anthropic")
	}
	if tracer.costCalc != calc {
		t.Error("cost calculator not set")
	}
}

func TestOptions_Defaults(t *testing.T) {
	tracer := NewTracer("test")
	if tracer.provider != "unknown" {
		t.Errorf("default provider = %q, want %q", tracer.provider, "unknown")
	}
	if tracer.costCalc != nil {
		t.Error("default cost calculator should be nil")
	}
}
