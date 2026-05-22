// Package gollm provides OpenTelemetry-native observability for LLM calls.
//
// gollm wraps LLM client calls with OpenTelemetry spans, capturing
// token usage, latency, cost, and request/response metadata following
// the OpenTelemetry GenAI semantic conventions.
//
// Usage:
//
//	tracer := gollm.NewTracer("my-service")
//	resp, err := tracer.Chat(ctx, &gollm.Request{
//	    Model:    "gpt-4",
//	    Messages: []gollm.Message{{Role: "user", Content: "Hello"}},
//	}, openai.Complete)
package gollm

import (
	"context"
	"time"
)

// Role represents a message role in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    Role
	Content string
}

// Request represents an LLM completion request.
type Request struct {
	// Model is the model identifier (e.g. "gpt-4", "claude-3-opus").
	Model string

	// Messages is the conversation history.
	Messages []Message

	// Temperature controls randomness (0.0-2.0).
	Temperature *float64

	// TopP controls nucleus sampling (0.0-1.0).
	TopP *float64

	// MaxTokens limits the response length.
	MaxTokens *int

	// Stop sequences.
	Stop []string

	// Extra provider-specific fields.
	Extra map[string]any
}

// Response represents an LLM completion response.
type Response struct {
	// ID is the provider's request ID.
	ID string

	// Model is the model that actually served the request.
	Model string

	// Content is the generated text.
	Content string

	// FinishReason indicates why generation stopped.
	FinishReason string

	// Usage tracks token counts.
	Usage Usage

	// Latency is the total request duration.
	Latency time.Duration

	// Provider is the provider name (openai, anthropic, gemini).
	Provider string
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// CompleteFunc is the actual LLM API call function that gollm wraps.
// It takes a context and request, returns a response.
// Providers implement this to make the real API call.
type CompleteFunc func(ctx context.Context, req *Request) (*Response, error)

// StreamFunc is the streaming variant of CompleteFunc.
// It returns a channel of partial responses.
type StreamFunc func(ctx context.Context, req *Request) (<-chan StreamChunk, error)

// StreamChunk represents a partial response in a stream.
type StreamChunk struct {
	Content string
	Usage   *Usage // Only set in the final chunk
	Error   error
}

// CostEntry represents the cost per token for a model.
type CostEntry struct {
	// InputCostPer1K is the cost per 1000 input tokens.
	InputCostPer1K float64

	// OutputCostPer1K is the cost per 1000 output tokens.
	OutputCostPer1K float64
}

// Float64Ptr is a helper to create a *float64.
func Float64Ptr(v float64) *float64 { return &v }

// IntPtr is a helper to create a *int.
func IntPtr(v int) *int { return &v }
