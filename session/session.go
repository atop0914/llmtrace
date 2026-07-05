// Package session provides multi-turn conversation session tracking for LLM calls.
//
// Sessions maintain conversation history across multiple LLM calls,
// enabling context-aware interactions with automatic message management,
// token counting, and configurable TTL-based expiration.
//
// Usage:
//
//	mgr := session.NewManager(session.WithMaxTurns(50))
//	sess := mgr.Create(session.WithSystemPrompt("You are a helpful assistant."))
//	sess.AddUserMessage("Hello!")
//	resp, err := tracer.Chat(ctx, sess.Request("gpt-4o"), provider)
//	sess.AddAssistantMessage(resp.Content)
package session

import (
	"sync"
	"time"

	"github.com/atop0914/llmtrace"
)

// Session represents a multi-turn conversation session.
type Session struct {
	mu       sync.RWMutex
	id       string
	messages []llmtrace.Message
	metadata map[string]string
	created  time.Time
	updated  time.Time
	ttl      time.Duration
	maxTurns int
}

// SessionConfig configures session creation.
type SessionConfig struct {
	SystemPrompt string
	Metadata     map[string]string
	TTL          time.Duration
	MaxTurns     int
}

// SessionOption configures a session.
type SessionOption func(*SessionConfig)

// WithSystemPrompt sets the system prompt for the session.
func WithSystemPrompt(prompt string) SessionOption {
	return func(c *SessionConfig) {
		c.SystemPrompt = prompt
	}
}

// WithMetadata adds metadata to the session.
func WithMetadata(key, value string) SessionOption {
	return func(c *SessionConfig) {
		if c.Metadata == nil {
			c.Metadata = make(map[string]string)
		}
		c.Metadata[key] = value
	}
}

// WithSessionTTL sets the time-to-live for the session.
func WithSessionTTL(ttl time.Duration) SessionOption {
	return func(c *SessionConfig) {
		c.TTL = ttl
	}
}

// WithMaxTurns sets the maximum number of turns (user+assistant pairs) to keep.
func WithMaxTurns(n int) SessionOption {
	return func(c *SessionConfig) {
		c.MaxTurns = n
	}
}

// ID returns the session ID.
func (s *Session) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// Messages returns a copy of the conversation history.
func (s *Session) Messages() []llmtrace.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := make([]llmtrace.Message, len(s.messages))
	copy(msgs, s.messages)
	return msgs
}

// Metadata returns the session metadata.
func (s *Session) Metadata() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]string, len(s.metadata))
	for k, v := range s.metadata {
		m[k] = v
	}
	return m
}

// Created returns when the session was created.
func (s *Session) Created() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.created
}

// Updated returns when the session was last updated.
func (s *Session) Updated() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updated
}

// IsExpired checks if the session has exceeded its TTL.
func (s *Session) IsExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ttl <= 0 {
		return false
	}
	return time.Since(s.updated) > s.ttl
}

// AddMessage adds a message to the session.
func (s *Session) AddMessage(msg llmtrace.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	s.updated = time.Now()
	s.trimIfNeeded()
}

// AddUserMessage adds a user message to the session.
func (s *Session) AddUserMessage(content string) {
	s.AddMessage(llmtrace.Message{Role: llmtrace.RoleUser, Content: content})
}

// AddAssistantMessage adds an assistant message to the session.
func (s *Session) AddAssistantMessage(content string) {
	s.AddMessage(llmtrace.Message{Role: llmtrace.RoleAssistant, Content: content})
}

// AddToolMessage adds a tool message to the session.
func (s *Session) AddToolMessage(content string) {
	s.AddMessage(llmtrace.Message{Role: llmtrace.RoleTool, Content: content})
}

// TurnCount returns the number of user messages (turns) in the session.
func (s *Session) TurnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, msg := range s.messages {
		if msg.Role == llmtrace.RoleUser {
			count++
		}
	}
	return count
}

// Clear removes all messages from the session (except system prompt).
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	var system []llmtrace.Message
	for _, msg := range s.messages {
		if msg.Role == llmtrace.RoleSystem {
			system = append(system, msg)
		}
	}
	s.messages = system
	s.updated = time.Now()
}

// Request creates an llmtrace.Request from the session's conversation history.
func (s *Session) Request(model string) *llmtrace.Request {
	return &llmtrace.Request{
		Model:    model,
		Messages: s.Messages(),
	}
}

// trimIfNeeded removes old messages if maxTurns is exceeded.
// Must be called with mu held.
func (s *Session) trimIfNeeded() {
	if s.maxTurns <= 0 {
		return
	}

	// Count user messages
	userCount := 0
	for _, msg := range s.messages {
		if msg.Role == llmtrace.RoleUser {
			userCount++
		}
	}

	if userCount <= s.maxTurns {
		return
	}

	// Find the system messages to preserve
	var system []llmtrace.Message
	for _, msg := range s.messages {
		if msg.Role == llmtrace.RoleSystem {
			system = append(system, msg)
		}
	}

	// Keep the last maxTurns user messages and their assistant responses
	excess := userCount - s.maxTurns
	trimmed := make([]llmtrace.Message, 0, len(s.messages))
	skipUntilAssistant := false
	userSeen := 0

	for _, msg := range s.messages {
		if msg.Role == llmtrace.RoleSystem {
			continue // handled separately
		}
		if msg.Role == llmtrace.RoleUser {
			userSeen++
			if userSeen <= excess {
				skipUntilAssistant = true
				continue
			}
			skipUntilAssistant = false
		}
		if skipUntilAssistant {
			continue
		}
		trimmed = append(trimmed, msg)
	}

	s.messages = append(system, trimmed...)
}

// EstimateTokens provides a rough estimate of token count for the session.
// Uses a simple heuristic: ~4 chars per token.
func (s *Session) EstimateTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, msg := range s.messages {
		total += len(msg.Content)
	}
	return total / 4
}
