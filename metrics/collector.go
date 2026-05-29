package metrics

import (
	"context"
	"time"

	"github.com/atop0914/llmtrace"
)

// LLMCollector collects LLM-specific metrics using a Registry.
// It provides a Middleware that can be plugged into the Tracer pipeline.
type LLMCollector struct {
	// RequestTotal counts total LLM requests.
	RequestTotal *CounterVec

	// RequestDuration tracks request latency distribution.
	RequestDuration *HistogramVec

	// TokensTotal counts total tokens processed.
	TokensTotal *CounterVec

	// InputTokensTotal counts total input tokens.
	InputTokensTotal *CounterVec

	// OutputTokensTotal counts total output tokens.
	OutputTokensTotal *CounterVec

	// CostTotal tracks cumulative cost in USD.
	CostTotal *CounterVec

	// ActiveRequests tracks currently in-flight requests.
	ActiveRequests *GaugeVec

	// ErrorsTotal counts failed requests by error type.
	ErrorsTotal *CounterVec

	// StreamChunksTotal counts total stream chunks received.
	StreamChunksTotal *CounterVec

	registry *Registry
}

// NewLLMCollector creates a new LLM metrics collector attached to the given registry.
func NewLLMCollector(reg *Registry) *LLMCollector {
	return &LLMCollector{
		RequestTotal: reg.RegisterCounter(
			"requests_total",
			"Total number of LLM requests",
			[]string{"provider", "model"},
		),
		RequestDuration: reg.RegisterHistogram(
			"request_duration_seconds",
			"LLM request duration in seconds",
			[]string{"provider", "model"},
			DurationBuckets(),
		),
		TokensTotal: reg.RegisterCounter(
			"tokens_total",
			"Total tokens processed (input + output)",
			[]string{"provider", "model"},
		),
		InputTokensTotal: reg.RegisterCounter(
			"input_tokens_total",
			"Total input tokens sent",
			[]string{"provider", "model"},
		),
		OutputTokensTotal: reg.RegisterCounter(
			"output_tokens_total",
			"Total output tokens received",
			[]string{"provider", "model"},
		),
		CostTotal: reg.RegisterCounter(
			"cost_usd_total",
			"Total cost in USD",
			[]string{"provider", "model"},
		),
		ActiveRequests: reg.RegisterGauge(
			"active_requests",
			"Number of currently in-flight LLM requests",
			[]string{"provider"},
		),
		ErrorsTotal: reg.RegisterCounter(
			"errors_total",
			"Total failed LLM requests by error category",
			[]string{"provider", "error_type"},
		),
		StreamChunksTotal: reg.RegisterCounter(
			"stream_chunks_total",
			"Total stream chunks received",
			[]string{"provider", "model"},
		),
		registry: reg,
	}
}

// Middleware returns an llmtrace.Middleware that records metrics for each request.
func (c *LLMCollector) Middleware() llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			provider := getProviderFromContext(ctx)
			model := req.Model

			c.ActiveRequests.With(provider).Inc()
			start := time.Now()

			resp, err := next(ctx, req)

			duration := time.Since(start).Seconds()
			c.ActiveRequests.With(provider).Dec()
			c.RequestTotal.With(provider, model).Inc()
			c.RequestDuration.With(provider, model).Observe(duration)

			if err != nil {
				errType := classifyError(err)
				c.ErrorsTotal.With(provider, errType).Inc()
			} else if resp != nil {
				c.TokensTotal.With(provider, model).Add(float64(resp.Usage.TotalTokens))
				c.InputTokensTotal.With(provider, model).Add(float64(resp.Usage.InputTokens))
				c.OutputTokensTotal.With(provider, model).Add(float64(resp.Usage.OutputTokens))
				if resp.Latency > 0 {
					// Also track actual response latency (may differ from wall time)
					_ = resp.Latency
				}
			}

			return resp, err
		}
	}
}

// StreamMiddleware returns an llmtrace.StreamMiddleware that records metrics for streaming requests.
func (c *LLMCollector) StreamMiddleware() llmtrace.StreamMiddleware {
	return func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			provider := getProviderFromContext(ctx)
			model := req.Model

			c.ActiveRequests.With(provider).Inc()
			start := time.Now()

			ch, err := next(ctx, req)
			if err != nil {
				c.ActiveRequests.With(provider).Dec()
				c.RequestTotal.With(provider, model).Inc()
				c.ErrorsTotal.With(provider, classifyError(err)).Inc()
				return nil, err
			}

			// Wrap the channel to track stream completion
			out := make(chan llmtrace.StreamChunk)
			go func() {
				defer close(out)
				defer func() {
					c.ActiveRequests.With(provider).Dec()
					c.RequestTotal.With(provider, model).Inc()
					c.RequestDuration.With(provider, model).Observe(time.Since(start).Seconds())
				}()

				for chunk := range ch {
					c.StreamChunksTotal.With(provider, model).Inc()
					if chunk.Error != nil {
						c.ErrorsTotal.With(provider, classifyError(chunk.Error)).Inc()
					}
					if chunk.Usage != nil {
						c.TokensTotal.With(provider, model).Add(float64(chunk.Usage.TotalTokens))
						c.InputTokensTotal.With(provider, model).Add(float64(chunk.Usage.InputTokens))
						c.OutputTokensTotal.With(provider, model).Add(float64(chunk.Usage.OutputTokens))
					}
					out <- chunk
				}
			}()

			return out, nil
		}
	}
}

// RecordCost manually records a cost entry (useful when cost is computed externally).
func (c *LLMCollector) RecordCost(provider, model string, cost float64) {
	c.CostTotal.With(provider, model).Add(cost)
}

// classifyError maps an error to a Prometheus label value.
func classifyError(err error) string {
	if llmtrace.IsRateLimit(err) {
		return "rate_limit"
	}
	if llmtrace.IsAuthError(err) {
		return "auth"
	}
	if llmtrace.IsInvalidRequest(err) {
		return "invalid_request"
	}
	if llmtrace.IsServerError(err) {
		return "server_error"
	}
	return "unknown"
}

// providerContextKey is the context key for the provider name.
type providerContextKey struct{}

// ContextWithProvider returns a new context with the provider name.
// This is used internally by Chat/ChatStream to propagate the provider.
func ContextWithProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, providerContextKey{}, provider)
}

// getProviderFromContext extracts the provider name from context.
func getProviderFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(providerContextKey{}).(string); ok {
		return v
	}
	return "unknown"
}
