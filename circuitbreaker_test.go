package llmtrace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             10 * time.Second,
		MaxHalfOpenRequests: 1,
	})
	if cb.State() != StateClosed {
		t.Errorf("initial state = %v, want StateClosed", cb.State())
	}
}

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})
	if cb.State() != StateClosed {
		t.Errorf("initial state = %v, want StateClosed", cb.State())
	}

	// Trigger with default threshold (5)
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	if cb.State() != StateOpen {
		t.Errorf("state after 5 failures = %v, want StateOpen", cb.State())
	}
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             10 * time.Second,
		MaxHalfOpenRequests: 1,
	})

	// Should stay closed with fewer failures than threshold
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Errorf("state after 2 failures = %v, want StateClosed", cb.State())
	}

	// Third failure trips the breaker
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("state after 3 failures = %v, want StateOpen", cb.State())
	}
}

func TestCircuitBreaker_OpenRejects(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             1 * time.Hour, // Long timeout to stay open
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.State())
	}

	// All requests should be rejected
	err := cb.Allow()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Allow() = %v, want ErrCircuitOpen", err)
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    2,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should transition to Half-Open when we check state
	if cb.State() != StateHalfOpen {
		t.Errorf("state after timeout = %v, want StateHalfOpen", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenSuccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    2,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	})

	// Trip the breaker
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Should be half-open
	if err := cb.Allow(); err != nil {
		t.Fatalf("Allow() in half-open = %v", err)
	}

	// First success
	cb.RecordSuccess()
	if cb.State() != StateHalfOpen {
		t.Errorf("state after 1 success = %v, want StateHalfOpen", cb.State())
	}

	// Second success should close the circuit
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Errorf("state after 2 successes = %v, want StateClosed", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    2,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// Allow one request in half-open
	cb.Allow()

	// Failure should send it back to Open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("state after half-open failure = %v, want StateOpen", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenMaxRequests(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    2,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)

	// First request allowed
	if err := cb.Allow(); err != nil {
		t.Fatalf("first Allow() = %v", err)
	}

	// Second request should be rejected (max 1 concurrent)
	err := cb.Allow()
	if !errors.Is(err, ErrTooManyRequests) {
		t.Errorf("second Allow() = %v, want ErrTooManyRequests", err)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             1 * time.Hour,
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %v", cb.State())
	}

	cb.Reset()
	if cb.State() != StateClosed {
		t.Errorf("state after Reset() = %v, want StateClosed", cb.State())
	}

	snap := cb.Snapshot()
	if snap.TotalFailures != 0 || snap.TotalRequests != 0 || snap.TotalRejected != 0 {
		t.Errorf("counters not reset: %+v", snap)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    1,
		Timeout:             1 * time.Second,
		MaxHalfOpenRequests: 1,
	})

	// Two failures, then a success
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	// Should still be closed
	if cb.State() != StateClosed {
		t.Errorf("state = %v, want StateClosed", cb.State())
	}

	// Failure counter should be reset — 2 more failures shouldn't trip
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Errorf("state after reset failures = %v, want StateClosed", cb.State())
	}
}

func TestCircuitBreaker_Execute(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             1 * time.Hour,
		MaxHalfOpenRequests: 1,
	})

	// Successful execution
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("Execute() = %v, want nil", err)
	}

	// Failed execution
	testErr := errors.New("provider error")
	err = cb.Execute(func() error { return testErr })
	if !errors.Is(err, testErr) {
		t.Errorf("Execute() = %v, want %v", err, testErr)
	}
}

func TestCircuitBreaker_Execute_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             1 * time.Hour,
		MaxHalfOpenRequests: 1,
	})

	// Trip the breaker
	cb.RecordFailure()
	cb.RecordFailure()

	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute() = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Error("function should not be called when circuit is open")
	}
}

func TestCircuitBreaker_Execute_CustomIsFailure(t *testing.T) {
	// Only server errors count as failures
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          1 * time.Second,
		IsFailure: func(err error) bool {
			return IsServerError(err)
		},
	})

	clientErr := NewProviderError("openai", 400, "bad request")
	serverErr := NewProviderError("openai", 500, "internal error")

	// Client errors don't count as failures
	for i := 0; i < 5; i++ {
		cb.Execute(func() error { return clientErr })
	}
	if cb.State() != StateClosed {
		t.Errorf("client errors should not trip breaker, state = %v", cb.State())
	}

	// Server errors do
	cb.Execute(func() error { return serverErr })
	cb.Execute(func() error { return serverErr })
	if cb.State() != StateOpen {
		t.Errorf("server errors should trip breaker, state = %v", cb.State())
	}
}

func TestCircuitBreaker_Snapshot(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             1 * time.Second,
		MaxHalfOpenRequests: 1,
	})

	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordFailure()

	snap := cb.Snapshot()
	if snap.State != StateClosed {
		t.Errorf("Snapshot.State = %v, want StateClosed", snap.State)
	}
	if snap.ConsecutiveFailures != 1 {
		t.Errorf("Snapshot.ConsecutiveFailures = %d, want 1", snap.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_IsHealthy(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          50 * time.Millisecond,
	})

	if !cb.IsHealthy() {
		t.Error("new breaker should be healthy")
	}

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsHealthy() {
		t.Error("open breaker should not be healthy")
	}

	time.Sleep(60 * time.Millisecond)
	// Half-open is not healthy
	if cb.IsHealthy() {
		t.Error("half-open breaker should not be healthy")
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    10,
		SuccessThreshold:    3,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 2,
	})

	var wg sync.WaitGroup
	var successes, failures atomic.Int64

	// Concurrent requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := cb.Execute(func() error {
				if i%3 == 0 {
					return errors.New("simulated error")
				}
				return nil
			})
			if err != nil {
				if errors.Is(err, ErrCircuitOpen) || errors.Is(err, ErrTooManyRequests) {
					failures.Add(1)
				}
			} else {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()

	// No panics or races — just verify it's still functional
	state := cb.State()
	if state != StateClosed && state != StateOpen && state != StateHalfOpen {
		t.Errorf("invalid state after concurrent use: %v", state)
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	var mu sync.Mutex

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(from, to CircuitState) {
			mu.Lock()
			transitions = append(transitions, from.String()+"->"+to.String())
			mu.Unlock()
		},
	})

	cb.RecordFailure()
	cb.RecordFailure() // closed->open

	time.Sleep(60 * time.Millisecond)
	cb.Allow() // open->half-open (lazy transition)

	cb.RecordSuccess() // half-open->closed

	mu.Lock()
	defer mu.Unlock()
	if len(transitions) != 3 {
		t.Fatalf("expected 3 transitions, got %d: %v", len(transitions), transitions)
	}
	expected := []string{"closed->open", "open->half-open", "half-open->closed"}
	for i, tr := range transitions {
		if tr != expected[i] {
			t.Errorf("transition[%d] = %q, want %q", i, tr, expected[i])
		}
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestCircuitBreaker_RecoveryCycle(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 2,
	})

	for cycle := 0; cycle < 3; cycle++ {
		// Trip the breaker
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		if cb.State() != StateOpen {
			t.Fatalf("cycle %d: expected Open, got %v", cycle, cb.State())
		}

		// Wait for timeout
		time.Sleep(60 * time.Millisecond)

		// Transition to half-open and recover
		cb.Allow()
		cb.RecordSuccess()
		cb.RecordSuccess()

		if cb.State() != StateClosed {
			t.Fatalf("cycle %d: expected Closed after recovery, got %v", cycle, cb.State())
		}
	}
}

// Mock provider for middleware tests
type mockCircuitProvider struct {
	completeFunc func(ctx context.Context, req *Request) (*Response, error)
	streamFunc   func(ctx context.Context, req *Request) (<-chan StreamChunk, error)
}

func (m *mockCircuitProvider) Name() string            { return "mock" }
func (m *mockCircuitProvider) DefaultModel() string    { return "mock-model" }
func (m *mockCircuitProvider) SupportsStreaming() bool { return true }
func (m *mockCircuitProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &Response{Content: "ok"}, nil
}
func (m *mockCircuitProvider) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Content: "ok"}
	close(ch)
	return ch, nil
}

func TestWithCircuitBreaker_Success(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          1 * time.Second,
	})

	provider := &mockCircuitProvider{}
	mw := WithCircuitBreaker(cb)
	fn := mw(provider.Complete)

	resp, err := fn(context.Background(), &Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("response = %q, want %q", resp.Content, "ok")
	}

	snap := cb.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("total requests = %d, want 1", snap.TotalRequests)
	}
}

func TestWithCircuitBreaker_FailureTrips(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Hour,
	})

	provider := &mockCircuitProvider{
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return nil, errors.New("server error")
		},
	}
	mw := WithCircuitBreaker(cb)
	fn := mw(provider.Complete)

	req := &Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}}

	// Two failures trip the breaker
	fn(context.Background(), req)
	fn(context.Background(), req)

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want StateOpen", cb.State())
	}

	// Third call should be rejected
	_, err := fn(context.Background(), req)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("third call = %v, want ErrCircuitOpen", err)
	}
}

func TestWithStreamCircuitBreaker_Success(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		Timeout:          1 * time.Second,
	})

	provider := &mockCircuitProvider{}
	mw := WithStreamCircuitBreaker(cb)
	fn := mw(provider.Stream)

	ch, err := fn(context.Background(), &Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	// Give goroutine time to record success
	time.Sleep(10 * time.Millisecond)

	snap := cb.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("total requests = %d, want 1", snap.TotalRequests)
	}
}

func TestWithStreamCircuitBreaker_InitError(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Hour,
	})

	provider := &mockCircuitProvider{
		streamFunc: func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			return nil, errors.New("connection failed")
		},
	}
	mw := WithStreamCircuitBreaker(cb)
	fn := mw(provider.Stream)

	req := &Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}}
	fn(context.Background(), req)
	fn(context.Background(), req)

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want StateOpen", cb.State())
	}
}

func TestWithStreamCircuitBreaker_StreamError(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Hour,
	})

	provider := &mockCircuitProvider{
		streamFunc: func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Content: "partial"}
			ch <- StreamChunk{Error: errors.New("stream error")}
			close(ch)
			return ch, nil
		},
	}
	mw := WithStreamCircuitBreaker(cb)
	fn := mw(provider.Stream)

	req := &Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}}

	// Two stream failures should trip the breaker
	ch1, _ := fn(context.Background(), req)
	for range ch1 {
	}
	time.Sleep(10 * time.Millisecond)

	ch2, _ := fn(context.Background(), req)
	for range ch2 {
	}
	time.Sleep(10 * time.Millisecond)

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want StateOpen", cb.State())
	}
}

func TestWithCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Hour,
	})

	// Trip the breaker
	cb.RecordFailure()
	cb.RecordFailure()

	mw := WithCircuitBreaker(cb)
	fn := mw(func(ctx context.Context, req *Request) (*Response, error) {
		t.Error("function should not be called when circuit is open")
		return nil, nil
	})

	_, err := fn(context.Background(), &Request{})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("error = %v, want ErrCircuitOpen", err)
	}
}

func TestWithStreamCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Hour,
	})

	cb.RecordFailure()
	cb.RecordFailure()

	mw := WithStreamCircuitBreaker(cb)
	fn := mw(func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		t.Error("function should not be called when circuit is open")
		return nil, nil
	})

	_, err := fn(context.Background(), &Request{})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("error = %v, want ErrCircuitOpen", err)
	}
}

func TestHealthCheck(t *testing.T) {
	provider := &mockCircuitProvider{
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return &Response{Content: "pong"}, nil
		},
	}

	hc := NewHealthCheck(provider)
	result := hc.Check(context.Background())

	if !result.Healthy {
		t.Error("expected healthy")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	provider := &mockCircuitProvider{
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return nil, errors.New("connection refused")
		},
	}

	hc := NewHealthCheck(provider)
	result := hc.Check(context.Background())

	if result.Healthy {
		t.Error("expected unhealthy")
	}
	if result.Error == nil {
		t.Error("expected error")
	}
}

func TestHealthCheck_Timeout(t *testing.T) {
	provider := &mockCircuitProvider{
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return nil, nil
			}
		},
	}

	hc := NewHealthCheck(provider)
	hc.Timeout = 50 * time.Millisecond

	result := hc.Check(context.Background())
	if result.Healthy {
		t.Error("expected unhealthy due to timeout")
	}
}

func TestHealthCheck_CustomModel(t *testing.T) {
	var gotModel string
	provider := &mockCircuitProvider{
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			gotModel = req.Model
			return &Response{Content: "ok"}, nil
		},
	}

	hc := NewHealthCheck(provider)
	hc.Model = "gpt-4o"
	hc.Check(context.Background())

	if gotModel != "gpt-4o" {
		t.Errorf("model = %q, want %q", gotModel, "gpt-4o")
	}
}
