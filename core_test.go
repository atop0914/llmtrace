package llmtrace

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// setupTracer creates a Tracer with an in-memory span exporter for testing.
func setupTracer(t *testing.T, opts ...Option) (*Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)

	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		exporter.Reset()
	})

	tracer := NewTracer("test-service", opts...)
	return tracer, exporter
}

func TestTracer_Complete_Success(t *testing.T) {
	tracer, exporter := setupTracer(t, WithProvider("openai"))

	req := &Request{
		Model:       "gpt-4o",
		Temperature: Float64Ptr(0.7),
		TopP:        Float64Ptr(0.9),
		MaxTokens:   IntPtr(100),
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	}

	mockResp := &Response{
		ID:           "resp-123",
		Model:        "gpt-4o",
		Content:      "Hi there!",
		FinishReason: "stop",
		Usage:        Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Provider:     "openai",
	}

	fn := func(ctx context.Context, r *Request) (*Response, error) {
		time.Sleep(10 * time.Millisecond) // Simulate latency
		return mockResp, nil
	}

	resp, err := tracer.Complete(context.Background(), req, fn)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Hi there!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hi there!")
	}

	// Verify span attributes
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status.Code != codes.Ok {
		t.Errorf("status code = %v, want Ok", span.Status.Code)
	}

	attrs := attrMap(span.Attributes)
	assertAttr(t, attrs, AttrGenAISystem, "openai")
	assertAttr(t, attrs, AttrGenAIOperationName, "chat")
	assertAttr(t, attrs, AttrGenAIRequestModel, "gpt-4o")
	assertAttr(t, attrs, AttrGenAIResponseModel, "gpt-4o")
	assertAttr(t, attrs, AttrGenAIResponseID, "resp-123")
	assertAttr(t, attrs, AttrGenAIFinishReasons, "stop")
	assertAttrInt(t, attrs, AttrGenAIInputTokens, 10)
	assertAttrInt(t, attrs, AttrGenAIOutputTokens, 5)
	assertAttrInt(t, attrs, AttrGenAITotalTokens, 15)
	assertAttrFloat(t, attrs, AttrGenAIRequestTemp, 0.7)
	assertAttrFloat(t, attrs, AttrGenAIRequestTopP, 0.9)
	assertAttrInt(t, attrs, AttrGenAIRequestMaxTok, 100)

	if span.SpanKind != trace.SpanKindClient {
		t.Errorf("span kind = %v, want Client", span.SpanKind)
	}
}

func TestTracer_Complete_Error(t *testing.T) {
	tracer, exporter := setupTracer(t)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	expectedErr := errors.New("API rate limit exceeded")
	fn := func(ctx context.Context, r *Request) (*Response, error) {
		return nil, expectedErr
	}

	resp, err := tracer.Complete(context.Background(), req, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got %v", resp)
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("error = %q, want %q", err.Error(), expectedErr.Error())
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status.Code != codes.Error {
		t.Errorf("status code = %v, want Error", span.Status.Code)
	}
	if len(span.Events) == 0 {
		t.Error("expected error event on span")
	}
}

func TestTracer_Complete_WithCostCalculator(t *testing.T) {
	calc := NewCostCalculator()
	tracer, exporter := setupTracer(t,
		WithProvider("openai"),
		WithCostCalculator(calc),
	)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	resp := &Response{
		Model: "gpt-4o",
		Usage: Usage{InputTokens: 1000, OutputTokens: 500},
	}

	fn := func(ctx context.Context, r *Request) (*Response, error) {
		return resp, nil
	}

	_, err := tracer.Complete(context.Background(), req, fn)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	spans := exporter.GetSpans()
	attrs := attrMap(spans[0].Attributes)
	// Cost: 1000/1000*0.0025 + 500/1000*0.01 = 0.0025 + 0.005 = 0.0075
	assertAttrFloat(t, attrs, AttrGenAICostUSD, 0.0075)
}

func TestTracer_Complete_MinimalRequest(t *testing.T) {
	tracer, exporter := setupTracer(t)

	req := &Request{
		Model:    "gpt-3.5-turbo",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}

	fn := func(ctx context.Context, r *Request) (*Response, error) {
		return &Response{
			Model: "gpt-3.5-turbo",
			Usage: Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		}, nil
	}

	_, err := tracer.Complete(context.Background(), req, fn)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	attrs := attrMap(spans[0].Attributes)
	assertAttr(t, attrs, AttrGenAIRequestModel, "gpt-3.5-turbo")
	// Temperature/TopP/MaxTokens not set — should not appear
	if _, ok := attrs[AttrGenAIRequestTemp]; ok {
		t.Error("temperature should not be set when nil")
	}
}

func TestTracer_Stream_Success(t *testing.T) {
	tracer, exporter := setupTracer(t, WithProvider("anthropic"))

	req := &Request{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []Message{{Role: RoleUser, Content: "Tell me a story"}},
	}

	streamFn := func(ctx context.Context, r *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 3)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Once "}
			ch <- StreamChunk{Content: "upon "}
			ch <- StreamChunk{
				Content: "a time",
				Usage:   &Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			}
		}()
		return ch, nil
	}

	ch, err := tracer.Stream(context.Background(), req, streamFn)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Errorf("got %d chunks, want 3", len(chunks))
	}

	// Verify span was created
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	attrs := attrMap(spans[0].Attributes)
	assertAttr(t, attrs, AttrGenAISystem, "anthropic")
	assertAttr(t, attrs, AttrGenAIRequestModel, "claude-3-5-sonnet-20241022")
	assertAttrInt(t, attrs, AttrGenAIInputTokens, 10)
	assertAttrInt(t, attrs, AttrGenAIOutputTokens, 20)
	assertAttrInt(t, attrs, AttrGenAITotalTokens, 30)
}

func TestTracer_Stream_Error(t *testing.T) {
	tracer, exporter := setupTracer(t)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	expectedErr := errors.New("stream failed")
	streamFn := func(ctx context.Context, r *Request) (<-chan StreamChunk, error) {
		return nil, expectedErr
	}

	ch, err := tracer.Stream(context.Background(), req, streamFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ch != nil {
		t.Error("expected nil channel on error")
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Error("expected error status on span")
	}
}

func TestTracer_Stream_ChunkError(t *testing.T) {
	tracer, exporter := setupTracer(t)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	streamFn := func(ctx context.Context, r *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 2)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Content: "Hello"}
			ch <- StreamChunk{Error: errors.New("mid-stream error")}
		}()
		return ch, nil
	}

	ch, err := tracer.Stream(context.Background(), req, streamFn)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	// Drain channel
	for range ch {
	}

	// Wait for the goroutine to finish and export the span
	time.Sleep(50 * time.Millisecond)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Error("expected error status after chunk error")
	}
}

// --- helpers ---

func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value
	}
	return m
}

func assertAttr(t *testing.T, attrs map[string]attribute.Value, key, want string) {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Errorf("missing attribute %q", key)
		return
	}
	if v.AsString() != want {
		t.Errorf("attr %q = %q, want %q", key, v.AsString(), want)
	}
}

func assertAttrInt(t *testing.T, attrs map[string]attribute.Value, key string, want int) {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Errorf("missing attribute %q", key)
		return
	}
	if int(v.AsInt64()) != want {
		t.Errorf("attr %q = %d, want %d", key, v.AsInt64(), want)
	}
}

func assertAttrFloat(t *testing.T, attrs map[string]attribute.Value, key string, want float64) {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Errorf("missing attribute %q", key)
		return
	}
	got := v.AsFloat64()
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("attr %q = %f, want %f", key, got, want)
	}
}
