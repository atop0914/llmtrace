package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func TestSession_AddMessage(t *testing.T) {
	s := &Session{
		id:       "test-1",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Hello")
	if len(s.Messages()) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages()))
	}
	if s.Messages()[0].Role != llmtrace.RoleUser {
		t.Errorf("expected role user, got %s", s.Messages()[0].Role)
	}

	s.AddAssistantMessage("Hi there!")
	if len(s.Messages()) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(s.Messages()))
	}
}

func TestSession_TurnCount(t *testing.T) {
	s := &Session{
		id:       "test-2",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Q1")
	s.AddAssistantMessage("A1")
	s.AddUserMessage("Q2")
	s.AddAssistantMessage("A2")
	s.AddUserMessage("Q3")

	if s.TurnCount() != 3 {
		t.Errorf("expected 3 turns, got %d", s.TurnCount())
	}
}

func TestSession_Clear(t *testing.T) {
	s := &Session{
		id: "test-3",
		messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are helpful."},
		},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Hello")
	s.AddAssistantMessage("Hi!")
	s.AddUserMessage("How are you?")

	s.Clear()

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (system), got %d", len(msgs))
	}
	if msgs[0].Role != llmtrace.RoleSystem {
		t.Errorf("expected system message, got %s", msgs[0].Role)
	}
}

func TestSession_Request(t *testing.T) {
	s := &Session{
		id:       "test-4",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Hello")
	s.AddAssistantMessage("Hi!")
	s.AddUserMessage("What's 2+2?")

	req := s.Request("gpt-4o")
	if req.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", req.Model)
	}
	if len(req.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(req.Messages))
	}
}

func TestSession_MaxTurns(t *testing.T) {
	s := &Session{
		id:       "test-5",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
		maxTurns: 3,
	}

	// Add 5 turns
	for i := 0; i < 5; i++ {
		s.AddUserMessage(fmt.Sprintf("Q%d", i))
		s.AddAssistantMessage(fmt.Sprintf("A%d", i))
	}

	// Should have trimmed to 3 turns
	if s.TurnCount() != 3 {
		t.Errorf("expected 3 turns after trim, got %d", s.TurnCount())
	}

	// The last 3 questions should remain
	msgs := s.Messages()
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
	// Q2, A2, Q3, A3, Q4, A4
	if msgs[0].Content != "Q2" {
		t.Errorf("expected Q2, got %s", msgs[0].Content)
	}
}

func TestSession_MaxTurnsWithSystemPrompt(t *testing.T) {
	s := &Session{
		id: "test-6",
		messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are helpful."},
		},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
		maxTurns: 2,
	}

	for i := 0; i < 4; i++ {
		s.AddUserMessage(fmt.Sprintf("Q%d", i))
		s.AddAssistantMessage(fmt.Sprintf("A%d", i))
	}

	msgs := s.Messages()
	// System + 2 turns (4 messages)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
	if msgs[0].Role != llmtrace.RoleSystem {
		t.Errorf("expected system message first, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "You are helpful." {
		t.Errorf("expected system prompt, got %s", msgs[0].Content)
	}
}

func TestSession_TTL(t *testing.T) {
	s := &Session{
		id:       "test-ttl",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
		ttl:      100 * time.Millisecond,
	}

	if s.IsExpired() {
		t.Error("session should not be expired immediately")
	}

	time.Sleep(150 * time.Millisecond)

	if !s.IsExpired() {
		t.Error("session should be expired after TTL")
	}
}

func TestSession_TTLZero(t *testing.T) {
	s := &Session{
		id:       "test-no-ttl",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
		ttl:      0, // no TTL
	}

	time.Sleep(50 * time.Millisecond)

	if s.IsExpired() {
		t.Error("session with zero TTL should never expire")
	}
}

func TestSession_EstimateTokens(t *testing.T) {
	s := &Session{
		id:       "test-tokens",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Hello, this is a test message")  // 28 chars -> ~7 tokens
	s.AddAssistantMessage("Hi there, how can I help?") // 25 chars -> ~6 tokens

	tokens := s.EstimateTokens()
	if tokens < 10 || tokens > 20 {
		t.Errorf("expected ~13 tokens, got %d", tokens)
	}
}

func TestSession_ConcurrentAccess(t *testing.T) {
	s := &Session{
		id:       "test-concurrent",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.AddUserMessage(fmt.Sprintf("Q%d", i))
			} else {
				s.AddAssistantMessage(fmt.Sprintf("A%d", i))
			}
		}(i)
	}
	wg.Wait()

	if len(s.Messages()) != 100 {
		t.Errorf("expected 100 messages, got %d", len(s.Messages()))
	}
}

func TestSession_AddToolMessage(t *testing.T) {
	s := &Session{
		id:       "test-tool",
		messages: []llmtrace.Message{},
		metadata: make(map[string]string),
		created:  time.Now(),
		updated:  time.Now(),
	}

	s.AddUserMessage("Search for Go docs")
	s.AddAssistantMessage("I'll search for that.")
	s.AddToolMessage("Found: https://go.dev/doc/")
	s.AddAssistantMessage("Here are the Go docs: https://go.dev/doc/")

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != llmtrace.RoleTool {
		t.Errorf("expected tool role, got %s", msgs[2].Role)
	}
}

func TestSession_Metadata(t *testing.T) {
	s := &Session{
		id:       "test-meta",
		messages: []llmtrace.Message{},
		metadata: map[string]string{"user_id": "u123", "source": "web"},
		created:  time.Now(),
		updated:  time.Now(),
	}

	m := s.Metadata()
	if m["user_id"] != "u123" {
		t.Errorf("expected user_id u123, got %s", m["user_id"])
	}
	if m["source"] != "web" {
		t.Errorf("expected source web, got %s", m["source"])
	}

	// Verify it's a copy
	m["user_id"] = "changed"
	if s.Metadata()["user_id"] != "u123" {
		t.Error("metadata should be a copy")
	}
}
