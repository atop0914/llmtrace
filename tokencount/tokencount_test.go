package tokencount

import (
	"math"
	"strings"
	"testing"
)

func TestDefaultModels(t *testing.T) {
	r := DefaultModels()
	names := r.List()
	if len(names) == 0 {
		t.Fatal("DefaultModels() returned empty registry")
	}

	// Spot-check some models
	for _, want := range []string{"gpt-4o", "claude-sonnet-4-20250514", "gemini-2.0-flash"} {
		info, ok := r.Get(want)
		if !ok {
			t.Errorf("missing model %q", want)
			continue
		}
		if info.ContextWindow == 0 {
			t.Errorf("model %q has zero ContextWindow", want)
		}
		if info.CharsPerToken <= 0 {
			t.Errorf("model %q has invalid CharsPerToken: %f", want, info.CharsPerToken)
		}
	}
}

func TestModelRegistry_Register(t *testing.T) {
	r := DefaultModels()
	r.Register("custom-model", ModelInfo{
		ContextWindow:   32000,
		MaxOutputTokens: 4096,
		InputCostPer1K:  0.001,
		OutputCostPer1K: 0.002,
		CharsPerToken:   4.5,
	})
	info, ok := r.Get("custom-model")
	if !ok {
		t.Fatal("custom-model not found after register")
	}
	if info.ContextWindow != 32000 {
		t.Errorf("ContextWindow = %d, want 32000", info.ContextWindow)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text          string
		charsPerToken float64
		want          int
	}{
		{"", 4.0, 0},
		{"hello", 4.0, 2},          // 5 chars / 4.0 = 1.25 → ceil = 2
		{"hello world", 4.0, 3},    // 11 chars / 4.0 = 2.75 → ceil = 3
		{strings.Repeat("a", 100), 4.0, 25},
		{strings.Repeat("a", 100), 0, 25},  // defaults to 4.0
		{"你好世界", 2.0, 6},                // 12 bytes (CJK 3 bytes each) / 2.0 = 6
	}
	for _, tt := range tests {
		got := EstimateTokens(tt.text, tt.charsPerToken)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q, %.1f) = %d, want %d", tt.text, tt.charsPerToken, got, tt.want)
		}
	}
}

func TestManager_ValidateRequest(t *testing.T) {
	m := NewManager()

	t.Run("fits", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "Hello, how are you?"}}
		r := m.ValidateRequest("gpt-4o", msgs, 1000)
		if r.Error != "" {
			t.Errorf("unexpected error: %s", r.Error)
		}
		if !r.FitsContext {
			t.Error("expected FitsContext=true")
		}
		if r.InputTokens <= 0 {
			t.Errorf("InputTokens = %d, want > 0", r.InputTokens)
		}
		if r.SuggestedMaxTokens <= 0 {
			t.Errorf("SuggestedMaxTokens = %d, want > 0", r.SuggestedMaxTokens)
		}
	})

	t.Run("unknown_model", func(t *testing.T) {
		r := m.ValidateRequest("nonexistent", nil, 0)
		if r.Error == "" {
			t.Error("expected error for unknown model")
		}
	})

	t.Run("overflow", func(t *testing.T) {
		// Create a huge message that overflows context
		huge := strings.Repeat("x", 600000) // ~150k tokens at 4 chars/token
		msgs := []Message{{Role: "user", Content: huge}}
		r := m.ValidateRequest("gpt-4o", msgs, 1000)
		if r.FitsContext {
			t.Error("expected FitsContext=false for overflow")
		}
		if r.Error == "" {
			t.Error("expected error for overflow")
		}
		if r.AvailableForOutput >= 0 {
			t.Errorf("AvailableForOutput = %d, want < 0", r.AvailableForOutput)
		}
	})

	t.Run("max_tokens_zero_uses_model_default", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "Hi"}}
		r := m.ValidateRequest("gpt-4o", msgs, 0)
		if r.RequestedMaxTokens != 0 {
			t.Errorf("RequestedMaxTokens = %d, want 0", r.RequestedMaxTokens)
		}
		// SuggestedMaxTokens should be min(available, MaxOutputTokens)
		info, _ := m.registry.Get("gpt-4o")
		if r.SuggestedMaxTokens > info.MaxOutputTokens {
			t.Errorf("SuggestedMaxTokens %d > MaxOutputTokens %d", r.SuggestedMaxTokens, info.MaxOutputTokens)
		}
	})

	t.Run("usage_ratio", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "Hello"}}
		r := m.ValidateRequest("gpt-4o", msgs, 0)
		if r.UsageRatio <= 0 || r.UsageRatio >= 1 {
			t.Errorf("UsageRatio = %f, want between 0 and 1", r.UsageRatio)
		}
	})

	t.Run("preserves_system_message", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		}
		r := m.ValidateRequest("gpt-4o", msgs, 1000)
		if !r.FitsContext {
			t.Error("expected FitsContext=true")
		}
	})
}

func TestManager_ValidateText(t *testing.T) {
	m := NewManager()
	r := m.ValidateText("gpt-4o", "Hello world, this is a test.", 500)
	if r.Error != "" {
		t.Errorf("unexpected error: %s", r.Error)
	}
	if !r.FitsContext {
		t.Error("expected FitsContext=true")
	}
}

func TestManager_ValidateTokenCount(t *testing.T) {
	m := NewManager()

	t.Run("fits", func(t *testing.T) {
		r := m.ValidateTokenCount("gpt-4o", 1000, 4000)
		if !r.FitsContext {
			t.Error("expected FitsContext=true")
		}
	})

	t.Run("exact_fit", func(t *testing.T) {
		info, _ := m.registry.Get("gpt-4o")
		r := m.ValidateTokenCount("gpt-4o", info.ContextWindow-100, 100)
		if !r.FitsContext {
			t.Error("expected FitsContext=true for exact fit")
		}
	})

	t.Run("over_by_one", func(t *testing.T) {
		info, _ := m.registry.Get("gpt-4o")
		r := m.ValidateTokenCount("gpt-4o", info.ContextWindow-100, 101)
		if r.FitsContext {
			t.Error("expected FitsContext=false when over by one")
		}
	})
}

func TestManager_EstimateCost(t *testing.T) {
	m := NewManager()

	t.Run("gpt4o", func(t *testing.T) {
		cost, err := m.EstimateCost("gpt-4o", 1000, 1000)
		if err != nil {
			t.Fatal(err)
		}
		// gpt-4o: $0.0025/1K input + $0.01/1K output = $0.0125 for 1K+1K
		want := 0.0025 + 0.01
		if diff := math.Abs(cost - want); diff > 1e-9 {
			t.Errorf("cost = %f, want %f", cost, want)
		}
	})

	t.Run("zero_tokens", func(t *testing.T) {
		cost, err := m.EstimateCost("gpt-4o", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if cost != 0 {
			t.Errorf("cost = %f, want 0", cost)
		}
	})

	t.Run("unknown_model", func(t *testing.T) {
		_, err := m.EstimateCost("nonexistent", 1000, 1000)
		if err == nil {
			t.Error("expected error for unknown model")
		}
	})
}

func TestManager_EstimateMessagesCost(t *testing.T) {
	m := NewManager()
	msgs := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What is the capital of France?"},
	}
	cost, err := m.EstimateMessagesCost("gpt-4o", msgs, 50)
	if err != nil {
		t.Fatal(err)
	}
	if cost <= 0 {
		t.Errorf("cost = %f, want > 0", cost)
	}
}

func TestManager_RecommendModel(t *testing.T) {
	m := NewManager()

	t.Run("short_input", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "Hello"}}
		model := m.RecommendModel(msgs, 100)
		if model == "" {
			t.Fatal("expected a model recommendation")
		}
		// Should recommend a cheap model
		info, _ := m.registry.Get(model)
		if info.ContextWindow == 0 {
			t.Errorf("recommended model %q has no context window", model)
		}
	})

	t.Run("no_model_fits", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: strings.Repeat("x", 10000000)}}
		model := m.RecommendModel(msgs, 100000)
		if model != "" {
			t.Errorf("expected empty recommendation, got %q", model)
		}
	})
}

func TestManager_TruncateToFit(t *testing.T) {
	m := NewManager()

	t.Run("no_truncation_needed", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		}
		result, truncated := m.TruncateToFit("gpt-4o", msgs, 1000)
		if truncated {
			t.Error("expected no truncation")
		}
		if len(result) != 3 {
			t.Errorf("got %d messages, want 3", len(result))
		}
	})

	t.Run("truncates_from_beginning", func(t *testing.T) {
		// 10 messages × 60K chars = 600K chars = ~150K tokens → exceeds 128K window
		msgs := make([]Message, 10)
		for i := range msgs {
			msgs[i] = Message{Role: "user", Content: strings.Repeat("x", 60000)}
		}
		result, truncated := m.TruncateToFit("gpt-4o", msgs, 1000)
		if !truncated {
			t.Error("expected truncation")
		}
		if len(result) >= len(msgs) {
			t.Errorf("expected fewer messages, got %d vs %d", len(result), len(msgs))
		}
	})

	t.Run("preserves_system_message", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: strings.Repeat("x", 100000)},
			{Role: "assistant", Content: strings.Repeat("y", 100000)},
			{Role: "user", Content: strings.Repeat("z", 100000)},
		}
		result, _ := m.TruncateToFit("gpt-4o", msgs, 1000)
		if len(result) == 0 {
			t.Fatal("empty result")
		}
		if result[0].Role != "system" {
			t.Errorf("first message role = %q, want system", result[0].Role)
		}
	})

	t.Run("unknown_model", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "Hello"}}
		result, truncated := m.TruncateToFit("nonexistent", msgs, 1000)
		if truncated {
			t.Error("expected no truncation for unknown model")
		}
		if len(result) != len(msgs) {
			t.Error("messages should be unchanged")
		}
	})
}

func TestFormatMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}
	formatted := formatMessages(msgs)
	if !strings.Contains(formatted, "<|system|>") {
		t.Error("missing system role tag")
	}
	if !strings.Contains(formatted, "<|user|>") {
		t.Error("missing user role tag")
	}
	if !strings.Contains(formatted, "You are helpful.") {
		t.Error("missing system content")
	}
	if !strings.Contains(formatted, "Hello") {
		t.Error("missing user content")
	}
}
