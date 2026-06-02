package llmtrace

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRateLimitExceeded is returned when a request cannot proceed because
// the rate limit has been exceeded and the context deadline is reached.
var ErrRateLimitExceeded = errors.New("llmtrace: rate limit exceeded")

// Limiter provides rate limiting using a token bucket algorithm.
// It controls how many LLM API calls can be made per unit of time.
//
// The token bucket fills at a steady rate (Rate) up to a maximum capacity (Burst).
// Each call consumes one token. If no tokens are available, the caller blocks
// until a token is available or the context is canceled.
//
// A Limiter is safe for concurrent use by multiple goroutines.
type Limiter struct {
	mu       sync.Mutex
	rate     float64   // tokens per second
	burst    int       // maximum bucket size
	tokens   float64   // current available tokens
	lastTime time.Time // last time tokens were added
}

// NewLimiter creates a new rate limiter.
//
// rate is the number of tokens added per second (sustained throughput).
// burst is the maximum number of tokens that can accumulate (peak throughput).
//
// Example:
//
//	// 10 requests per second, burst of 20
//	lim := llmtrace.NewLimiter(10, 20)
//
//	// Use as middleware
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithRateLimit(lim)),
//	)
func NewLimiter(rate float64, burst int) *Limiter {
	if rate < 0 {
		rate = 0
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst), // start with full bucket
		lastTime: time.Now(),
	}
}

// Allow reports whether a single token is available right now.
// It does not block. This is useful for non-blocking checks.
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN reports whether N tokens are available right now.
// It does not block.
func (l *Limiter) AllowN(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= float64(n) {
		l.tokens -= float64(n)
		return true
	}
	return false
}

// Wait blocks until one token is available or the context is canceled.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.WaitN(ctx, 1)
}

// WaitN blocks until N tokens are available or the context is canceled.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	for {
		l.mu.Lock()
		l.refill()
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		// Calculate how long to wait for enough tokens
		deficit := float64(n) - l.tokens
		var waitTime time.Duration
		if l.rate > 0 {
			waitTime = time.Duration(deficit / l.rate * float64(time.Second))
		}
		l.mu.Unlock()

		if waitTime <= 0 {
			// Rate is 0 or negative, can never satisfy
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return ErrRateLimitExceeded
			}
		}

		// Add a small buffer to ensure tokens are replenished
		waitTime += time.Millisecond

		// Wait for tokens to replenish or context cancellation
		timer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Try again
		}
	}
}

// refill adds tokens based on elapsed time. Must be called with lock held.
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastTime).Seconds()
	l.lastTime = now

	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
}

// Tokens returns the current number of available tokens (approximate).
// Useful for monitoring and testing.
func (l *Limiter) Tokens() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	return l.tokens
}

// Rate returns the configured rate (tokens per second).
func (l *Limiter) Rate() float64 {
	return l.rate
}

// Burst returns the configured burst size.
func (l *Limiter) Burst() int {
	return l.burst
}

// WithRateLimit returns a Middleware that enforces rate limiting on completion calls.
// Each call consumes one token from the limiter. If no tokens are available,
// the call blocks until a token is available or the context is canceled.
//
// Example:
//
//	lim := llmtrace.NewLimiter(10, 20) // 10 req/s, burst 20
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithRateLimit(lim)),
//	)
func WithRateLimit(lim *Limiter) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			if err := lim.Wait(ctx); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}

// WithStreamRateLimit returns a StreamMiddleware that enforces rate limiting
// on streaming calls. The token is consumed when the stream is initiated.
func WithStreamRateLimit(lim *Limiter) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			if err := lim.Wait(ctx); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}

// RateLimitConfig configures a rate limiter for use with Chat/ChatStream options.
type RateLimitConfig struct {
	// Rate is the number of requests allowed per second.
	Rate float64

	// Burst is the maximum number of requests that can be made in a burst.
	Burst int
}

// WithCallRateLimit returns a ChatOption that applies rate limiting to a Chat call.
//
// Example:
//
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallRateLimit(llmtrace.RateLimitConfig{
//	        Rate:  10,  // 10 requests per second
//	        Burst: 20,  // burst up to 20
//	    }),
//	)
func WithCallRateLimit(cfg RateLimitConfig) ChatOption {
	return func(o *ChatOptions) {
		lim := NewLimiter(cfg.Rate, cfg.Burst)
		o.Middlewares = append(o.Middlewares, WithRateLimit(lim))
	}
}
