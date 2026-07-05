package tokencount

import (
	"strings"
	"testing"
)

func BenchmarkEstimateTokens(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text, 4.0)
	}
}

func BenchmarkValidateRequest(b *testing.B) {
	m := NewManager()
	msgs := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: strings.Repeat("Hello world. ", 100)},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ValidateRequest("gpt-4o", msgs, 1000)
	}
}

func BenchmarkTruncateToFit(b *testing.B) {
	m := NewManager()
	msgs := make([]Message, 20)
	for i := range msgs {
		msgs[i] = Message{Role: "user", Content: strings.Repeat("x", 5000)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.TruncateToFit("gpt-4o", msgs, 1000)
	}
}

func BenchmarkRecommendModel(b *testing.B) {
	m := NewManager()
	msgs := []Message{
		{Role: "user", Content: "What is the capital of France?"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecommendModel(msgs, 500)
	}
}
