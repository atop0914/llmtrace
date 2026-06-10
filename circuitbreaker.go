// Package llmtrace provides a circuit breaker for LLM provider calls.
//
// The circuit breaker prevents cascading failures by monitoring error rates
// and temporarily blocking requests to unhealthy providers. It implements
// the standard three-state pattern: Closed → Open → Half-Open → Closed.
//
// Usage:
//
//	cb := llmtrace.NewCircuitBreaker(llmtrace.CircuitBreakerConfig{
//	    FailureThreshold: 5,
//	    SuccessThreshold: 2,
//	    Timeout:          30 * time.Second,
//	})
//
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithCircuitBreaker(cb)),
//	)
package llmtrace

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// StateClosed is the normal operating state. Requests pass through
	// and failures are counted. When failures reach the threshold,
	// the circuit transitions to Open.
	StateClosed CircuitState = iota

	// StateOpen blocks all requests. After the timeout period,
	// the circuit transitions to Half-Open to test recovery.
	StateOpen

	// StateHalfOpen allows a limited number of requests through
	// to test if the provider has recovered. If enough succeed,
	// the circuit returns to Closed. Any failure sends it back to Open.
	StateHalfOpen
)

// String returns a human-readable name for the circuit state.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Sentinel errors for circuit breaker.
var (
	// ErrCircuitOpen is returned when the circuit breaker is in the Open state
	// and the timeout has not yet elapsed.
	ErrCircuitOpen = errors.New("llmtrace: circuit breaker is open")

	// ErrTooManyRequests is returned when the circuit breaker is in Half-Open
	// state and the maximum concurrent half-open requests has been reached.
	ErrTooManyRequests = errors.New("llmtrace: too many requests in half-open state")
)

// CircuitBreakerConfig configures a CircuitBreaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures in Closed state
	// that trip the breaker to Open. Default: 5.
	FailureThreshold int

	// SuccessThreshold is the number of consecutive successes in Half-Open
	// state needed to close the circuit. Default: 2.
	SuccessThreshold int

	// Timeout is how long the circuit stays Open before transitioning
	// to Half-Open. Default: 30s.
	Timeout time.Duration

	// MaxHalfOpenRequests is the maximum number of concurrent requests
	// allowed in Half-Open state. Default: 1.
	MaxHalfOpenRequests int

	// IsFailure determines whether an error should count as a failure.
	// By default, all non-nil errors count as failures.
	// Override to exclude certain error types (e.g., client-side errors).
	IsFailure func(err error) bool

	// OnStateChange is called when the circuit breaker changes state.
	OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns a config with sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    5,
		SuccessThreshold:    2,
		Timeout:             30 * time.Second,
		MaxHalfOpenRequests: 1,
	}
}

// CircuitBreaker monitors request outcomes and prevents calls to
// unhealthy providers. It is safe for concurrent use.
type CircuitBreaker struct {
	config CircuitBreakerConfig

	mu              sync.Mutex
	state           CircuitState
	failures        int // consecutive failures in Closed state
	successes       int // consecutive successes in Half-Open state
	halfOpenCount   int // current in-flight requests in Half-Open
	lastFailureTime time.Time
	totalRequests   int64
	totalFailures   int64
	totalRejected   int64 // requests rejected by open circuit
}

// NewCircuitBreaker creates a new CircuitBreaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxHalfOpenRequests <= 0 {
		cfg.MaxHalfOpenRequests = 1
	}
	return &CircuitBreaker{
		config: cfg,
		state:  StateClosed,
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.getStateUnsafe()
}

// getStateUnsafe returns the state, performing lazy transitions.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) getStateUnsafe() CircuitState {
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) >= cb.config.Timeout {
		cb.transitionTo(StateHalfOpen)
	}
	return cb.state
}

// transitionTo changes state and fires the OnStateChange callback.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}
	old := cb.state
	cb.state = newState

	// Reset counters on state transition
	switch newState {
	case StateClosed:
		cb.failures = 0
		cb.successes = 0
		cb.halfOpenCount = 0
	case StateHalfOpen:
		cb.successes = 0
		cb.halfOpenCount = 0
	case StateOpen:
		cb.halfOpenCount = 0
	}

	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(old, newState)
	}
}

// Allow checks if a request is allowed under the current circuit state.
// For Half-Open state, it increments the in-flight counter.
// Returns nil if allowed, or ErrCircuitOpen / ErrTooManyRequests if rejected.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.getStateUnsafe()

	switch state {
	case StateClosed:
		cb.totalRequests++
		return nil

	case StateOpen:
		cb.totalRejected++
		return ErrCircuitOpen

	case StateHalfOpen:
		if cb.halfOpenCount >= cb.config.MaxHalfOpenRequests {
			cb.totalRejected++
			return ErrTooManyRequests
		}
		cb.halfOpenCount++
		cb.totalRequests++
		return nil

	default:
		return nil
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failures = 0
	case StateHalfOpen:
		cb.successes++
		cb.halfOpenCount--
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()
	cb.totalFailures++

	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
		}
	case StateHalfOpen:
		cb.halfOpenCount--
		cb.transitionTo(StateOpen)
	}
}

// Execute wraps a function call with circuit breaker protection.
// It checks if the call is allowed, executes it, and records the outcome.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.Allow(); err != nil {
		return err
	}

	err := fn()

	if err != nil {
		// Check custom failure classifier
		if cb.config.IsFailure == nil || cb.config.IsFailure(err) {
			cb.RecordFailure()
		} else {
			// Non-failure error (e.g., client error) — record as success
			cb.RecordSuccess()
		}
		return err
	}

	cb.RecordSuccess()
	return nil
}

// Reset forces the circuit breaker back to Closed state and clears counters.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(StateClosed)
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenCount = 0
	cb.totalRequests = 0
	cb.totalFailures = 0
	cb.totalRejected = 0
}

// Snapshot returns a point-in-time view of the circuit breaker's state.
type CircuitSnapshot struct {
	// State is the current circuit state.
	State CircuitState
	// ConsecutiveFailures is the current count of consecutive failures (Closed state).
	ConsecutiveFailures int
	// ConsecutiveSuccesses is the current count of consecutive successes (Half-Open state).
	ConsecutiveSuccesses int
	// TotalRequests is the total number of allowed requests.
	TotalRequests int64
	// TotalFailures is the total number of recorded failures.
	TotalFailures int64
	// TotalRejected is the total number of rejected requests.
	TotalRejected int64
}

// Snapshot returns the current state of the circuit breaker.
func (cb *CircuitBreaker) Snapshot() CircuitSnapshot {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.getStateUnsafe()
	return CircuitSnapshot{
		State:                state,
		ConsecutiveFailures:  cb.failures,
		ConsecutiveSuccesses: cb.successes,
		TotalRequests:        cb.totalRequests,
		TotalFailures:        cb.totalFailures,
		TotalRejected:        cb.totalRejected,
	}
}

// IsHealthy reports whether the circuit is in Closed state (healthy).
func (cb *CircuitBreaker) IsHealthy() bool {
	return cb.State() == StateClosed
}

// WithCircuitBreaker returns a Middleware that protects CompleteFunc calls
// with the given CircuitBreaker.
func WithCircuitBreaker(cb *CircuitBreaker) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			if err := cb.Allow(); err != nil {
				return nil, err
			}

			resp, err := next(ctx, req)
			if err != nil {
				if cb.config.IsFailure == nil || cb.config.IsFailure(err) {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}
				return resp, err
			}

			cb.RecordSuccess()
			return resp, err
		}
	}
}

// WithStreamCircuitBreaker returns a StreamMiddleware that protects StreamFunc calls
// with the given CircuitBreaker.
func WithStreamCircuitBreaker(cb *CircuitBreaker) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			if err := cb.Allow(); err != nil {
				return nil, err
			}

			ch, err := next(ctx, req)
			if err != nil {
				if cb.config.IsFailure == nil || cb.config.IsFailure(err) {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}
				return nil, err
			}

			// Monitor stream for completion errors
			wrapped := make(chan StreamChunk, cap(ch))
			go func() {
				defer close(wrapped)
				var hasError bool
				for chunk := range ch {
					if chunk.Error != nil {
						hasError = true
					}
					wrapped <- chunk
				}
				if hasError {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}
			}()

			return wrapped, nil
		}
	}
}

// HealthCheck performs a lightweight health check on a provider.
// It sends a minimal request and reports whether the provider is responsive.
type HealthCheck struct {
	// Provider is the provider to check.
	Provider Provider

	// Timeout for the health check request. Default: 10s.
	Timeout time.Duration

	// Model to use for the check. If empty, uses provider's default.
	Model string
}

// NewHealthCheck creates a HealthCheck for the given provider.
func NewHealthCheck(p Provider) *HealthCheck {
	return &HealthCheck{
		Provider: p,
		Timeout:  10 * time.Second,
	}
}

// CheckResult holds the outcome of a health check.
type CheckResult struct {
	// Healthy reports whether the provider responded successfully.
	Healthy bool
	// Latency is the round-trip time of the health check.
	Latency time.Duration
	// Error is the failure reason, if any.
	Error error
}

// Check performs the health check and returns the result.
func (hc *HealthCheck) Check(ctx context.Context) CheckResult {
	if hc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, hc.Timeout)
		defer cancel()
	}

	model := hc.Model
	if model == "" {
		model = hc.Provider.DefaultModel()
	}

	maxTokens := 1
	start := time.Now()
	_, err := hc.Provider.Complete(ctx, &Request{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "ping"},
		},
		MaxTokens: &maxTokens,
	})
	latency := time.Since(start)

	return CheckResult{
		Healthy: err == nil,
		Latency: latency,
		Error:   err,
	}
}
