package proxy

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

// --- Mock provider ---

type mockProvider struct {
	name         string
	defaultModel string
	completeFunc func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error)
	streamFunc   func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error)
}

func (m *mockProvider) Name() string              { return m.name }
func (m *mockProvider) DefaultModel() string       { return m.defaultModel }
func (m *mockProvider) SupportsStreaming() bool     { return true }
func (m *mockProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &llmtrace.Response{
		ID:           "test-id",
		Model:        req.Model,
		Content:      "Hello from " + m.name,
		FinishReason: "stop",
		Usage:        llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (m *mockProvider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	ch := make(chan llmtrace.StreamChunk, 3)
	go func() {
		defer close(ch)
		ch <- llmtrace.StreamChunk{Content: "Hello "}
		ch <- llmtrace.StreamChunk{Content: "world"}
		ch <- llmtrace.StreamChunk{
			Content: "",
			Usage:   &llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		}
	}()
	return ch, nil
}

func newTestServer(t *testing.T, providers map[string]ProviderEntry, apiKey string) *Server {
	t.Helper()
	return New(Config{
		Listen:    ":0",
		Providers: providers,
		APIKey:    apiKey,
	})
}

// --- Tests ---

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestModelsEndpoint(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	anthropic := &mockProvider{name: "anthropic"}

	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt-4o":  {Provider: openai},
		"claude":  {Provider: anthropic},
	}, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp modelsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Object != "list" {
		t.Errorf("expected object=list, got %s", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(resp.Data))
	}
	// Models should be sorted
	if resp.Data[0].ID != "claude" {
		t.Errorf("expected first model=claude, got %s", resp.Data[0].ID)
	}
	if resp.Data[1].ID != "gpt-4o" {
		t.Errorf("expected second model=gpt-4o, got %s", resp.Data[1].ID)
	}
}

func TestChatCompletions_NonStreaming(t *testing.T) {
	provider := &mockProvider{name: "openai", defaultModel: "gpt-4o-mini"}
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: provider, Default: true},
	}, "")

	body := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello"}],
		"temperature": 0.7
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp chatResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Object != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %s", resp.Object)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello from openai" {
		t.Errorf("expected content 'Hello from openai', got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected completion_tokens=5, got %d", resp.Usage.CompletionTokens)
	}
}

func TestChatCompletions_Streaming(t *testing.T) {
	provider := &mockProvider{name: "openai"}
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: provider, Default: true},
	}, "")

	body := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type=text/event-stream, got %s", ct)
	}

	bodyStr := w.Body.String()
	lines := strings.Split(strings.TrimSpace(bodyStr), "\n")

	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	// Should have 3 chunks + [DONE]
	if len(dataLines) != 4 {
		t.Fatalf("expected 4 data lines (3 chunks + DONE), got %d: %v", len(dataLines), dataLines)
	}

	if dataLines[3] != "[DONE]" {
		t.Errorf("expected last line to be [DONE], got %s", dataLines[3])
	}

	// Parse first chunk
	var sc streamChunk
	json.Unmarshal([]byte(dataLines[0]), &sc)
	if sc.Object != "chat.completion.chunk" {
		t.Errorf("expected object=chat.completion.chunk, got %s", sc.Object)
	}
	if len(sc.Choices) != 1 {
		t.Fatalf("expected 1 choice in chunk, got %d", len(sc.Choices))
	}
	if sc.Choices[0].Delta.Content != "Hello " {
		t.Errorf("expected delta content='Hello ', got %q", sc.Choices[0].Delta.Content)
	}
}

func TestChatCompletions_ModelRouting(t *testing.T) {
	openai := &mockProvider{
		name: "openai",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				ID:      "openai-id",
				Model:   req.Model,
				Content: "OpenAI response",
				Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
	anthropic := &mockProvider{
		name: "anthropic",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				ID:      "anthropic-id",
				Model:   req.Model,
				Content: "Anthropic response",
				Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}

	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt":    {Provider: openai},
		"claude": {Provider: anthropic},
	}, "")

	tests := []struct {
		model    string
		expected string
	}{
		{"gpt-4o", "OpenAI response"},
		{"claude-3-opus", "Anthropic response"},
		{"gpt-3.5-turbo", "OpenAI response"},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			body := fmt.Sprintf(`{"model": %q, "messages": [{"role": "user", "content": "Hi"}]}`, tc.model)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp chatResponse
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Choices[0].Message.Content != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, resp.Choices[0].Message.Content)
			}
		})
	}
}

func TestChatCompletions_DefaultProvider(t *testing.T) {
	defaultP := &mockProvider{
		name: "fallback",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				ID:      "fallback-id",
				Model:   req.Model,
				Content: "Fallback response",
				Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}

	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt":     {Provider: &mockProvider{name: "openai"}},
		"special": {Provider: defaultP, Default: true},
	}, "")

	body := `{"model": "unknown-model", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp chatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Choices[0].Message.Content != "Fallback response" {
		t.Errorf("expected fallback, got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatCompletions_NoProviderFound(t *testing.T) {
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: &mockProvider{name: "openai"}},
	}, "")

	body := `{"model": "unknown-model", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiError
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Type != "invalid_request" {
		t.Errorf("expected error type=invalid_request, got %s", resp.Error.Type)
	}
}

func TestAuth_Required(t *testing.T) {
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: &mockProvider{name: "openai"}},
	}, "test-key-123")

	// No auth header
	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuth_InvalidKey(t *testing.T) {
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: &mockProvider{name: "openai"}},
	}, "test-key-123")

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()

	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuth_ValidKey(t *testing.T) {
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: &mockProvider{name: "openai"}, Default: true},
	}, "test-key-123")

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()

	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuth_HealthAlwaysOpen(t *testing.T) {
	srv := newTestServer(t, nil, "test-key-123")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for health, got %d", w.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestEmptyMessages(t *testing.T) {
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: &mockProvider{name: "openai"}, Default: true},
	}, "")

	body := `{"model": "gpt-4o", "messages": []}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvalidJSON(t *testing.T) {
	srv := newTestServer(t, nil, "")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestProviderError(t *testing.T) {
	provider := &mockProvider{
		name: "openai",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return nil, fmt.Errorf("rate limit exceeded")
		},
	}
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: provider, Default: true},
	}, "")

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}

	var resp apiError
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Type != "upstream_error" {
		t.Errorf("expected error type=upstream_error, got %s", resp.Error.Type)
	}
}

func TestFindProvider_PrefixMatch(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	gpt4 := &mockProvider{name: "gpt4-specific"}

	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt-4o": {Provider: gpt4},
		"gpt":    {Provider: openai},
	}, "")

	tests := []struct {
		model    string
		expected string
	}{
		{"gpt-4o", "gpt4-specific"}, // exact match wins
		{"gpt-4o-mini", "gpt4-specific"}, // prefix: "gpt-4o" > "gpt"
		{"gpt-3.5-turbo", "openai"}, // prefix: "gpt"
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			p, _ := srv.findProvider(tc.model)
			if p == nil {
				t.Fatal("expected provider, got nil")
			}
			if p.Name() != tc.expected {
				t.Errorf("expected provider=%s, got %s", tc.expected, p.Name())
			}
		})
	}
}

func TestReadSSELines(t *testing.T) {
	input := "data: chunk1\n\ndata: chunk2\n\ndata: [DONE]\n\n"
	lines, err := readSSELines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "chunk1" {
		t.Errorf("expected 'chunk1', got %q", lines[0])
	}
	if lines[2] != "[DONE]" {
		t.Errorf("expected '[DONE]', got %q", lines[2])
	}
}

func TestStreamError(t *testing.T) {
	provider := &mockProvider{
		name: "openai",
		streamFunc: func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			return nil, fmt.Errorf("stream setup failed")
		},
	}
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: provider, Default: true},
	}, "")

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}], "stream": true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SSE, got %d", w.Code)
	}

	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "stream setup failed") {
		t.Errorf("expected error message in SSE stream, got: %s", bodyStr)
	}
}

func TestStreamChunkError(t *testing.T) {
	provider := &mockProvider{
		name: "openai",
		streamFunc: func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			ch := make(chan llmtrace.StreamChunk, 2)
			go func() {
				defer close(ch)
				ch <- llmtrace.StreamChunk{Content: "ok"}
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("chunk error")}
			}()
			return ch, nil
		},
	}
	srv := newTestServer(t, map[string]ProviderEntry{
		"gpt": {Provider: provider, Default: true},
	}, "")

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}], "stream": true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "chunk error") {
		t.Errorf("expected chunk error in SSE stream, got: %s", bodyStr)
	}
}

func TestToLLMRequest(t *testing.T) {
	srv := newTestServer(t, nil, "")
	temp := 0.7
	maxTok := 100

	apiReq := &chatRequest{
		Model: "gpt-4o",
		Messages: []chatMessage{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
		},
		Temperature: &temp,
		MaxTokens:   &maxTok,
		Stop:        []string{"END"},
	}

	llmReq := srv.toLLMRequest(apiReq)

	if llmReq.Model != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %s", llmReq.Model)
	}
	if len(llmReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(llmReq.Messages))
	}
	if llmReq.Messages[0].Role != llmtrace.RoleSystem {
		t.Errorf("expected role=system, got %s", llmReq.Messages[0].Role)
	}
	if llmReq.Temperature == nil || *llmReq.Temperature != 0.7 {
		t.Errorf("expected temperature=0.7")
	}
	if llmReq.MaxTokens == nil || *llmReq.MaxTokens != 100 {
		t.Errorf("expected max_tokens=100")
	}
	if len(llmReq.Stop) != 1 || llmReq.Stop[0] != "END" {
		t.Errorf("expected stop=[END]")
	}
}

func TestToAPIResponse(t *testing.T) {
	srv := newTestServer(t, nil, "")
	resp := &llmtrace.Response{
		ID:           "test-123",
		Model:        "gpt-4o",
		Content:      "Hello!",
		FinishReason: "stop",
		Usage:        llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}

	apiResp := srv.toAPIResponse(resp, "gpt-4o")

	if apiResp.ID != "test-123" {
		t.Errorf("expected ID=test-123, got %s", apiResp.ID)
	}
	if apiResp.Object != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %s", apiResp.Object)
	}
	if apiResp.Model != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %s", apiResp.Model)
	}
	if apiResp.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", apiResp.Choices[0].Message.Role)
	}
	if apiResp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected content=Hello!, got %s", apiResp.Choices[0].Message.Content)
	}
	if apiResp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", apiResp.Usage.PromptTokens)
	}
}

// --- Benchmarks ---

func BenchmarkChatCompletions(b *testing.B) {
	provider := &mockProvider{
		name: "openai",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				ID:      "bench-id",
				Model:   req.Model,
				Content: "benchmark response",
				Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
	srv := New(Config{
		Listen: ":0",
		Providers: map[string]ProviderEntry{
			"gpt": {Provider: provider, Default: true},
		},
	})

	body := `{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello"}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", w.Code)
		}
	}
}

func BenchmarkFindProvider(b *testing.B) {
	providers := map[string]ProviderEntry{
		"gpt-4o":      {Provider: &mockProvider{name: "openai"}},
		"gpt-3.5":     {Provider: &mockProvider{name: "openai"}},
		"claude":      {Provider: &mockProvider{name: "anthropic"}},
		"gemini":      {Provider: &mockProvider{name: "gemini"}},
		"mistral":     {Provider: &mockProvider{name: "mistral"}, Default: true},
	}
	srv := New(Config{Providers: providers})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv.findProvider("gpt-4o-mini")
	}
}
