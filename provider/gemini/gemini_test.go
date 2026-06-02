package gemini

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
	if p.Name() != "gemini" {
		t.Errorf("Name() = %q, want %q", p.Name(), "gemini")
	}
}

func TestProvider_DefaultModel(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{"default", nil, DefaultModel},
		{"custom", []Option{WithModel("gemini-pro")}, "gemini-pro"},
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
		if !strings.Contains(r.URL.Path, "/models/") {
			t.Errorf("path = %q, should contain /models/", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path = %q, should contain :generateContent", r.URL.Path)
		}
		// API key should be in query
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("key = %q, want test-key", r.URL.Query().Get("key"))
		}

		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Contents) != 1 {
			t.Errorf("contents count = %d, want 1", len(req.Contents))
		}
		if req.Contents[0].Role != "user" {
			t.Errorf("content role = %q, want user", req.Contents[0].Role)
		}

		resp := generateResponse{
			Candidates: []candidate{
				{
					Content: content{
						Role:  "model",
						Parts: []part{{Text: "Hello!"}},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: usageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 5,
				TotalTokenCount:      15,
			},
			ModelVersion: "gemini-2.0-flash",
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
		Model: "gemini-2.0-flash",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want Hello!", resp.Content)
	}
	if resp.Model != "gemini-2.0-flash" {
		t.Errorf("Model = %q, want gemini-2.0-flash", resp.Model)
	}
	if resp.FinishReason != "STOP" {
		t.Errorf("FinishReason = %q, want STOP", resp.FinishReason)
	}
	if resp.Provider != "gemini" {
		t.Errorf("Provider = %q, want gemini", resp.Provider)
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
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.GenerationConfig == nil {
			t.Fatal("generationConfig is nil")
		}
		if req.GenerationConfig.Temperature == nil || *req.GenerationConfig.Temperature != 0.7 {
			t.Error("temperature not set correctly")
		}
		if req.GenerationConfig.TopP == nil || *req.GenerationConfig.TopP != 0.9 {
			t.Error("topP not set correctly")
		}
		if req.GenerationConfig.MaxOutputTokens == nil || *req.GenerationConfig.MaxOutputTokens != 100 {
			t.Error("maxOutputTokens not set correctly")
		}
		if len(req.GenerationConfig.StopSequences) != 1 || req.GenerationConfig.StopSequences[0] != "\n" {
			t.Errorf("stopSequences = %v, want [\\n]", req.GenerationConfig.StopSequences)
		}

		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: "OK"}}}}},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	temp := 0.7
	topP := 0.9
	maxTok := 100

	req := &llmtrace.Request{
		Model:       "gemini-2.0-flash",
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
		if !strings.Contains(r.URL.Path, DefaultModel) {
			t.Errorf("path = %q, should contain default model %q", r.URL.Path, DefaultModel)
		}
		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: "OK"}}}}},
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
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.SystemInstruction == nil {
			t.Fatal("systemInstruction is nil")
		}
		if req.SystemInstruction.Parts[0].Text != "You are helpful" {
			t.Errorf("system instruction = %q, want 'You are helpful'", req.SystemInstruction.Parts[0].Text)
		}
		// System message should not be in contents
		for _, c := range req.Contents {
			if c.Role == "system" {
				t.Error("system message should not be in contents")
			}
		}

		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: "OK"}}}}},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "gemini-2.0-flash",
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

func TestProvider_Complete_AssistantRoleMappedToModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)

		if len(req.Contents) != 2 {
			t.Fatalf("contents count = %d, want 2", len(req.Contents))
		}
		if req.Contents[0].Role != "user" {
			t.Errorf("content 0 role = %q, want user", req.Contents[0].Role)
		}
		if req.Contents[1].Role != "model" {
			t.Errorf("content 1 role = %q, want model", req.Contents[1].Role)
		}

		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{{Content: content{Parts: []part{{Text: "OK"}}}}},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model: "gemini-2.0-flash",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hi"},
			{Role: llmtrace.RoleAssistant, Content: "Hello!"},
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
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    403,
				"message": "API key not valid",
				"status":  "PERMISSION_DENIED",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gemini-2.0-flash",
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
	if apiErr.StatusCode != 403 {
		t.Errorf("status code = %d, want 403", apiErr.StatusCode)
	}
	if apiErr.Message != "API key not valid" {
		t.Errorf("message = %q, want 'API key not valid'", apiErr.Message)
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
		Model:    "gemini-2.0-flash",
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

func TestProvider_Complete_EmptyCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{},
			UsageMetadata: usageMetadata{
				PromptTokenCount: 5,
				TotalTokenCount:  5,
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gemini-2.0-flash",
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

func TestProvider_Complete_MultipleParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(generateResponse{
			Candidates: []candidate{
				{
					Content: content{
						Role:  "model",
						Parts: []part{{Text: "Hello"}, {Text: " world!"}},
					},
					FinishReason: "STOP",
				},
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gemini-2.0-flash",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hi"}},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Hello world!" {
		t.Errorf("Content = %q, want 'Hello world!'", resp.Content)
	}
}

func TestProvider_Stream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)

		if !strings.Contains(r.URL.RawQuery, "alt=sse") {
			t.Error("expected alt=sse in query for streaming")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]},"finishReason":""}]}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world!"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`,
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
		Model:    "gemini-2.0-flash",
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

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Content != "Hello" {
		t.Errorf("chunk 0 content = %q, want Hello", chunks[0].Content)
	}
	if chunks[1].Content != " world!" {
		t.Errorf("chunk 1 content = %q, want ' world!'", chunks[1].Content)
	}
	// Usage should be on the last chunk
	if chunks[1].Usage == nil {
		t.Fatal("expected usage on last chunk")
	}
	if chunks[1].Usage.TotalTokens != 7 {
		t.Errorf("total tokens = %d, want 7", chunks[1].Usage.TotalTokens)
	}
}

func TestProvider_Stream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    400,
				"message": "Invalid request",
				"status":  "INVALID_ARGUMENT",
			},
		})
	}))
	defer server.Close()

	p := New(WithBaseURL(server.URL))
	req := &llmtrace.Request{
		Model:    "gemini-2.0-flash",
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

func TestAPIError_Error(t *testing.T) {
	e := &APIError{
		StatusCode: 429,
		Message:    "Rate limit exceeded",
		Status:     "RESOURCE_EXHAUSTED",
	}
	s := e.Error()
	if !strings.Contains(s, "429") {
		t.Errorf("error should contain 429: %s", s)
	}
	if !strings.Contains(s, "Rate limit exceeded") {
		t.Errorf("error should contain message: %s", s)
	}
	if !strings.Contains(s, "RESOURCE_EXHAUSTED") {
		t.Errorf("error should contain status: %s", s)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check that Provider implements llmtrace.Provider
	var _ llmtrace.Provider = New()
}
