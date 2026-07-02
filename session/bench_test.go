package session

import (
	"fmt"
	"testing"

	"github.com/atop0914/llmtrace"
)

func BenchmarkSession_AddMessage(b *testing.B) {
	s := &Session{
		id:       "bench-1",
		messages: make([]llmtrace.Message, 0, 1000),
		metadata: make(map[string]string),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.AddUserMessage(fmt.Sprintf("Message %d", i))
	}
}

func BenchmarkSession_Messages(b *testing.B) {
	s := &Session{
		id:       "bench-2",
		messages: make([]llmtrace.Message, 0, 100),
		metadata: make(map[string]string),
	}
	for i := 0; i < 50; i++ {
		s.AddUserMessage(fmt.Sprintf("Q%d", i))
		s.AddAssistantMessage(fmt.Sprintf("A%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Messages()
	}
}

func BenchmarkSession_TurnCount(b *testing.B) {
	s := &Session{
		id:       "bench-3",
		messages: make([]llmtrace.Message, 0, 100),
		metadata: make(map[string]string),
	}
	for i := 0; i < 50; i++ {
		s.AddUserMessage(fmt.Sprintf("Q%d", i))
		s.AddAssistantMessage(fmt.Sprintf("A%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.TurnCount()
	}
}

func BenchmarkManager_CreateGetDelete(b *testing.B) {
	m := NewManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := m.Create()
		m.Get(s.ID())
		m.Delete(s.ID())
	}
}

func BenchmarkSession_EstimateTokens(b *testing.B) {
	s := &Session{
		id:       "bench-tokens",
		messages: make([]llmtrace.Message, 0, 100),
		metadata: make(map[string]string),
	}
	for i := 0; i < 20; i++ {
		s.AddUserMessage("This is a test message with some content to estimate tokens")
		s.AddAssistantMessage("This is a response with some content that should be counted")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.EstimateTokens()
	}
}
