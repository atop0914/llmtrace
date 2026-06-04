package compat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atop0914/llmtrace"
)

// --- Test helpers ---

func openAIResponse(content string, inputTokens, outputTokens int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-test-123",
			"model": "test-model",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     inputTokens,
				"completion_tokens": outputTokens,
				"total_tokens":      inputTokens + outputTokens,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func streamingResponse(chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		for _, chunk := range chunks {
			data := map[string]any{
				"id":    "chatcmpl-stream-1",
				"model": "test-model",
				"choices": []map[string]any{
					{
						"index": 0,
						"delta": map[string]string{"content": chunk},
					},
				},
			}
			b, _ := json.Marshal(data)
			w.Write([]byte("data: " + string(b) + "\n\n"))
			flusher.Flush()
		}

		// Final chunk with usage
		finalData := map[string]any{
			"id":    "chatcmpl-stream-1",
			"model": "test-model",
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		b, _ := json.Marshal(finalData)
		w.Write([]byte("data: " + string(b) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}
}

// --- Unit Tests ---

func TestNew_Defaults(t *testing.T) {
	p := New()
	if p.Name() != "openai-compat" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai-compat")
	}
	if p.DefaultModel() != "gpt-3.5-turbo" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "gpt-3.5-turbo")
	}
	if !p.SupportsStreaming() {
		t.Error("SupportsStreaming() should be true")
	}
}

func TestNew_WithOptions(t *testing.T) {
	p := New(
		WithName("groq"),
		WithAPIKey("gsk_test"),
		WithBaseURL("https://api.groq.com/openai/v1"),
		WithModel("llama3-70b-8192"),
	)

	if p.Name() != "groq" {
		t.Errorf("Name() = %q, want %q", p.Name(), "groq")
	}
	if p.DefaultModel() != "llama3-70b-8192" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "llama3-70b-8192")
	}
}

func TestComplete_Success(t *testing.T) {
	server := httptest.NewServer(openAIResponse("Hello from vLLM!", 10, 5))
	defer server.Close()

	p := New(
		WithName("vllm"),
		WithBaseURL(server.URL),
		WithModel("llama3"),
	)

	req := &llmtrace.Request{
		Model: "llama3",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Hello from vLLM!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from vLLM!")
	}
	if resp.Provider != "vllm" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "vllm")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Latency <= 0 {
		t.Error("Latency should be positive")
	}
}

func TestComplete_UsesDefaultModel(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &req)
		receivedModel = req.Model

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
		WithModel("custom-default"),
	)

	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if receivedModel != "custom-default" {
		t.Errorf("received model = %q, want %q", receivedModel, "custom-default")
	}
}

func TestComplete_WithParameters(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
	)

	temp := 0.7
	topP := 0.9
	maxTokens := 100
	req := &llmtrace.Request{
		Model: "test-model",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
		Stop:        []string{"STOP"},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if receivedBody["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", receivedBody["temperature"])
	}
	if receivedBody["top_p"] != 0.9 {
		t.Errorf("top_p = %v, want 0.9", receivedBody["top_p"])
	}
	if receivedBody["max_tokens"] != 100.0 { // JSON numbers are float64
		t.Errorf("max_tokens = %v, want 100", receivedBody["max_tokens"])
	}
}

func TestComplete_AuthHeader(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
		WithAPIKey("my-secret-key"),
	)

	req := &llmtrace.Request{
		Model: "test",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if authHeader != "Bearer my-secret-key" {
		t.Errorf("Authorization = %q, want %q", authHeader, "Bearer my-secret-key")
	}
}

func TestComplete_ExtraHeaders(t *testing.T) {
	var customHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customHeader = r.Header.Get("X-Custom-Header")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
		WithExtraHeader("X-Custom-Header", "custom-value"),
	)

	req := &llmtrace.Request{
		Model: "test",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if customHeader != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", customHeader, "custom-value")
	}
}

func TestComplete_APIError(t *testing.T) {
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

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
		WithAPIKey("bad-key"),
	)

	req := &llmtrace.Request{
		Model: "test",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if apiErr.Provider != "test" {
		t.Errorf("Provider = %q, want %q", apiErr.Provider, "test")
	}
	if apiErr.Message != "Invalid API key" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "Invalid API key")
	}
}

func TestComplete_NonJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
	)

	req := &llmtrace.Request{
		Model: "test",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code 500: %v", err)
	}
}

func TestStream_Success(t *testing.T) {
	server := httptest.NewServer(streamingResponse([]string{"Hello", " from", " Groq!"}))
	defer server.Close()

	p := New(
		WithName("groq"),
		WithBaseURL(server.URL),
		WithModel("llama3"),
	)

	req := &llmtrace.Request{
		Model: "llama3",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content strings.Builder
	var finalUsage *llmtrace.Usage
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
		}
		if chunk.Usage != nil {
			finalUsage = chunk.Usage
		}
	}

	if content.String() != "Hello from Groq!" {
		t.Errorf("content = %q, want %q", content.String(), "Hello from Groq!")
	}
	if finalUsage == nil {
		t.Fatal("expected usage in final chunk")
	}
	if finalUsage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", finalUsage.InputTokens)
	}
	if finalUsage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", finalUsage.OutputTokens)
	}
}

func TestStream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
				"code":    "rate_limit_exceeded",
			},
		})
	}))
	defer server.Close()

	p := New(
		WithName("test"),
		WithBaseURL(server.URL),
	)

	req := &llmtrace.Request{
		Model: "test",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	_, err := p.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusTooManyRequests)
	}
}

// --- Provider interface compliance ---

func TestProvider_InterfaceCompliance(t *testing.T) {
	var _ llmtrace.Provider = New()
	var _ llmtrace.Provider = New(WithName("test"), WithBaseURL("http://localhost"))
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Provider:   "groq",
		StatusCode: 429,
		Message:    "Rate limit exceeded",
		Type:       "rate_limit_error",
		Code:       "rate_limit_exceeded",
	}

	msg := err.Error()
	if !strings.Contains(msg, "groq") {
		t.Errorf("error should contain provider name: %s", msg)
	}
	if !strings.Contains(msg, "429") {
		t.Errorf("error should contain status code: %s", msg)
	}
	if !strings.Contains(msg, "Rate limit exceeded") {
		t.Errorf("error should contain message: %s", msg)
	}
}

// --- Benchmarks ---

func BenchmarkComplete(b *testing.B) {
	server := httptest.NewServer(openAIResponse("ok", 10, 5))
	defer server.Close()

	p := New(
		WithName("bench"),
		WithBaseURL(server.URL),
		WithModel("test-model"),
	)

	req := &llmtrace.Request{
		Model: "test-model",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStream(b *testing.B) {
	server := httptest.NewServer(streamingResponse([]string{"a", "b", "c"}))
	defer server.Close()

	p := New(
		WithName("bench"),
		WithBaseURL(server.URL),
		WithModel("test-model"),
	)

	req := &llmtrace.Request{
		Model: "test-model",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, err := p.Stream(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		for range ch {
		}
	}
}
