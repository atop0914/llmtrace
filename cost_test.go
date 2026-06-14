package llmtrace

import (
	"sync"
	"testing"
)

func TestNewCostCalculator(t *testing.T) {
	c := NewCostCalculator()
	if c == nil {
		t.Fatal("NewCostCalculator returned nil")
	}
	if c.prices == nil {
		t.Fatal("prices map not initialized")
	}
	// Should have default prices loaded
	if len(c.prices) == 0 {
		t.Fatal("no default prices loaded")
	}
}

func TestCostCalculator_DefaultPrices(t *testing.T) {
	c := NewCostCalculator()

	tests := []struct {
		model      string
		wantInput  float64
		wantOutput float64
	}{
		{"gpt-4o", 0.0025, 0.01},
		{"gpt-4o-mini", 0.00015, 0.0006},
		{"gpt-4-turbo", 0.01, 0.03},
		{"gpt-4", 0.03, 0.06},
		{"gpt-3.5-turbo", 0.0005, 0.0015},
		{"o1", 0.015, 0.06},
		{"o1-mini", 0.003, 0.012},
		{"o3-mini", 0.0011, 0.0044},
		{"claude-sonnet-4-20250514", 0.003, 0.015},
		{"claude-3-5-sonnet-20241022", 0.003, 0.015},
		{"claude-3-5-haiku-20241022", 0.0008, 0.004},
		{"claude-3-opus-20240229", 0.015, 0.075},
		{"gemini-2.5-pro", 0.00125, 0.01},
		{"gemini-2.5-flash", 0.000075, 0.0003},
		{"gemini-2.0-flash", 0.0001, 0.0004},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			c.mu.RLock()
			entry, ok := c.prices[tt.model]
			c.mu.RUnlock()
			if !ok {
				t.Fatalf("model %q not found in defaults", tt.model)
			}
			if entry.InputCostPer1K != tt.wantInput {
				t.Errorf("InputCostPer1K = %f, want %f", entry.InputCostPer1K, tt.wantInput)
			}
			if entry.OutputCostPer1K != tt.wantOutput {
				t.Errorf("OutputCostPer1K = %f, want %f", entry.OutputCostPer1K, tt.wantOutput)
			}
		})
	}
}

func TestCostCalculator_SetPrice(t *testing.T) {
	c := NewCostCalculator()
	c.SetPrice("custom-model", CostEntry{
		InputCostPer1K:  0.005,
		OutputCostPer1K: 0.015,
	})

	usage := Usage{InputTokens: 1000, OutputTokens: 1000}
	cost := c.Calculate("custom-model", usage)
	// (1000/1000 * 0.005) + (1000/1000 * 0.015) = 0.02
	if diff := cost - 0.02; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %f, want 0.02", cost)
	}
}

func TestCostCalculator_SetPrice_Override(t *testing.T) {
	c := NewCostCalculator()
	// Override gpt-4o pricing
	c.SetPrice("gpt-4o", CostEntry{
		InputCostPer1K:  0.001,
		OutputCostPer1K: 0.005,
	})

	usage := Usage{InputTokens: 1000, OutputTokens: 1000}
	cost := c.Calculate("gpt-4o", usage)
	// (1000/1000 * 0.001) + (1000/1000 * 0.005) = 0.006
	if diff := cost - 0.006; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %f, want 0.006", cost)
	}
}

func TestCostCalculator_Calculate(t *testing.T) {
	c := NewCostCalculator()

	tests := []struct {
		name     string
		model    string
		usage    Usage
		wantCost float64
	}{
		{
			name:     "gpt-4o 1k input 1k output",
			model:    "gpt-4o",
			usage:    Usage{InputTokens: 1000, OutputTokens: 1000},
			wantCost: 0.0025 + 0.01, // 0.0125
		},
		{
			name:     "gpt-4o 500 input 200 output",
			model:    "gpt-4o",
			usage:    Usage{InputTokens: 500, OutputTokens: 200},
			wantCost: 500.0/1000*0.0025 + 200.0/1000*0.01, // 0.00125 + 0.002 = 0.00325
		},
		{
			name:     "unknown model returns 0",
			model:    "unknown-model",
			usage:    Usage{InputTokens: 1000, OutputTokens: 1000},
			wantCost: 0,
		},
		{
			name:     "zero tokens",
			model:    "gpt-4o",
			usage:    Usage{InputTokens: 0, OutputTokens: 0},
			wantCost: 0,
		},
		{
			name:     "large token count",
			model:    "gpt-4",
			usage:    Usage{InputTokens: 100000, OutputTokens: 50000},
			wantCost: 100000.0/1000*0.03 + 50000.0/1000*0.06, // 3.0 + 3.0 = 6.0
		},
		{
			name:     "claude-3-opus",
			model:    "claude-3-opus-20240229",
			usage:    Usage{InputTokens: 2000, OutputTokens: 1000},
			wantCost: 2000.0/1000*0.015 + 1000.0/1000*0.075, // 0.03 + 0.075 = 0.105
		},
		{
			name:     "gemini-2.5-flash cheap",
			model:    "gemini-2.5-flash",
			usage:    Usage{InputTokens: 10000, OutputTokens: 10000},
			wantCost: 10000.0/1000*0.000075 + 10000.0/1000*0.0003, // 0.00075 + 0.003 = 0.00375
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Calculate(tt.model, tt.usage)
			if diff := got - tt.wantCost; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("Calculate() = %f, want %f", got, tt.wantCost)
			}
		})
	}
}

func TestCostCalculator_Concurrent(t *testing.T) {
	c := NewCostCalculator()
	var wg sync.WaitGroup
	n := 100

	// Concurrent reads
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Calculate("gpt-4o", Usage{InputTokens: 100, OutputTokens: 100})
		}()
	}

	// Concurrent writes
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.SetPrice("test-model", CostEntry{InputCostPer1K: 0.001, OutputCostPer1K: 0.002})
		}()
	}

	wg.Wait()
}
