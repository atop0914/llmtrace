package llmtrace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// --- Benchmark helpers ---

// setupBenchTracer creates a Tracer with a no-op or in-memory exporter for benchmarks.
func setupBenchTracer(b *testing.B, opts ...Option) *Tracer {
	b.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	b.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		exporter.Reset()
	})
	return NewTracer("bench-service", opts...)
}

// benchRequest returns a standard benchmark request.
func benchRequest() *Request {
	return &Request{
		Model:       "gpt-4o",
		Temperature: Float64Ptr(0.7),
		TopP:        Float64Ptr(0.9),
		MaxTokens:   IntPtr(200),
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a helpful assistant."},
			{Role: RoleUser, Content: "What is the meaning of life?"},
		},
	}
}

// benchResponse returns a standard benchmark response.
func benchResponse() *Response {
	return &Response{
		ID:           "resp-bench-123",
		Model:        "gpt-4o",
		Content:      "The meaning of life is a philosophical question...",
		FinishReason: "stop",
		Usage:        Usage{InputTokens: 25, OutputTokens: 50, TotalTokens: 75},
		Provider:     "openai",
	}
}

// noOpComplete returns a CompleteFunc that returns immediately with a mock response.
func noOpComplete() CompleteFunc {
	resp := benchResponse()
	return func(ctx context.Context, req *Request) (*Response, error) {
		return resp, nil
	}
}

// --- Tracer benchmarks ---

func BenchmarkTracer_Complete(b *testing.B) {
	tracer := setupBenchTracer(b, WithProvider("openai"))
	req := benchRequest()
	fn := noOpComplete()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Complete(ctx, req, fn)
	}
}

func BenchmarkTracer_Complete_WithCost(b *testing.B) {
	tracer := setupBenchTracer(b,
		WithProvider("openai"),
		WithCostCalculator(NewCostCalculator()),
	)
	req := benchRequest()
	fn := noOpComplete()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Complete(ctx, req, fn)
	}
}

func BenchmarkTracer_Complete_WithError(b *testing.B) {
	tracer := setupBenchTracer(b, WithProvider("openai"))
	req := benchRequest()
	errFn := func(ctx context.Context, r *Request) (*Response, error) {
		return nil, errors.New("rate limit exceeded")
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Complete(ctx, req, errFn)
	}
}

func BenchmarkTracer_Stream(b *testing.B) {
	tracer := setupBenchTracer(b, WithProvider("openai"))
	req := benchRequest()
	streamFn := func(ctx context.Context, r *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 3)
		ch <- StreamChunk{Content: "Hello "}
		ch <- StreamChunk{Content: "world "}
		ch <- StreamChunk{Content: "!", Usage: &Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}
		close(ch)
		return ch, nil
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := tracer.Stream(ctx, req, streamFn)
		for range ch {
		}
	}
}

// --- CostCalculator benchmarks ---

func BenchmarkCostCalculator_Calculate(b *testing.B) {
	calc := NewCostCalculator()
	usage := Usage{InputTokens: 1000, OutputTokens: 2000, TotalTokens: 3000}
	models := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514", "gemini-2.5-pro", "unknown-model"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calc.Calculate(models[i%len(models)], usage)
	}
}

func BenchmarkCostCalculator_Calculate_Parallel(b *testing.B) {
	calc := NewCostCalculator()
	usage := Usage{InputTokens: 1000, OutputTokens: 2000, TotalTokens: 3000}
	models := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514", "gemini-2.5-pro"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = calc.Calculate(models[i%len(models)], usage)
			i++
		}
	})
}

func BenchmarkCostCalculator_SetPrice(b *testing.B) {
	calc := NewCostCalculator()
	entry := CostEntry{InputCostPer1K: 0.01, OutputCostPer1K: 0.03}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calc.SetPrice(fmt.Sprintf("model-%d", i), entry)
	}
}

// --- Retry benchmarks ---

func BenchmarkRetryConfig_CalculateDelay(b *testing.B) {
	cfg := RetryConfig{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.2,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.CalculateDelay((i % 10) + 1)
	}
}

func BenchmarkRetryConfig_CalculateDelay_NoJitter(b *testing.B) {
	cfg := RetryConfig{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		Jitter:          0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.CalculateDelay((i % 10) + 1)
	}
}

func BenchmarkWithRetry_ImmediateSuccess(b *testing.B) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          0,
	}
	fn := func(ctx context.Context) error { return nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithRetry(ctx, cfg, fn)
	}
}

func BenchmarkWithRetryResult_ImmediateSuccess(b *testing.B) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          0,
	}
	resp := benchResponse()
	fn := func(ctx context.Context) (*Response, error) { return resp, nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = WithRetryResult(ctx, cfg, fn)
	}
}

func BenchmarkIsTransientError(b *testing.B) {
	errs := []error{
		NewRetryableError(errors.New("transient")),
		&ProviderError{StatusCode: 429, Type: ErrorTypeRateLimit},
		&ProviderError{StatusCode: 500, Type: ErrorTypeServerError},
		errors.New("permanent"),
		nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsTransientError(errs[i%len(errs)])
	}
}

func BenchmarkIsRetryable(b *testing.B) {
	retryable := NewRetryableError(errors.New("retry me"))
	normal := errors.New("don't retry")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			_ = IsRetryable(retryable)
		} else {
			_ = IsRetryable(normal)
		}
	}
}

// --- Rate Limiter benchmarks ---

func BenchmarkLimiter_Allow(b *testing.B) {
	lim := NewLimiter(1000000, 1000000) // Very high rate to avoid blocking

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lim.Allow()
	}
}

func BenchmarkLimiter_AllowN(b *testing.B) {
	lim := NewLimiter(1000000, 1000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lim.AllowN(5)
	}
}

func BenchmarkLimiter_Allow_Parallel(b *testing.B) {
	lim := NewLimiter(1000000, 1000000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = lim.Allow()
		}
	})
}

func BenchmarkLimiter_Wait(b *testing.B) {
	lim := NewLimiter(1000000, 1000000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lim.Wait(ctx)
	}
}

func BenchmarkLimiter_Wait_Parallel(b *testing.B) {
	lim := NewLimiter(1000000, 1000000)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = lim.Wait(ctx)
		}
	})
}

func BenchmarkLimiter_Tokens(b *testing.B) {
	lim := NewLimiter(100, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lim.Tokens()
	}
}

// --- Middleware benchmarks ---

func BenchmarkMiddleware_Chain_1(b *testing.B) {
	mw := WithCompleteHook(func(ctx context.Context, req *Request, resp *Response, err error) {})
	chain := Chain(mw)
	fn := chain(noOpComplete())
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(ctx, req)
	}
}

func BenchmarkMiddleware_Chain_3(b *testing.B) {
	hook := func(ctx context.Context, req *Request, resp *Response, err error) {}
	mw1 := WithCompleteHook(hook)
	mw2 := WithCompleteHook(hook)
	mw3 := WithCompleteHook(hook)
	chain := Chain(mw1, mw2, mw3)
	fn := chain(noOpComplete())
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(ctx, req)
	}
}

func BenchmarkMiddleware_Chain_5(b *testing.B) {
	hook := func(ctx context.Context, req *Request, resp *Response, err error) {}
	mws := make([]Middleware, 5)
	for i := range mws {
		mws[i] = WithCompleteHook(hook)
	}
	chain := Chain(mws...)
	fn := chain(noOpComplete())
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(ctx, req)
	}
}

func BenchmarkMiddleware_Timing(b *testing.B) {
	mw := WithTiming(func(req *Request, durationMS float64) {})
	chain := Chain(mw)
	fn := chain(noOpComplete())
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(ctx, req)
	}
}

func BenchmarkMiddleware_RateLimit(b *testing.B) {
	lim := NewLimiter(1000000, 1000000) // Very high rate
	mw := WithRateLimit(lim)
	chain := Chain(mw)
	fn := chain(noOpComplete())
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(ctx, req)
	}
}

// --- Chat (end-to-end) benchmarks ---

func BenchmarkChat_NoMiddleware(b *testing.B) {
	tracer := setupBenchTracer(b)
	provider := &mockProvider{name: "bench-llm"}
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Chat(ctx, req, provider)
	}
}

func BenchmarkChat_WithMiddleware(b *testing.B) {
	tracer := setupBenchTracer(b)
	provider := &mockProvider{name: "bench-llm"}
	req := benchRequest()
	ctx := context.Background()
	hook := func(ctx context.Context, req *Request, resp *Response, err error) {}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Chat(ctx, req, provider,
			WithCallMiddleware(WithCompleteHook(hook)),
		)
	}
}

func BenchmarkChat_WithRetry(b *testing.B) {
	tracer := setupBenchTracer(b)
	provider := &mockProvider{name: "bench-llm"}
	req := benchRequest()
	ctx := context.Background()
	retryCfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tracer.Chat(ctx, req, provider, WithCallRetry(retryCfg))
	}
}

func BenchmarkChatStream(b *testing.B) {
	tracer := setupBenchTracer(b)
	provider := &mockProvider{name: "bench-llm", supportsStream: true}
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := tracer.ChatStream(ctx, req, provider)
		for range ch {
		}
	}
}

// --- Error classification benchmarks ---

func BenchmarkClassifyHTTPStatus(b *testing.B) {
	statuses := []int{200, 400, 401, 403, 404, 429, 500, 502, 503}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ClassifyHTTPStatus(statuses[i%len(statuses)])
	}
}

func BenchmarkNewProviderError(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewProviderError("openai", 429, "too many requests")
	}
}

func BenchmarkIsRateLimit(b *testing.B) {
	errs := []error{
		&ProviderError{Type: ErrorTypeRateLimit},
		&ProviderError{Type: ErrorTypeServerError},
		errors.New("other"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsRateLimit(errs[i%len(errs)])
	}
}

func BenchmarkIsServerError(b *testing.B) {
	errs := []error{
		&ProviderError{Type: ErrorTypeServerError},
		&ProviderError{Type: ErrorTypeAuth},
		errors.New("other"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsServerError(errs[i%len(errs)])
	}
}

// --- Provider benchmarks ---

func BenchmarkProvider_Complete(b *testing.B) {
	provider := &mockProvider{name: "bench-llm"}
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Complete(ctx, req)
	}
}

func BenchmarkProvider_Stream(b *testing.B) {
	provider := &mockProvider{name: "bench-llm", supportsStream: true}
	req := benchRequest()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := provider.Stream(ctx, req)
		for range ch {
		}
	}
}

// --- HTTP handler mock for benchmarking middleware in isolation ---

func BenchmarkProviderError_ErrorString(b *testing.B) {
	err := &ProviderError{
		Provider:   "openai",
		StatusCode: http.StatusTooManyRequests,
		Code:       "rate_limit",
		Message:    "You have exceeded your rate limit",
		Type:       ErrorTypeRateLimit,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}
