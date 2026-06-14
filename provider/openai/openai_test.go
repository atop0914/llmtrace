package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atop0914/llmtrace"
)

func TestProvider_Name(t *testing.T) {
	p := New()
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestProvider_DefaultModel(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"default", nil, DefaultModel},
		{"custom", []Option{WithModel("gpt-4")}, "gpt-4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts...)
			if got := p.DefaultModel(); got != tt.want {
				t.Errorf("DefaultModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProvider_SupportsStreaming(t *testing.T) {
	p := New()
	if !p.SupportsStreaming() {
		t.Error("SupportsStreaming() = false, want true")
	}
}

func TestProvider_Complete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", req.Model)
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages count = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want user", req.Messages[0].Role)
		}

		resp := chatResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4o",
			Choices: []choice{
				{
					Index:        0,
					Message:      chatMessage{Role: "assistant", Content: "Hello!"},
					FinishReason: "stop",
				},
			},
			Usage: usageResponse{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
	)

	req := &llmtrace.Request{
		Model: "gpt-4o",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.ID != "chatcmpl-123" {
		t.Errorf("ID = %q, want chatcmpl-123", resp.ID)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want Hello!", resp.Content)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", resp.Provider)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestProvider_Complete_WithOptionalParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Error("temperature not set correctly")
		}
		if req.TopP == nil || *req.TopP != 0.9 {
			t.Error("top_p not set correctly")
		}
		if req.MaxTokens == nil || *req.MaxTokens != 100 {
			t.Error("max_tokens not set correctly")
		}
		if len(req.Stop) != 1 || req.Stop[0] != "\n" {
			t.Errorf("stop = %v, want [\\n]", req.Stop)
		}

		json.NewEncoder(w).Encode(chatResponse{
			Model:   "gpt-4o",
			Choices: []choice{{Message: chatMessage{Content: "OK"}}},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	temp := 0.7
	topP := 0.9
	maxTok := 100

	req := &llmtrace.Request{
		Model:       "gpt-4o",
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTok,
		Stop:        []string{"\n"},
		Messages:    []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestProvider_Complete_UsesDefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != DefaultModel {
			t.Errorf("model = %q, want %q", req.Model, DefaultModel)
		}
		json.NewEncoder(w).Encode(chatResponse{Model: req.Model})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestProvider_Complete_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid API key",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status code = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Message != "Invalid API key" {
		t.Errorf("message = %q, want 'Invalid API key'", apiErr.Message)
	}
}

func TestProvider_Complete_GenericHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status 500: %v", err)
	}
}

func TestProvider_Complete_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(chatResponse{
			ID:      "chatcmpl-empty",
			Model:   "gpt-4o",
			Choices: []choice{},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
}

func TestProvider_Stream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world!"},"finish_reason":null}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "%s\n\n", c)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var chunks []llmtrace.StreamChunk
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "" {
		t.Errorf("chunk 0 content = %q, want empty", chunks[0].Content)
	}
	if chunks[1].Content != "Hello" {
		t.Errorf("chunk 1 content = %q, want Hello", chunks[1].Content)
	}
	if chunks[2].Content != " world!" {
		t.Errorf("chunk 2 content = %q, want ' world!'", chunks[2].Content)
	}
	// Usage should be on the last chunk
	if chunks[2].Usage == nil {
		t.Fatal("expected usage on last chunk")
	}
	if chunks[2].Usage.TotalTokens != 7 {
		t.Errorf("total tokens = %d, want 7", chunks[2].Usage.TotalTokens)
	}
}

func TestProvider_Stream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid request",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := err.(*APIError); !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
}

func TestProvider_Complete_MultipleMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 3 {
			t.Errorf("messages count = %d, want 3", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("msg 0 role = %q, want system", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "user" {
			t.Errorf("msg 1 role = %q, want user", req.Messages[1].Role)
		}
		if req.Messages[2].Role != "assistant" {
			t.Errorf("msg 2 role = %q, want assistant", req.Messages[2].Role)
		}
		json.NewEncoder(w).Encode(chatResponse{
			Model:   "gpt-4o",
			Choices: []choice{{Message: chatMessage{Content: "OK"}}},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "gpt-4o",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are helpful"},
			{Role: llmtrace.RoleUser, Content: "Hi"},
			{Role: llmtrace.RoleAssistant, Content: "Hello!"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestAPIError_Error(t *testing.T) {
	e := &APIError{
		StatusCode: 429,
		Message:    "Rate limit exceeded",
		Type:       "rate_limit_error",
		Code:       "rate_limit_exceeded",
	}
	s := e.Error()
	if !strings.Contains(s, "429") {
		t.Errorf("error should contain 429: %s", s)
	}
	if !strings.Contains(s, "Rate limit exceeded") {
		t.Errorf("error should contain message: %s", s)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check that Provider implements llmtrace.Provider
	var _ llmtrace.Provider = New()
}
