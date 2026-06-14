package llmtrace

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testHandler is a custom slog.Handler that captures log records for testing.
type testHandler struct {
	records []slog.Record
	attrs   map[string][]slog.Attr
}

func newTestHandler() *testHandler {
	return &testHandler{
		records: make([]slog.Record, 0),
		attrs:   make(map[string][]slog.Attr),
	}
}

func (h *testHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	attrs := make([]slog.Attr, 0)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	h.attrs[r.Message] = append(h.attrs[r.Message], attrs...)
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *testHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *testHandler) recordCount() int {
	return len(h.records)
}

func (h *testHandler) hasMessage(msg string) bool {
	for _, r := range h.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

func (h *testHandler) getAttrs(msg string) map[string]any {
	result := make(map[string]any)
	attrs, ok := h.attrs[msg]
	if !ok {
		return result
	}
	for _, a := range attrs {
		result[a.Key] = a.Value.Any()
	}
	return result
}

func TestSlog_DefaultConfig(t *testing.T) {
	cfg := DefaultSlogConfig()
	if cfg.Level != slog.LevelInfo {
		t.Errorf("expected Level %v, got %v", slog.LevelInfo, cfg.Level)
	}
	if cfg.ErrorLevel != slog.LevelError {
		t.Errorf("expected ErrorLevel %v, got %v", slog.LevelError, cfg.ErrorLevel)
	}
	if !cfg.LogRequest {
		t.Error("expected LogRequest to be true")
	}
	if !cfg.LogResponse {
		t.Error("expected LogResponse to be true")
	}
	if !cfg.LogErrors {
		t.Error("expected LogErrors to be true")
	}
	if !cfg.SanitizeContent {
		t.Error("expected SanitizeContent to be true")
	}
}

func TestSlog_SuccessfulCompletion(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		Level:       slog.LevelInfo,
		ErrorLevel:  slog.LevelError,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithSlog(cfg)
	mockResp := &Response{
		ID:           "resp-123",
		Model:        "gpt-4o",
		Content:      "Hello!",
		FinishReason: "stop",
		Usage:        Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Latency:      100 * time.Millisecond,
		Provider:     "openai",
	}

	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return mockResp, nil
	})

	req := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Hello"}},
		Temperature: Float64Ptr(0.7),
		MaxTokens:   IntPtr(100),
	}

	resp, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != mockResp {
		t.Error("expected response to match mock")
	}

	// Check that request and response were logged
	if !handler.hasMessage("llm request started") {
		t.Error("expected 'llm request started' log message")
	}
	if !handler.hasMessage("llm request completed") {
		t.Error("expected 'llm request completed' log message")
	}
	if handler.recordCount() != 2 {
		t.Errorf("expected 2 log records, got %d", handler.recordCount())
	}

	// Check request attributes
	reqAttrs := handler.getAttrs("llm request started")
	if reqAttrs["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", reqAttrs["model"])
	}
	if reqAttrs["message_count"] != int64(1) {
		t.Errorf("expected message_count 1, got %v", reqAttrs["message_count"])
	}
	if reqAttrs["max_tokens"] != int64(100) {
		t.Errorf("expected max_tokens 100, got %v", reqAttrs["max_tokens"])
	}
	if reqAttrs["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", reqAttrs["temperature"])
	}

	// Check response attributes
	respAttrs := handler.getAttrs("llm request completed")
	if respAttrs["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", respAttrs["model"])
	}
	if respAttrs["provider"] != "openai" {
		t.Errorf("expected provider 'openai', got %v", respAttrs["provider"])
	}
	if respAttrs["input_tokens"] != int64(10) {
		t.Errorf("expected input_tokens 10, got %v", respAttrs["input_tokens"])
	}
	if respAttrs["output_tokens"] != int64(5) {
		t.Errorf("expected output_tokens 5, got %v", respAttrs["output_tokens"])
	}
	if respAttrs["total_tokens"] != int64(15) {
		t.Errorf("expected total_tokens 15, got %v", respAttrs["total_tokens"])
	}
	if respAttrs["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason 'stop', got %v", respAttrs["finish_reason"])
	}
	if respAttrs["response_id"] != "resp-123" {
		t.Errorf("expected response_id 'resp-123', got %v", respAttrs["response_id"])
	}
}

func TestSlog_ErrorCompletion(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		Level:       slog.LevelInfo,
		ErrorLevel:  slog.LevelError,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithSlog(cfg)
	expectedErr := &ProviderError{
		Provider:   "openai",
		StatusCode: 429,
		Code:       "rate_limit_exceeded",
		Type:       ErrorTypeRateLimit,
		Message:    "too many requests",
	}

	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, expectedErr
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Check that request start and error were logged
	if !handler.hasMessage("llm request started") {
		t.Error("expected 'llm request started' log message")
	}
	if !handler.hasMessage("llm request failed") {
		t.Error("expected 'llm request failed' log message")
	}
	if handler.hasMessage("llm request completed") {
		t.Error("should not log completion on error")
	}

	// Check error attributes
	errAttrs := handler.getAttrs("llm request failed")
	if errAttrs["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", errAttrs["model"])
	}
	// ProviderError.Error() returns "openai: too many requests"
	if errAttrs["error"] != "openai: too many requests" {
		t.Errorf("expected error 'openai: too many requests', got %v", errAttrs["error"])
	}
	if errAttrs["provider"] != "openai" {
		t.Errorf("expected provider 'openai', got %v", errAttrs["provider"])
	}
	if errAttrs["status_code"] != int64(429) {
		t.Errorf("expected status_code 429, got %v", errAttrs["status_code"])
	}
	if errAttrs["error_code"] != "rate_limit_exceeded" {
		t.Errorf("expected error_code 'rate_limit_exceeded', got %v", errAttrs["error_code"])
	}
	if errAttrs["error_type"] != string(ErrorTypeRateLimit) {
		t.Errorf("expected error_type 'rate_limit', got %v", errAttrs["error_type"])
	}
}

func TestSlog_PlainError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	plainErr := errors.New("connection timeout")

	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, plainErr
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errAttrs := handler.getAttrs("llm request failed")
	if errAttrs["error"] != "connection timeout" {
		t.Errorf("expected error 'connection timeout', got %v", errAttrs["error"])
	}
	// Should not have provider-specific attributes for plain errors
	if _, ok := errAttrs["provider"]; ok {
		t.Error("should not have provider attribute for plain error")
	}
}

func TestSlog_DisabledRequestLogging(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		LogRequest:  false,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if handler.hasMessage("llm request started") {
		t.Error("should not log request start when LogRequest is false")
	}
	if !handler.hasMessage("llm request completed") {
		t.Error("expected 'llm request completed' log message")
	}
}

func TestSlog_DisabledResponseLogging(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		LogRequest:  true,
		LogResponse: false,
		LogErrors:   true,
	}

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !handler.hasMessage("llm request started") {
		t.Error("expected 'llm request started' log message")
	}
	if handler.hasMessage("llm request completed") {
		t.Error("should not log response when LogResponse is false")
	}
}

func TestSlog_DisabledErrorLogging(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   false,
	}

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, errors.New("test error")
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = fn(context.Background(), req)

	if handler.hasMessage("llm request failed") {
		t.Error("should not log error when LogErrors is false")
	}
}

func TestSlog_NilLogger(t *testing.T) {
	// Test that nil logger falls back to slog.Default()
	cfg := DefaultSlogConfig()
	cfg.Logger = nil

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// Should not panic with nil logger
	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSlog_OptionalFields(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	// Request without optional fields
	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqAttrs := handler.getAttrs("llm request started")
	if _, ok := reqAttrs["max_tokens"]; ok {
		t.Error("should not have max_tokens when not set")
	}
	if _, ok := reqAttrs["temperature"]; ok {
		t.Error("should not have temperature when not set")
	}
}

func TestSlog_LatencyTracking(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		time.Sleep(10 * time.Millisecond)
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	respAttrs := handler.getAttrs("llm request completed")
	latency, ok := respAttrs["latency"].(time.Duration)
	if !ok {
		t.Fatal("expected latency to be a time.Duration")
	}
	if latency < 10*time.Millisecond {
		t.Errorf("expected latency >= 10ms, got %v", latency)
	}
}

func TestSlog_MultipleMessages(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:   "gpt-4o",
			Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency: 50 * time.Millisecond,
		}, nil
	})

	req := &Request{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a helpful assistant."},
			{Role: RoleUser, Content: "Hello!"},
			{Role: RoleAssistant, Content: "Hi there!"},
			{Role: RoleUser, Content: "How are you?"},
		},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqAttrs := handler.getAttrs("llm request started")
	if reqAttrs["message_count"] != int64(4) {
		t.Errorf("expected message_count 4, got %v", reqAttrs["message_count"])
	}
}

// Stream slog tests

func TestStreamSlog_SuccessfulStream(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		Level:       slog.LevelInfo,
		ErrorLevel:  slog.LevelError,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithStreamSlog(cfg)

	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 3)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
			ch <- StreamChunk{Content: " world"}
			ch <- StreamChunk{
				Content: "!",
				Usage:   &Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}
		}()
		return ch, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	ch, err := streamFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consume all chunks
	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Wait for async logging
	time.Sleep(20 * time.Millisecond)

	if !handler.hasMessage("llm stream started") {
		t.Error("expected 'llm stream started' log message")
	}
	if !handler.hasMessage("llm stream completed") {
		t.Error("expected 'llm stream completed' log message")
	}

	// Check stream start attributes
	startAttrs := handler.getAttrs("llm stream started")
	if startAttrs["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", startAttrs["model"])
	}
	if startAttrs["type"] != "stream" {
		t.Errorf("expected type 'stream', got %v", startAttrs["type"])
	}

	// Check stream completion attributes
	compAttrs := handler.getAttrs("llm stream completed")
	if compAttrs["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", compAttrs["model"])
	}
	if compAttrs["chunks_received"] != int64(3) {
		t.Errorf("expected chunks_received 3, got %v", compAttrs["chunks_received"])
	}
	if compAttrs["input_tokens"] != int64(10) {
		t.Errorf("expected input_tokens 10, got %v", compAttrs["input_tokens"])
	}
	if compAttrs["output_tokens"] != int64(5) {
		t.Errorf("expected output_tokens 5, got %v", compAttrs["output_tokens"])
	}
}

func TestStreamSlog_StreamError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		Level:       slog.LevelInfo,
		ErrorLevel:  slog.LevelError,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithStreamSlog(cfg)
	expectedErr := errors.New("stream connection lost")

	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 2)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
			ch <- StreamChunk{Error: expectedErr}
		}()
		return ch, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	ch, err := streamFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consume all chunks
	for range ch {
	}

	// Wait for async logging
	time.Sleep(20 * time.Millisecond)

	if !handler.hasMessage("llm stream error") {
		t.Error("expected 'llm stream error' log message")
	}

	errAttrs := handler.getAttrs("llm stream error")
	if errAttrs["error"] != "stream connection lost" {
		t.Errorf("expected error 'stream connection lost', got %v", errAttrs["error"])
	}
	if errAttrs["chunks_received"] != int64(2) {
		t.Errorf("expected chunks_received 2, got %v", errAttrs["chunks_received"])
	}
}

func TestStreamSlog_StartError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		LogRequest:  true,
		LogResponse: true,
		LogErrors:   true,
	}

	mw := WithStreamSlog(cfg)
	expectedErr := errors.New("provider unavailable")

	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		return nil, expectedErr
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := streamFn(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !handler.hasMessage("llm stream failed") {
		t.Error("expected 'llm stream failed' log message")
	}

	errAttrs := handler.getAttrs("llm stream failed")
	if errAttrs["error"] != "provider unavailable" {
		t.Errorf("expected error 'provider unavailable', got %v", errAttrs["error"])
	}
}

func TestStreamSlog_DisabledLogging(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:      logger,
		LogRequest:  false,
		LogResponse: false,
		LogErrors:   false,
	}

	mw := WithStreamSlog(cfg)

	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
		}()
		return ch, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	ch, err := streamFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for range ch {
	}

	// Wait for async logging
	time.Sleep(20 * time.Millisecond)

	if handler.recordCount() != 0 {
		t.Errorf("expected 0 log records, got %d", handler.recordCount())
	}
}

func TestStreamSlog_NilLogger(t *testing.T) {
	cfg := DefaultSlogConfig()
	cfg.Logger = nil

	mw := WithStreamSlog(cfg)

	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
		}()
		return ch, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// Should not panic with nil logger
	ch, err := streamFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for range ch {
	}
}

func TestSlog_IntegrationWithChat(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	// Create a mock provider
	mockProvider := &mockSlogProvider{
		completeFn: func(ctx context.Context, req *Request) (*Response, error) {
			return &Response{
				ID:           "resp-456",
				Model:        "gpt-4o",
				Content:      "Hi there!",
				FinishReason: "stop",
				Usage:        Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
				Latency:      75 * time.Millisecond,
				Provider:     "openai",
			}, nil
		},
	}

	tracer := NewTracer("test-service", WithProvider("openai"))
	req := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Hello"}},
		Temperature: Float64Ptr(0.5),
	}

	resp, err := tracer.Chat(context.Background(), req, mockProvider,
		WithCallMiddleware(WithSlog(cfg)),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}

	// Check that logging happened
	if !handler.hasMessage("llm request started") {
		t.Error("expected 'llm request started' log message")
	}
	if !handler.hasMessage("llm request completed") {
		t.Error("expected 'llm request completed' log message")
	}
}

func TestSlog_JSONFormat(t *testing.T) {
	// Test that slog attributes are properly structured for JSON output
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{
			Model:        "gpt-4o",
			Content:      "Hello!",
			FinishReason: "stop",
			Usage:        Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			Latency:      100 * time.Millisecond,
			Provider:     "openai",
		}, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify attributes can be serialized to JSON (validates they're proper types)
	respAttrs := handler.getAttrs("llm request completed")
	jsonData, err := json.Marshal(respAttrs)
	if err != nil {
		t.Fatalf("failed to marshal attributes to JSON: %v", err)
	}
	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "gpt-4o") {
		t.Error("JSON output should contain model name")
	}
	if !strings.Contains(jsonStr, "openai") {
		t.Error("JSON output should contain provider name")
	}
}

// mockSlogProvider is a test provider for slog integration tests.
type mockSlogProvider struct {
	completeFn func(ctx context.Context, req *Request) (*Response, error)
	streamFn   func(ctx context.Context, req *Request) (<-chan StreamChunk, error)
}

func (m *mockSlogProvider) Name() string { return "mock" }

func (m *mockSlogProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, req)
	}
	return &Response{
		Model:   req.Model,
		Content: "mock response",
		Usage:   Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
	}, nil
}

func (m *mockSlogProvider) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	ch := make(chan StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- StreamChunk{
			Content: "mock stream",
			Usage:   &Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		}
	}()
	return ch, nil
}

func (m *mockSlogProvider) DefaultModel() string    { return "mock-model" }
func (m *mockSlogProvider) SupportsStreaming() bool { return true }

// --- Sanitizer integration tests ---

func TestSlog_SanitizeErrorMessages(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger
	// SanitizeContent is true by default

	mw := WithSlog(cfg)
	// Simulate an error that contains an API key
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, errors.New("authentication failed: api_key = abcdefghijklmnop1234")
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = fn(context.Background(), req)

	errAttrs := handler.getAttrs("llm request failed")
	errMsg, ok := errAttrs["error"].(string)
	if !ok {
		t.Fatal("expected error attribute to be a string")
	}
	if strings.Contains(errMsg, "abcdefghijklmnop1234") {
		t.Errorf("API key not sanitized in error message: %q", errMsg)
	}
	if !strings.Contains(errMsg, "[API_KEY_REDACTED]") {
		t.Errorf("expected [API_KEY_REDACTED] in sanitized error, got %q", errMsg)
	}
}

func TestSlog_SanitizeDisabled(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := SlogConfig{
		Logger:          logger,
		Level:           slog.LevelInfo,
		ErrorLevel:      slog.LevelError,
		LogRequest:      true,
		LogResponse:     true,
		LogErrors:       true,
		SanitizeContent: false,
	}

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, errors.New("authentication failed: api_key = abcdefghijklmnop1234")
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = fn(context.Background(), req)

	errAttrs := handler.getAttrs("llm request failed")
	errMsg, ok := errAttrs["error"].(string)
	if !ok {
		t.Fatal("expected error attribute to be a string")
	}
	// Without sanitization, the original error message should be preserved
	if !strings.Contains(errMsg, "abcdefghijklmnop1234") {
		t.Errorf("expected unsanitized error message, got %q", errMsg)
	}
}

func TestSlog_CustomSanitizer(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)

	// Create a sanitizer with a custom rule that masks employee IDs
	customSanitizer := NewSanitizer(WithCustomRules(SanitizeRule{
		Name:        "employee_id",
		Pattern:     regexp.MustCompile(`EMP-\d{6}`),
		Replacement: "[EMP_ID_REDACTED]",
	}))

	cfg := SlogConfig{
		Logger:          logger,
		Level:           slog.LevelInfo,
		ErrorLevel:      slog.LevelError,
		LogRequest:      true,
		LogResponse:     true,
		LogErrors:       true,
		SanitizeContent: true,
		Sanitizer:       customSanitizer,
	}

	mw := WithSlog(cfg)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, errors.New("unauthorized access by EMP-123456")
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = fn(context.Background(), req)

	errAttrs := handler.getAttrs("llm request failed")
	errMsg, ok := errAttrs["error"].(string)
	if !ok {
		t.Fatal("expected error attribute to be a string")
	}
	if strings.Contains(errMsg, "EMP-123456") {
		t.Errorf("employee ID not sanitized: %q", errMsg)
	}
	if !strings.Contains(errMsg, "[EMP_ID_REDACTED]") {
		t.Errorf("expected [EMP_ID_REDACTED] in output, got %q", errMsg)
	}
}

func TestSlog_SanitizeBearerTokenInError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithSlog(cfg)
	longToken := "Bearer " + "abcdefghijklmnopqrstuvwxyz0123456789"
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		return nil, errors.New("invalid token: " + longToken)
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = fn(context.Background(), req)

	errAttrs := handler.getAttrs("llm request failed")
	errMsg := errAttrs["error"].(string)
	if strings.Contains(errMsg, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("Bearer token not sanitized in error: %q", errMsg)
	}
	if !strings.Contains(errMsg, "[BEARER_REDACTED]") {
		t.Errorf("expected [BEARER_REDACTED] in output, got %q", errMsg)
	}
}

func TestStreamSlog_SanitizeStreamError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithStreamSlog(cfg)
	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 2)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
			ch <- StreamChunk{Error: errors.New("stream error: api_key = abcdefghijklmnop1234 leaked")}
		}()
		return ch, nil
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	ch, err := streamFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}

	time.Sleep(20 * time.Millisecond)

	errAttrs := handler.getAttrs("llm stream error")
	errMsg, ok := errAttrs["error"].(string)
	if !ok {
		t.Fatal("expected error attribute to be a string")
	}
	if strings.Contains(errMsg, "abcdefghijklmnop1234") {
		t.Errorf("API key not sanitized in stream error: %q", errMsg)
	}
	if !strings.Contains(errMsg, "[API_KEY_REDACTED]") {
		t.Errorf("expected [API_KEY_REDACTED] in stream error, got %q", errMsg)
	}
}

func TestStreamSlog_SanitizeStartError(t *testing.T) {
	handler := newTestHandler()
	logger := slog.New(handler)
	cfg := DefaultSlogConfig()
	cfg.Logger = logger

	mw := WithStreamSlog(cfg)
	longKey := "sk-proj-" + "abcdefghijklmnopqrstuvwxyz0123456"
	streamFn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		return nil, errors.New("connection failed: api_key = " + longKey)
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	_, _ = streamFn(context.Background(), req)

	errAttrs := handler.getAttrs("llm stream failed")
	errMsg, ok := errAttrs["error"].(string)
	if !ok {
		t.Fatal("expected error attribute to be a string")
	}
	if strings.Contains(errMsg, "abcdefghijklmnopqrstuvwxyz0123456") {
		t.Errorf("OpenAI key not sanitized in stream start error: %q", errMsg)
	}
	if !strings.Contains(errMsg, "[OPENAI_KEY_REDACTED]") {
		t.Errorf("expected [OPENAI_KEY_REDACTED] in output, got %q", errMsg)
	}
}
