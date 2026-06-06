package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atop0914/llmtrace"
)

// --- Test helpers ---

func testRequest() *llmtrace.Request {
	temp := 0.7
	maxTok := 100
	return &llmtrace.Request{
		Model: "gpt-4o",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are a helpful assistant."},
			{Role: llmtrace.RoleUser, Content: "Hello!"},
		},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}
}

func mockAzureServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// --- Unit Tests ---

func TestProviderInterface(t *testing.T) {
	p := New()
	var _ llmtrace.Provider = p
}

func TestName(t *testing.T) {
	p := New()
	if p.Name() != "azure" {
		t.Errorf("Name() = %q, want %q", p.Name(), "azure")
	}
}

func TestDefaultModel(t *testing.T) {
	p := New()
	if p.DefaultModel() != DefaultModel {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), DefaultModel)
	}

	p = New(WithModel("gpt-4"))
	if p.DefaultModel() != "gpt-4" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "gpt-4")
	}
}

func TestSupportsStreaming(t *testing.T) {
	p := New()
	if !p.SupportsStreaming() {
		t.Error("SupportsStreaming() = false, want true")
	}
}

func TestOptions(t *testing.T) {
	p := New(
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("my-deployment"),
		WithAPIKey("test-key"),
		WithAPIVersion("2024-02-01"),
		WithModel("gpt-4"),
	)

	if p.endpoint != "https://myresource.openai.azure.com" {
		t.Errorf("endpoint = %q", p.endpoint)
	}
	if p.deployment != "my-deployment" {
		t.Errorf("deployment = %q", p.deployment)
	}
	if p.apiKey != "test-key" {
		t.Errorf("apiKey = %q", p.apiKey)
	}
	if p.apiVersion != "2024-02-01" {
		t.Errorf("apiVersion = %q", p.apiVersion)
	}
	if p.defaultModel != "gpt-4" {
		t.Errorf("defaultModel = %q", p.defaultModel)
	}
}

func TestWithToken(t *testing.T) {
	p := New(
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("my-deployment"),
		WithToken("my-entra-token"),
	)

	if p.token != "my-entra-token" {
		t.Errorf("token = %q", p.token)
	}
	if p.apiKey != "" {
		t.Errorf("apiKey should be empty when token is set")
	}
}

func TestBuildURL(t *testing.T) {
	p := New(
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4o-deployment"),
		WithAPIVersion("2024-02-01"),
	)

	expected := "https://myresource.openai.azure.com/openai/deployments/gpt-4o-deployment/chat/completions?api-version=2024-02-01"
	if got := p.buildURL(); got != expected {
		t.Errorf("buildURL() = %q, want %q", got, expected)
	}
}

func TestBuildURLEndpointTrailingSlash(t *testing.T) {
	p := New(
		WithEndpoint("https://myresource.openai.azure.com/"),
		WithDeployment("gpt-4o"),
	)

	got := p.buildURL()
	if strings.Contains(got, "azure.com//") {
		t.Errorf("buildURL() has double slash: %q", got)
	}
}

// --- Complete Tests ---

func TestCompleteSuccess(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		// Verify Azure-specific headers
		if r.Header.Get("api-key") != "test-key" {
			t.Errorf("api-key header = %q, want %q", r.Header.Get("api-key"), "test-key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		// Verify URL path contains deployment
		if !strings.Contains(r.URL.Path, "/openai/deployments/test-deployment/") {
			t.Errorf("URL path = %q, want deployment in path", r.URL.Path)
		}

		// Verify API version query param
		if r.URL.Query().Get("api-version") == "" {
			t.Error("missing api-version query parameter")
		}

		// Verify request body
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 {
			t.Errorf("messages count = %d, want 2", len(req.Messages))
		}
		if req.Stream {
			t.Error("stream should be false for Complete")
		}

		resp := chatResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4o",
			Choices: []choice{
				{Index: 0, Message: chatMessage{Role: "assistant", Content: "Hi there!"}, FinishReason: "stop"},
			},
			Usage: usageResponse{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
		WithAPIKey("test-key"),
	)

	resp, err := p.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Content != "Hi there!" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.Provider != "azure" {
		t.Errorf("Provider = %q", resp.Provider)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", resp.Usage.TotalTokens)
	}
}

func TestCompleteWithToken(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		// Token auth should use Authorization header
		if auth := r.Header.Get("Authorization"); auth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer my-token")
		}
		// api-key should NOT be set
		if r.Header.Get("api-key") != "" {
			t.Errorf("api-key should be empty when using token auth")
		}

		resp := chatResponse{
			ID:      "chatcmpl-456",
			Model:   "gpt-4o",
			Choices: []choice{{Index: 0, Message: chatMessage{Role: "assistant", Content: "Hello!"}}},
			Usage:   usageResponse{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
		WithToken("my-token"),
	)

	resp, err := p.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.ID != "chatcmpl-456" {
		t.Errorf("ID = %q", resp.ID)
	}
}

func TestCompleteAPIKeyPriority(t *testing.T) {
	// When both api-key and token are set, api-key should take priority
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "primary-key" {
			t.Errorf("api-key = %q, want %q", r.Header.Get("api-key"), "primary-key")
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization should be empty when api-key is set")
		}

		resp := chatResponse{
			ID: "chatcmpl-789", Model: "gpt-4o",
			Choices: []choice{{Message: chatMessage{Role: "assistant", Content: "OK"}}},
			Usage:   usageResponse{TotalTokens: 1},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
		WithAPIKey("primary-key"),
		WithToken("fallback-token"),
	)

	_, err := p.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
}

func TestCompleteDefaultModel(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := chatResponse{
			ID: "chatcmpl-def", Model: "gpt-4o-mini",
			Choices: []choice{{Message: chatMessage{Role: "assistant", Content: "OK"}}},
			Usage:   usageResponse{TotalTokens: 1},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	// Request without model should not error (Azure uses deployment in URL)
	req := &llmtrace.Request{
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}
	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-4o-mini")
	}
}

func TestCompleteAPIError(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"message":"Deployment not found","type":"invalid_request_error","code":"deployment_not_found"}}`)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("nonexistent"),
	)

	_, err := p.Complete(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
	if apiErr.Code != "deployment_not_found" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "deployment_not_found")
	}
}

func TestCompleteHTTPError(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "Service Unavailable")
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	_, err := p.Complete(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should contain status code 503: %v", err)
	}
}

func TestCompleteEmptyChoices(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			ID: "chatcmpl-empty", Model: "gpt-4o",
			Choices: []choice{},
			Usage:   usageResponse{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	resp, err := p.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if resp.FinishReason != "" {
		t.Errorf("FinishReason = %q, want empty", resp.FinishReason)
	}
}

// --- Stream Tests ---

func TestStreamSuccess(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("stream should be true for Stream")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement Flusher")
		}

		chunks := []string{
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var contents []string
	var lastUsage *llmtrace.Usage
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			contents = append(contents, chunk.Content)
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	full := strings.Join(contents, "")
	if full != "Hello world" {
		t.Errorf("content = %q, want %q", full, "Hello world")
	}

	if lastUsage == nil {
		t.Fatal("expected usage in stream")
	}
	if lastUsage.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", lastUsage.InputTokens)
	}
	if lastUsage.OutputTokens != 2 {
		t.Errorf("OutputTokens = %d, want 2", lastUsage.OutputTokens)
	}
	if lastUsage.TotalTokens != 7 {
		t.Errorf("TotalTokens = %d, want 7", lastUsage.TotalTokens)
	}
}

func TestStreamAPIError(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded","type":"requests","code":"429"}}`)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	_, err := p.Stream(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusTooManyRequests)
	}
}

func TestStreamMalformedJSON(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {invalid json}\n\n")
		flusher.Flush()
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	for chunk := range ch {
		if chunk.Error == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(chunk.Error.Error(), "parse stream chunk") {
			t.Errorf("unexpected error: %v", chunk.Error)
		}
	}
}

func TestStreamEmpty(t *testing.T) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var count int
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 chunks, got %d", count)
	}
}

// --- Build request test ---

func TestBuildRequest(t *testing.T) {
	p := New()
	req := testRequest()
	chatReq := p.buildRequest(req, false)

	if len(chatReq.Messages) != 2 {
		t.Errorf("messages count = %d, want 2", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", chatReq.Messages[0].Role, "system")
	}
	if chatReq.Messages[1].Content != "Hello!" {
		t.Errorf("second message content = %q", chatReq.Messages[1].Content)
	}
	if chatReq.Temperature == nil || *chatReq.Temperature != 0.7 {
		t.Errorf("temperature mismatch")
	}
	if chatReq.MaxTokens == nil || *chatReq.MaxTokens != 100 {
		t.Errorf("max_tokens mismatch")
	}
	if chatReq.Stream {
		t.Error("stream should be false")
	}

	chatReqStream := p.buildRequest(req, true)
	if !chatReqStream.Stream {
		t.Error("stream should be true")
	}
}

// --- APIError tests ---

func TestAPIErrorString(t *testing.T) {
	e := &APIError{
		StatusCode: 404,
		Message:    "Deployment not found",
		Type:       "invalid_request_error",
		Code:       "deployment_not_found",
	}

	s := e.Error()
	if !strings.Contains(s, "404") {
		t.Errorf("error should contain status code: %s", s)
	}
	if !strings.Contains(s, "Deployment not found") {
		t.Errorf("error should contain message: %s", s)
	}
	if !strings.Contains(s, "invalid_request_error") {
		t.Errorf("error should contain type: %s", s)
	}
}

// --- API key via api-key header test ---

func TestSetHeadersAPIKey(t *testing.T) {
	p := New(WithAPIKey("my-key"))
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	p.setHeaders(req)

	if req.Header.Get("api-key") != "my-key" {
		t.Errorf("api-key = %q, want %q", req.Header.Get("api-key"), "my-key")
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty with api-key")
	}
}

func TestSetHeadersToken(t *testing.T) {
	p := New(WithToken("my-token"))
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	p.setHeaders(req)

	if req.Header.Get("Authorization") != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", req.Header.Get("Authorization"), "Bearer my-token")
	}
	if req.Header.Get("api-key") != "" {
		t.Errorf("api-key should be empty with token")
	}
}

func TestSetHeadersNone(t *testing.T) {
	p := New()
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	p.setHeaders(req)

	if req.Header.Get("api-key") != "" {
		t.Errorf("api-key should be empty")
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty")
	}
}

// --- Concurrency test ---

func TestCompleteConcurrent(t *testing.T) {
	var count atomic.Int32
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		resp := chatResponse{
			ID: fmt.Sprintf("chatcmpl-%d", n), Model: "gpt-4o",
			Choices: []choice{{Message: chatMessage{Role: "assistant", Content: "OK"}}},
			Usage:   usageResponse{TotalTokens: 1},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("test-deployment"),
	)

	ctx := context.Background()
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, err := p.Complete(ctx, testRequest())
			errs <- err
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Complete() error: %v", err)
		}
	}

	if n := count.Load(); n != 10 {
		t.Errorf("server received %d requests, want 10", n)
	}
}

// --- Benchmark ---

func BenchmarkComplete(b *testing.B) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			ID: "chatcmpl-bench", Model: "gpt-4o",
			Choices: []choice{{Message: chatMessage{Role: "assistant", Content: "OK"}}},
			Usage:   usageResponse{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("bench-deployment"),
	)

	ctx := context.Background()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Complete(ctx, req)
	}
}

func BenchmarkStream(b *testing.B) {
	server := mockAzureServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer server.Close()

	p := New(
		WithEndpoint(server.URL),
		WithDeployment("bench-deployment"),
	)

	ctx := context.Background()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := p.Stream(ctx, req)
		for range ch {
		}
	}
}

// Suppress unused import warnings
var _ = io.ReadAll
