package llmtrace

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"time"
)

// RetryConfig controls retry behavior for transient errors.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts.
	// 0 means no retries.
	MaxRetries int

	// InitialInterval is the base delay before the first retry.
	// Default: 500ms.
	InitialInterval time.Duration

	// MaxInterval is the maximum delay between retries.
	// Default: 30s.
	MaxInterval time.Duration

	// Multiplier is the exponential backoff multiplier.
	// Default: 2.0.
	Multiplier float64

	// Jitter adds randomness to the delay (0.0 to 1.0).
	// Default: 0.2 (20% jitter).
	Jitter float64
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.2,
	}
}

// RetryableError marks an error as retryable.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// NewRetryableError wraps an error to indicate it should be retried.
func NewRetryableError(err error) *RetryableError {
	return &RetryableError{Err: err}
}

// IsRetryable reports whether an error is retryable.
func IsRetryable(err error) bool {
	var retryable *RetryableError
	return errors.As(err, &retryable)
}

// IsTransientError reports whether an error represents a transient condition
// that may succeed on retry (network errors, 429, 5xx status codes).
func IsTransientError(err error) bool {
	if IsRetryable(err) {
		return true
	}

	// Check for HTTP status-based retryability
	var apiErr interface{ StatusCode() int }
	if errors.As(err, &apiErr) {
		code := apiErr.StatusCode()
		return code == http.StatusTooManyRequests || code >= 500
	}

	// Check for common transient error patterns
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}

	return false
}

// CalculateDelay computes the backoff delay for a given attempt.
func (c *RetryConfig) CalculateDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := float64(c.InitialInterval) * math.Pow(c.Multiplier, float64(attempt-1))

	// Apply jitter
	if c.Jitter > 0 {
		jitterRange := delay * c.Jitter
		delay = delay - jitterRange + rand.Float64()*2*jitterRange //nolint:gosec
	}

	d := time.Duration(delay)
	if d > c.MaxInterval {
		d = c.MaxInterval
	}
	return d
}

// WithRetry executes fn with retry logic based on the config.
// It only retries on transient/retryable errors.
func WithRetry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := cfg.CalculateDelay(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Check context before calling function
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		if !IsTransientError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// WithRetryResult is like WithRetry but returns a typed result.
func WithRetryResult[T any](ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := cfg.CalculateDelay(attempt)
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsTransientError(err) {
			return zero, err
		}
	}
	return zero, lastErr
}
