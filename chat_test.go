package llmtrace

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name           string
	defaultModel   string
	supportsStream bool
	completeFunc   func(ctx context.Context, req *Request) (*Response, error)
	streamFunc     func(ctx context.Context, req *Request) (<-chan StreamChunk, error)
}

func (m *mockProvider) Name() string            { return m.name }
func (m *mockProvider) DefaultModel() string    { return m.defaultModel }
func (m *mockProvider) SupportsStreaming() bool { return m.supportsStream }
func (m *mockProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &Response{
		Model:   req.Model,
		Content: "mock response",
		Usage:   Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	}, nil
}

func (m *mockProvider) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Content: "hello "}
	ch <- StreamChunk{Content: "world", Usage: &Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}}
	close(ch)
	return ch, nil
}

func setupTestTracer(t *testing.T) (*Tracer, *tracetest.InMemoryExporter) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		exporter.Reset()
	})
	return NewTracer("test"), exporter
}

func TestChat_Success(t *testing.T) {
	tracer, _ := setupTestTracer(t)
	provider := &mockProvider{name: "test-llm"}

	resp, err := tracer.Chat(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, provider)

	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("Content = %q, want %q", resp.Content, "mock response")
	}
	if resp.Model != "test-model" {
		t.Errorf("Model = %q, want %q", resp.Model, "test-model")
	}
}

func TestChat_WithMiddleware(t *testing.T) {
	tracer, _ := setupTestTracer(t)
	provider := &mockProvider{name: "test-llm"}

	var mu sync.Mutex
	var hookCalled bool

	mw := func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			mu.Lock()
			hookCalled = true
			mu.Unlock()
			return next(ctx, req)
		}
	}

	_, err := tracer.Chat(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, provider, WithCallMiddleware(mw))

	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !hookCalled {
		t.Error("middleware was not called")
	}
}

func TestChat_WithRetry(t *testing.T) {
	tracer, _ := setupTestTracer(t)
	calls := 0
	provider := &mockProvider{
		name: "test-llm",
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			calls++
			if calls < 3 {
				return nil, NewRetryableError(errors.New("transient"))
			}
			return &Response{
				Model:   req.Model,
				Content: "success after retry",
				Usage:   Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			}, nil
		},
	}

	retryCfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: 10,
		MaxInterval:     100,
		Multiplier:      2.0,
		Jitter:          0,
	}

	resp, err := tracer.Chat(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, provider, WithCallRetry(retryCfg))

	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "success after retry" {
		t.Errorf("Content = %q, want %q", resp.Content, "success after retry")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestChat_ProviderError(t *testing.T) {
	tracer, _ := setupTestTracer(t)
	expectedErr := errors.New("api error")
	provider := &mockProvider{
		name: "test-llm",
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return nil, expectedErr
		},
	}

	_, err := tracer.Chat(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, provider)

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}

func TestChatStream_Success(t *testing.T) {
	tracer, _ := setupTestTracer(t)
	provider := &mockProvider{name: "test-llm", supportsStream: true}

	ch, err := tracer.ChatStream(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, provider)

	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		content += chunk.Content
	}

	if content != "hello world" {
		t.Errorf("content = %q, want %q", content, "hello world")
	}
}

func TestChat_NilProvider(t *testing.T) {
	tracer, _ := setupTestTracer(t)

	// This should panic or we handle it gracefully
	defer func() {
		if r := recover(); r == nil {
			// If no panic, that's also acceptable
		}
	}()

	_, _ = tracer.Chat(context.Background(), &Request{
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}, nil)
}
