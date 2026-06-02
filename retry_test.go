package llmtrace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialInterval != 500*time.Millisecond {
		t.Errorf("InitialInterval = %v, want 500ms", cfg.InitialInterval)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", cfg.Multiplier)
	}
	if cfg.Jitter != 0.2 {
		t.Errorf("Jitter = %f, want 0.2", cfg.Jitter)
	}
}

func TestRetryableError(t *testing.T) {
	inner := errors.New("connection refused")
	err := NewRetryableError(inner)

	if err.Error() != "connection refused" {
		t.Errorf("Error() = %q, want %q", err.Error(), "connection refused")
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap() should return inner error")
	}
	if !IsRetryable(err) {
		t.Error("IsRetryable should be true for RetryableError")
	}
	if IsRetryable(inner) {
		t.Error("IsRetryable should be false for plain error")
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil error", nil, false},
		{"plain error", errors.New("bad request"), false},
		{"retryable error", NewRetryableError(errors.New("timeout")), true},
		{"wrapped retryable", fmt.Errorf("wrapped: %w", NewRetryableError(errors.New("timeout"))), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientError(tt.err); got != tt.transient {
				t.Errorf("IsTransientError() = %v, want %v", got, tt.transient)
			}
		})
	}
}

func TestCalculateDelay(t *testing.T) {
	cfg := RetryConfig{
		InitialInterval: 1 * time.Second,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		Jitter:          0, // no jitter for predictable tests
	}

	// Attempt 0 should be 0
	if d := cfg.CalculateDelay(0); d != 0 {
		t.Errorf("CalculateDelay(0) = %v, want 0", d)
	}

	// Attempt 1 should be InitialInterval
	if d := cfg.CalculateDelay(1); d != 1*time.Second {
		t.Errorf("CalculateDelay(1) = %v, want 1s", d)
	}

	// Attempt 2 should be 2s
	if d := cfg.CalculateDelay(2); d != 2*time.Second {
		t.Errorf("CalculateDelay(2) = %v, want 2s", d)
	}

	// Attempt 3 should be 4s
	if d := cfg.CalculateDelay(3); d != 4*time.Second {
		t.Errorf("CalculateDelay(3) = %v, want 4s", d)
	}

	// Attempt 10 should be capped at MaxInterval
	if d := cfg.CalculateDelay(10); d != 10*time.Second {
		t.Errorf("CalculateDelay(10) = %v, want 10s (max)", d)
	}
}

func TestCalculateDelay_WithJitter(t *testing.T) {
	cfg := RetryConfig{
		InitialInterval: 1 * time.Second,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.5,
	}

	// With jitter, delays should vary but be within expected range
	for i := 0; i < 100; i++ {
		d := cfg.CalculateDelay(1)
		// With 50% jitter on 1s: range is 0.5s to 1.5s
		if d < 500*time.Millisecond || d > 1500*time.Millisecond {
			t.Errorf("CalculateDelay(1) with jitter = %v, want 500ms-1500ms", d)
		}
	}
}

func TestWithRetry_Success(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 3}
	calls := 0

	err := WithRetry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return nil
	})

	if err != nil {
		t.Errorf("WithRetry() error = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithRetry_RetriesOnTransientError(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          0,
	}
	calls := 0

	err := WithRetry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return NewRetryableError(errors.New("transient"))
		}
		return nil
	})

	if err != nil {
		t.Errorf("WithRetry() error = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestWithRetry_StopsOnNonTransientError(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
		Jitter:          0,
	}
	calls := 0
	permErr := errors.New("permanent error")

	err := WithRetry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return permErr
	})

	if !errors.Is(err, permErr) {
		t.Errorf("WithRetry() error = %v, want %v", err, permErr)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry for non-transient)", calls)
	}
}

func TestWithRetry_ExhaustsRetries(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      2,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     50 * time.Millisecond,
		Multiplier:      2.0,
		Jitter:          0,
	}
	calls := 0

	err := WithRetry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return NewRetryableError(errors.New("always failing"))
	})

	if err == nil {
		t.Error("WithRetry() should return error after exhausting retries")
	}
	if calls != 3 { // 1 initial + 2 retries
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestWithRetry_RespectsContextCancellation(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      10,
		InitialInterval: 10 * time.Second, // very long delay
		Multiplier:      1.0,              // keep delay constant
		Jitter:          0,
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	// Cancel immediately after first call returns
	err := WithRetry(ctx, cfg, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			cancel() // cancel context during the first call
		}
		return NewRetryableError(errors.New("transient"))
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("WithRetry() error = %v, want context.Canceled", err)
	}
}

func TestWithRetryResult_Success(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 3}

	result, err := WithRetryResult(context.Background(), cfg, func(ctx context.Context) (string, error) {
		return "hello", nil
	})

	if err != nil {
		t.Errorf("WithRetryResult() error = %v, want nil", err)
	}
	if result != "hello" {
		t.Errorf("WithRetryResult() result = %q, want %q", result, "hello")
	}
}

func TestWithRetryResult_RetriesAndSucceeds(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
		Jitter:          0,
	}
	calls := 0

	result, err := WithRetryResult(context.Background(), cfg, func(ctx context.Context) (int, error) {
		calls++
		if calls < 2 {
			return 0, NewRetryableError(errors.New("transient"))
		}
		return 42, nil
	})

	if err != nil {
		t.Errorf("WithRetryResult() error = %v, want nil", err)
	}
	if result != 42 {
		t.Errorf("WithRetryResult() result = %d, want 42", result)
	}
}

// testHTTPStatusError simulates an HTTP error with a status code.
type testHTTPStatusError struct {
	code int
	msg  string
}

func (e *testHTTPStatusError) Error() string     { return e.msg }
func (e *testHTTPStatusError) StatusCode() int    { return e.code }

func TestIsTransientError_HTTPStatus(t *testing.T) {
	tests := []struct {
		code      int
		transient bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			err := &testHTTPStatusError{code: tt.code, msg: "test"}
			if got := IsTransientError(err); got != tt.transient {
				t.Errorf("IsTransientError(status %d) = %v, want %v", tt.code, got, tt.transient)
			}
		})
	}
}
