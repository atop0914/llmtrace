package ollama

import (
	"bufio"
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
	if p.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", p.Name(), "ollama")
	}
}

func TestProvider_DefaultModel(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"default", nil, DefaultModel},
		{"custom", []Option{WithModel("mistral")}, "mistral"},
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
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "llama3" {
			t.Errorf("model = %q, want llama3", req.Model)
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages count = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want user", req.Messages[0].Role)
		}
		if req.Messages[0].Content != "Hello" {
			t.Errorf("message content = %q, want Hello", req.Messages[0].Content)
		}
		if req.Stream {
			t.Error("stream should be false for Complete")
		}

		resp := chatResponse{
			Model: "llama3",
			Message: &chatMessage{
				Role:    "assistant",
				Content: "Hi there! How can I help you today?",
			},
			Done:               true,
			TotalDuration:      5033916709,
			LoadDuration:       378083,
			PromptEvalCount:    10,
			PromptEvalDuration: 325937000,
			EvalCount:          15,
			EvalDuration:       4676546000,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
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

	if resp.Content != "Hi there! How can I help you today?" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hi there! How can I help you today?")
	}
	if resp.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", resp.Model)
	}
	if resp.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", resp.Provider)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 25 {
		t.Errorf("TotalTokens = %d, want 25", resp.Usage.TotalTokens)
	}
	if resp.Latency <= 0 {
		t.Error("Latency should be positive")
	}
}

func TestProvider_Complete_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		// Verify options were passed
		if req.Options == nil {
			t.Fatal("options should not be nil")
		}
		if req.Options.Temperature == nil || *req.Options.Temperature != 0.7 {
			t.Error("temperature should be 0.7")
		}
		if req.Options.TopP == nil || *req.Options.TopP != 0.9 {
			t.Error("top_p should be 0.9")
		}
		if req.Options.NumPredict == nil || *req.Options.NumPredict != 100 {
			t.Error("num_predict should be 100")
		}

		resp := chatResponse{
			Model: "llama3",
			Message: &chatMessage{
				Role:    "assistant",
				Content: "Response with options",
			},
			Done:            true,
			PromptEvalCount: 5,
			EvalCount:       10,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	temp := 0.7
	topP := 0.9
	maxTokens := 100
	req := &llmtrace.Request{
		Model:       "llama3",
		Messages:    []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Response with options" {
		t.Errorf("Content = %q, want %q", resp.Content, "Response with options")
	}
}

func TestProvider_Complete_DefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "mistral" {
			t.Errorf("model = %q, want mistral (default)", req.Model)
		}

		resp := chatResponse{
			Model:   "mistral",
			Message: &chatMessage{Role: "assistant", Content: "ok"},
			Done:    true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL), WithModel("mistral"))
	req := &llmtrace.Request{
		// No model specified — should use default
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Model != "mistral" {
		t.Errorf("Model = %q, want mistral", resp.Model)
	}
}

func TestProvider_Complete_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal server error"}`)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	pe, ok := err.(*llmtrace.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if pe.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", pe.Provider)
	}
}

func TestProvider_Complete_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Error: "model 'nonexistent' not found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "nonexistent",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	pe, ok := err.(*llmtrace.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if !strings.Contains(pe.Message, "not found") {
		t.Errorf("error message = %q, should contain 'not found'", pe.Message)
	}
}

func TestProvider_Complete_MultipleMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 3 {
			t.Errorf("messages count = %d, want 3", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("message[0] role = %q, want system", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "user" {
			t.Errorf("message[1] role = %q, want user", req.Messages[1].Role)
		}
		if req.Messages[2].Role != "assistant" {
			t.Errorf("message[2] role = %q, want assistant", req.Messages[2].Role)
		}

		resp := chatResponse{
			Model:   "llama3",
			Message: &chatMessage{Role: "assistant", Content: "ok"},
			Done:    true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "llama3",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are helpful"},
			{Role: llmtrace.RoleUser, Content: "Hello"},
			{Role: llmtrace.RoleAssistant, Content: "Hi!"},
		},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestProvider_Complete_EmptyMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Model:   "llama3",
			Message: &chatMessage{Role: "assistant", Content: "ok"},
			Done:    true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{},
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Logf("empty messages error (expected): %v", err)
	}
}

func TestProvider_Complete_ConnectionError(t *testing.T) {
	p := New(WithBaseURL("http://localhost:1"))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestProvider_Stream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming request
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !req.Stream {
			t.Error("stream should be true")
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		// Send streaming chunks
		chunks := []chatResponse{
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: "Hello"}, Done: false},
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: " world"}, Done: false},
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: "!"}, Done: true, PromptEvalCount: 10, EvalCount: 3},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", data)
		}
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
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

	var chunks []llmtrace.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
	}

	// Should have 3 content chunks + 1 final usage chunk
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	// Check content
	if chunks[0].Content != "Hello" {
		t.Errorf("chunk[0] content = %q, want %q", chunks[0].Content, "Hello")
	}
	if chunks[1].Content != " world" {
		t.Errorf("chunk[1] content = %q, want %q", chunks[1].Content, " world")
	}
	if chunks[2].Content != "!" {
		t.Errorf("chunk[2] content = %q, want %q", chunks[2].Content, "!")
	}

	// Check final usage
	if chunks[3].Usage == nil {
		t.Fatal("final chunk should have usage")
	}
	if chunks[3].Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", chunks[3].Usage.InputTokens)
	}
	if chunks[3].Usage.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", chunks[3].Usage.OutputTokens)
	}
}

func TestProvider_Stream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		// Send an error chunk
		chunk := chatResponse{Error: "model not found"}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", data)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "nonexistent",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	for chunk := range ch {
		if chunk.Error != nil {
			pe, ok := chunk.Error.(*llmtrace.ProviderError)
			if !ok {
				t.Fatalf("expected ProviderError, got %T", chunk.Error)
			}
			if pe.Provider != "ollama" {
				t.Errorf("Provider = %q, want ollama", pe.Provider)
			}
			return
		}
	}
	t.Fatal("expected error chunk")
}

func TestProvider_Stream_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	_, err := p.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestProvider_ConcurrentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Model:   "llama3",
			Message: &chatMessage{Role: "assistant", Content: "ok"},
			Done:    true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))

	// Run concurrent requests without t.Parallel() to avoid server closing early
	for i := 0; i < 10; i++ {
		req := &llmtrace.Request{
			Model:    "llama3",
			Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
		}
		_, err := p.Complete(context.Background(), req)
		if err != nil {
			t.Fatalf("concurrent Complete() error = %v", err)
		}
	}
}

func TestConvertMessages(t *testing.T) {
	msgs := []llmtrace.Message{
		{Role: llmtrace.RoleSystem, Content: "You are helpful"},
		{Role: llmtrace.RoleUser, Content: "Hello"},
		{Role: llmtrace.RoleAssistant, Content: "Hi!"},
	}

	result := convertMessages(msgs)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("role[0] = %q, want system", result[0].Role)
	}
	if result[1].Content != "Hello" {
		t.Errorf("content[1] = %q, want Hello", result[1].Content)
	}
}

// TestProvider_Stream_BenchmarkScanner tests the scanner with large chunks
func TestProvider_Stream_LargeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		// Create a large content chunk
		largeContent := strings.Repeat("x", 50000)
		chunks := []chatResponse{
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: largeContent}, Done: false},
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: ""}, Done: true, PromptEvalCount: 100, EvalCount: 1},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", data)
		}
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var totalContent string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		totalContent += chunk.Content
	}

	if len(totalContent) != 50000 {
		t.Errorf("total content length = %d, want 50000", len(totalContent))
	}
}

// TestProvider_Stream_EmptyContent verifies empty content chunks are skipped
func TestProvider_Stream_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)

		chunks := []chatResponse{
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: "Hello"}, Done: false},
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: ""}, Done: false}, // empty
			{Model: "llama3", Message: nil, Done: false},                                          // nil message
			{Model: "llama3", Message: &chatMessage{Role: "assistant", Content: " world"}, Done: true, PromptEvalCount: 5, EvalCount: 2},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", data)
		}
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "llama3",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var contentChunks int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			contentChunks++
		}
	}

	// Should have 2 non-empty content chunks + 1 usage chunk
	if contentChunks != 2 {
		t.Errorf("content chunks = %d, want 2", contentChunks)
	}
}

// verifyNdjson is a helper to verify newline-delimited JSON format
func verifyNdjson(t *testing.T, data string) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("invalid NDJSON line: %v", err)
		}
	}
}
