package anthropic

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
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestProvider_DefaultModel(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"default", nil, DefaultModel},
		{"custom", []Option{WithModel("claude-3-haiku-20240307")}, "claude-3-haiku-20240307"},
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
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), apiVersion)
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("model = %q, want claude-sonnet-4-20250514", req.Model)
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages count = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want user", req.Messages[0].Role)
		}
		if req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %d, want 4096 (default)", req.MaxTokens)
		}

		resp := messagesResponse{
			ID:         "msg_123",
			Model:      "claude-sonnet-4-20250514",
			Content:    []contentBlock{{Type: "text", Text: "Hello!"}},
			StopReason: "end_turn",
			Usage: usageResponse{
				InputTokens:  10,
				OutputTokens: 5,
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
		Model: "claude-sonnet-4-20250514",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.ID != "msg_123" {
		t.Errorf("ID = %q, want msg_123", resp.ID)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want Hello!", resp.Content)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", resp.Model)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want end_turn", resp.FinishReason)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", resp.Provider)
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
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Error("temperature not set correctly")
		}
		if req.TopP == nil || *req.TopP != 0.9 {
			t.Error("top_p not set correctly")
		}
		if req.MaxTokens != 100 {
			t.Errorf("max_tokens = %d, want 100", req.MaxTokens)
		}
		if len(req.StopSequences) != 1 || req.StopSequences[0] != "\n" {
			t.Errorf("stop_sequences = %v, want [\\n]", req.StopSequences)
		}

		json.NewEncoder(w).Encode(messagesResponse{
			Model:   "claude-sonnet-4-20250514",
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 5, OutputTokens: 2},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	temp := 0.7
	topP := 0.9
	maxTok := 100

	req := &llmtrace.Request{
		Model:       "claude-sonnet-4-20250514",
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
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != DefaultModel {
			t.Errorf("model = %q, want %q", req.Model, DefaultModel)
		}
		json.NewEncoder(w).Encode(messagesResponse{
			Model:   req.Model,
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 5, OutputTokens: 2},
		})
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

func TestProvider_Complete_WithSystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.System != "You are helpful" {
			t.Errorf("system = %q, want 'You are helpful'", req.System)
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages count = %d, want 1 (system extracted)", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want user", req.Messages[0].Role)
		}

		json.NewEncoder(w).Encode(messagesResponse{
			Model:   "claude-sonnet-4-20250514",
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 10, OutputTokens: 3},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are helpful"},
			{Role: llmtrace.RoleUser, Content: "Hi"},
		},
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
			"type": "authentication_error",
			"error": map[string]any{
				"type":    "authentication_error",
				"message": "Invalid API key",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
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
		Model:    "claude-sonnet-4-20250514",
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

func TestProvider_Complete_MultipleContentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(messagesResponse{
			ID:    "msg_multi",
			Model: "claude-sonnet-4-20250514",
			Content: []contentBlock{
				{Type: "text", Text: "First part. "},
				{Type: "text", Text: "Second part."},
			},
			StopReason: "end_turn",
			Usage:      usageResponse{InputTokens: 5, OutputTokens: 10},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "First part. Second part." {
		t.Errorf("Content = %q, want 'First part. Second part.'", resp.Content)
	}
}

func TestProvider_Complete_MultipleMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		// system is extracted, so 2 messages remain (user + assistant)
		if len(req.Messages) != 2 {
			t.Errorf("messages count = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("msg 0 role = %q, want user", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "assistant" {
			t.Errorf("msg 1 role = %q, want assistant", req.Messages[1].Role)
		}
		json.NewEncoder(w).Encode(messagesResponse{
			Model:   "claude-sonnet-4-20250514",
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 10, OutputTokens: 3},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "claude-sonnet-4-20250514",
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

func TestProvider_Stream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_s1\",\"model\":\"claude-sonnet-4-20250514\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world!\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
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

	// 2 content chunks + 1 usage chunk = 3
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunk 0 content = %q, want Hello", chunks[0].Content)
	}
	if chunks[1].Content != " world!" {
		t.Errorf("chunk 1 content = %q, want ' world!'", chunks[1].Content)
	}
	// Last chunk should have usage
	if chunks[2].Usage == nil {
		t.Fatal("expected usage on last chunk")
	}
	if chunks[2].Usage.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10", chunks[2].Usage.InputTokens)
	}
	if chunks[2].Usage.OutputTokens != 5 {
		t.Errorf("output tokens = %d, want 5", chunks[2].Usage.OutputTokens)
	}
	if chunks[2].Usage.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", chunks[2].Usage.TotalTokens)
	}
}

func TestProvider_Stream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "invalid_request_error",
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "Invalid request",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
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

func TestProvider_Complete_DefaultMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.MaxTokens != 4096 {
			t.Errorf("max_tokens = %d, want 4096 (default)", req.MaxTokens)
		}
		json.NewEncoder(w).Encode(messagesResponse{
			Model:   "claude-sonnet-4-20250514",
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 5, OutputTokens: 2},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
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
	}
	s := e.Error()
	if !strings.Contains(s, "429") {
		t.Errorf("error should contain 429: %s", s)
	}
	if !strings.Contains(s, "Rate limit exceeded") {
		t.Errorf("error should contain message: %s", s)
	}
	if !strings.Contains(s, "anthropic") {
		t.Errorf("error should contain 'anthropic': %s", s)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check that Provider implements llmtrace.Provider
	var _ llmtrace.Provider = New()
}

func TestProvider_SetHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("x-api-key = %q, want sk-ant-test", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), apiVersion)
		}

		json.NewEncoder(w).Encode(messagesResponse{
			Model:   "claude-sonnet-4-20250514",
			Content: []contentBlock{{Type: "text", Text: "OK"}},
			Usage:   usageResponse{InputTokens: 5, OutputTokens: 2},
		})
	}))
	defer server.Close()

	p2 := New(WithAPIKey("sk-ant-test"), WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "claude-sonnet-4-20250514",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	_, err := p2.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}
