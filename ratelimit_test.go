package llmtrace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	lim := NewLimiter(10, 20)
	if lim.Rate() != 10 {
		t.Errorf("Rate() = %f, want 10", lim.Rate())
	}
	if lim.Burst() != 20 {
		t.Errorf("Burst() = %d, want 20", lim.Burst())
	}
}

func TestNewLimiter_ZeroBurst(t *testing.T) {
	lim := NewLimiter(10, 0)
	if lim.Burst() != 1 {
		t.Errorf("Burst() = %d, want 1 (minimum)", lim.Burst())
	}
}

func TestNewLimiter_NegativeRate(t *testing.T) {
	lim := NewLimiter(-5, 10)
	if lim.Rate() != 0 {
		t.Errorf("Rate() = %f, want 0 (clamped)", lim.Rate())
	}
}

func TestLimiter_Allow(t *testing.T) {
	lim := NewLimiter(10, 5)

	// Should allow up to burst
	for i := 0; i < 5; i++ {
		if !lim.Allow() {
			t.Errorf("Allow() = false on iteration %d, want true", i)
		}
	}

	// Next one should fail (bucket empty)
	if lim.Allow() {
		t.Error("Allow() = true after exhausting burst, want false")
	}
}

func TestLimiter_AllowN(t *testing.T) {
	lim := NewLimiter(10, 10)

	if !lim.AllowN(5) {
		t.Error("AllowN(5) = false, want true")
	}
	if !lim.AllowN(5) {
		t.Error("AllowN(5) = false on second call, want true")
	}
	// Bucket should be empty now
	if lim.AllowN(1) {
		t.Error("AllowN(1) = true after exhaustion, want false")
	}
}

func TestLimiter_AllowN_ExceedsBurst(t *testing.T) {
	lim := NewLimiter(10, 5)

	// Requesting more than burst should always fail
	if lim.AllowN(6) {
		t.Error("AllowN(6) = true with burst=5, want false")
	}
}

func TestLimiter_Tokens(t *testing.T) {
	lim := NewLimiter(10, 10)

	tokens := lim.Tokens()
	if tokens < 9.9 || tokens > 10.1 {
		t.Errorf("Tokens() = %f, want ~10", tokens)
	}

	lim.AllowN(3)
	tokens = lim.Tokens()
	if tokens < 6.9 || tokens > 7.1 {
		t.Errorf("Tokens() = %f after AllowN(3), want ~7", tokens)
	}
}

func TestLimiter_Refill(t *testing.T) {
	lim := NewLimiter(100, 10) // 100/s, burst 10

	// Exhaust tokens
	lim.AllowN(10)
	if lim.Allow() {
		t.Error("Allow() = true after exhaustion, want false")
	}

	// Wait for refill (100 tokens/sec = 1 token per 10ms)
	time.Sleep(60 * time.Millisecond)

	if !lim.Allow() {
		t.Error("Allow() = false after refill period, want true")
	}
}

func TestLimiter_Wait_Success(t *testing.T) {
	lim := NewLimiter(100, 1) // 100/s, burst 1

	// Consume the initial token
	if !lim.Allow() {
		t.Fatal("initial Allow() = false")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := lim.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	// Should have waited ~10ms for one token at 100/s
	if elapsed > 100*time.Millisecond {
		t.Errorf("Wait() took %v, expected < 100ms", elapsed)
	}
}

func TestLimiter_Wait_ContextCanceled(t *testing.T) {
	lim := NewLimiter(0.001, 1) // Very slow rate
	lim.Allow()                 // Exhaust

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := lim.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Wait() = %v, want context.Canceled", err)
	}
}

func TestLimiter_Wait_ContextDeadline(t *testing.T) {
	lim := NewLimiter(0.01, 1) // 0.01 tokens/sec = 100s per token
	lim.Allow()                // Exhaust

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := lim.Wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait() = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("Wait() took %v, expected ~50ms", elapsed)
	}
}

func TestLimiter_Wait_MultipleTokens(t *testing.T) {
	lim := NewLimiter(1000, 10) // 1000/s, burst 10

	ctx := context.Background()

	// Should succeed immediately (burst = 10)
	start := time.Now()
	err := lim.WaitN(ctx, 5)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WaitN(5) = %v, want nil", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("WaitN(5) took %v, expected immediate", elapsed)
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	lim := NewLimiter(100, 50) // 100/s, burst 50
	var count atomic.Int32
	var wg sync.WaitGroup

	// Launch 100 goroutines trying to acquire tokens
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if err := lim.Wait(ctx); err == nil {
				count.Add(1)
			}
		}()
	}

	wg.Wait()

	// With burst 50 and 200ms window at 100/s, we expect ~50-70 tokens
	got := count.Load()
	if got < 50 || got > 80 {
		t.Errorf("concurrent acquisitions = %d, want 50-80", got)
	}
}

func TestWithRateLimit_Middleware(t *testing.T) {
	lim := NewLimiter(100, 1)
	called := false

	inner := func(ctx context.Context, req *Request) (*Response, error) {
		called = true
		return &Response{Content: "ok"}, nil
	}

	mw := WithRateLimit(lim)
	fn := mw(inner)

	resp, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("fn() = %v, want nil", err)
	}
	if !called {
		t.Error("inner function was not called")
	}
	if resp.Content != "ok" {
		t.Errorf("resp.Content = %q, want %q", resp.Content, "ok")
	}
}

func TestWithRateLimit_Middleware_Blocks(t *testing.T) {
	lim := NewLimiter(100, 1) // burst 1
	lim.Allow()                // exhaust

	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "ok"}, nil
	}

	mw := WithRateLimit(lim)
	fn := mw(inner)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := fn(ctx, &Request{Model: "test"})
	elapsed := time.Since(start)

	// Should have waited ~10ms for token
	if err != nil {
		t.Fatalf("fn() = %v, want nil", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Errorf("fn() returned in %v, expected some wait", elapsed)
	}
}

func TestWithRateLimit_Middleware_ContextCanceled(t *testing.T) {
	lim := NewLimiter(0.001, 1) // Very slow
	lim.Allow()                 // exhaust

	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "ok"}, nil
	}

	mw := WithRateLimit(lim)
	fn := mw(inner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fn(ctx, &Request{Model: "test"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("fn() = %v, want context.Canceled", err)
	}
}

func TestWithStreamRateLimit_Middleware(t *testing.T) {
	lim := NewLimiter(100, 1)

	inner := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		ch <- StreamChunk{Content: "hello"}
		close(ch)
		return ch, nil
	}

	mw := WithStreamRateLimit(lim)
	fn := mw(inner)

	ch, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("fn() = %v, want nil", err)
	}

	chunk := <-ch
	if chunk.Content != "hello" {
		t.Errorf("chunk.Content = %q, want %q", chunk.Content, "hello")
	}
}

func TestWithStreamRateLimit_ContextCanceled(t *testing.T) {
	lim := NewLimiter(0.001, 1) // Very slow
	lim.Allow()                 // exhaust

	inner := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		close(ch)
		return ch, nil
	}

	mw := WithStreamRateLimit(lim)
	fn := mw(inner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fn(ctx, &Request{Model: "test"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("fn() = %v, want context.Canceled", err)
	}
}

func TestWithCallRateLimit_ChatOption(t *testing.T) {
	cfg := ChatOptions{}
	opt := WithCallRateLimit(RateLimitConfig{
		Rate:  10,
		Burst: 5,
	})
	opt(&cfg)

	if len(cfg.Middlewares) != 1 {
		t.Errorf("len(Middlewares) = %d, want 1", len(cfg.Middlewares))
	}
}

func TestErrRateLimitExceeded(t *testing.T) {
	if ErrRateLimitExceeded.Error() != "llmtrace: rate limit exceeded" {
		t.Errorf("ErrRateLimitExceeded.Error() = %q", ErrRateLimitExceeded.Error())
	}
}

func TestLimiter_IntegrationWithChat(t *testing.T) {
	// Test rate limiter integrated with Chat via middleware
	lim := NewLimiter(100, 1)

	provider := &mockProvider{
		name:           "mock",
		defaultModel:   "mock-model",
		supportsStream: true,
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return &Response{Content: "response", Model: req.Model}, nil
		},
	}

	tracer := NewTracer("test", WithProvider("test"))

	// First call should succeed immediately (burst = 1)
	resp, err := tracer.Chat(context.Background(), &Request{Model: "test"}, provider,
		WithCallMiddleware(WithRateLimit(lim)),
	)
	if err != nil {
		t.Fatalf("Chat() = %v, want nil", err)
	}
	if resp.Content != "response" {
		t.Errorf("resp.Content = %q, want %q", resp.Content, "response")
	}

	// Second call should wait briefly for token refill
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	resp, err = tracer.Chat(ctx, &Request{Model: "test"}, provider,
		WithCallMiddleware(WithRateLimit(lim)),
	)
	if err != nil {
		t.Fatalf("Chat() second call = %v, want nil", err)
	}
}
