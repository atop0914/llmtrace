package tokencount

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/atop0914/llmtrace"
)

// TokenStats tracks cumulative token usage across multiple LLM calls.
// All fields are updated atomically and are safe for concurrent use.
type TokenStats struct {
	TotalInputTokens  atomic.Int64
	TotalOutputTokens atomic.Int64
	TotalTokens       atomic.Int64
	TotalRequests     atomic.Int64
	TotalCostMicros   atomic.Int64 // cost in micro-USD (1e-6), for integer arithmetic
	RejectedRequests  atomic.Int64 // requests rejected due to context overflow
}

// Snapshot returns a point-in-time copy of the stats.
func (s *TokenStats) Snapshot() TokenStatsSnapshot {
	return TokenStatsSnapshot{
		TotalInputTokens:  s.TotalInputTokens.Load(),
		TotalOutputTokens: s.TotalOutputTokens.Load(),
		TotalTokens:       s.TotalTokens.Load(),
		TotalRequests:     s.TotalRequests.Load(),
		TotalCostUSD:      float64(s.TotalCostMicros.Load()) / 1e6,
		RejectedRequests:  s.RejectedRequests.Load(),
	}
}

// TokenStatsSnapshot is an immutable point-in-time view of TokenStats.
type TokenStatsSnapshot struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalTokens       int64
	TotalRequests     int64
	TotalCostUSD      float64
	RejectedRequests  int64
}

// TokenUsageEvent contains information about a single LLM call's token usage.
type TokenUsageEvent struct {
	// Model is the model used.
	Model string

	// InputTokens is the actual input token count (from response or estimated).
	InputTokens int

	// OutputTokens is the actual output token count.
	OutputTokens int

	// EstimatedCostUSD is the estimated cost for this call.
	EstimatedCostUSD float64

	// ContextUsageRatio is inputTokens / contextWindow (0.0-1.0+).
	ContextUsageRatio float64

	// PreCallCheck is the validation result (nil if validation was skipped).
	PreCallCheck *CheckResult

	// Duration is the total call duration.
	Duration time.Duration

	// Rejected indicates the call was rejected due to context overflow.
	Rejected bool

	// Error is any error from the call.
	Error error
}

// TokenCountConfig configures the token counting middleware.
type TokenCountConfig struct {
	// Manager provides model registry and validation.
	// If nil, a default manager is created.
	Manager *Manager

	// Stats tracks cumulative usage. If nil, a new one is created.
	Stats *TokenStats

	// RejectOnOverflow rejects requests that exceed the context window.
	// Default: false (allows the request to proceed, relying on the provider).
	RejectOnOverflow bool

	// OnUsage is called after each request with token usage details.
	// Optional.
	OnUsage func(event TokenUsageEvent)

	// EstimateIfMissing estimates token counts from message text when
	// the provider doesn't return usage data. Default: true.
	EstimateIfMissing bool
}

// TokenCountOption configures the token counting middleware.
type TokenCountOption func(*TokenCountConfig)

// WithManager sets a custom token manager.
func WithManager(m *Manager) TokenCountOption {
	return func(c *TokenCountConfig) {
		c.Manager = m
	}
}

// WithTokenStats sets a shared stats tracker.
func WithTokenStats(s *TokenStats) TokenCountOption {
	return func(c *TokenCountConfig) {
		c.Stats = s
	}
}

// WithRejectOnOverflow enables rejecting requests that exceed context window.
func WithRejectOnOverflow() TokenCountOption {
	return func(c *TokenCountConfig) {
		c.RejectOnOverflow = true
	}
}

// WithUsageCallback sets a callback invoked after each LLM call.
func WithUsageCallback(fn func(TokenUsageEvent)) TokenCountOption {
	return func(c *TokenCountConfig) {
		c.OnUsage = fn
	}
}

// WithEstimateIfMissing controls whether to estimate tokens when provider
// doesn't return usage. Default is true.
func WithEstimateIfMissing(estimate bool) TokenCountOption {
	return func(c *TokenCountConfig) {
		c.EstimateIfMissing = estimate
	}
}

// defaultConfig returns a TokenCountConfig with sensible defaults.
func defaultConfig(opts []TokenCountOption) *TokenCountConfig {
	cfg := &TokenCountConfig{
		EstimateIfMissing: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.Manager == nil {
		cfg.Manager = NewManager()
	}
	if cfg.Stats == nil {
		cfg.Stats = &TokenStats{}
	}
	return cfg
}

// ErrContextOverflow is returned when a request exceeds the model's context window.
type ErrContextOverflow struct {
	Model       string
	InputTokens int
	ContextSize int
}

func (e *ErrContextOverflow) Error() string {
	return "context overflow: model " + e.Model +
		" input " + itoa(e.InputTokens) +
		" tokens exceeds context window " + itoa(e.ContextSize)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// WithTokenCount returns a middleware that tracks token usage and optionally
// validates context window before each LLM call.
//
// Usage:
//
//	stats := &tokencount.TokenStats{}
//	mw := tokencount.WithTokenCount(
//	    tokencount.WithTokenStats(stats),
//	    tokencount.WithRejectOnOverflow(),
//	    tokencount.WithUsageCallback(func(e tokencount.TokenUsageEvent) {
//	        log.Printf("tokens: in=%d out=%d cost=$%.4f", e.InputTokens, e.OutputTokens, e.EstimatedCostUSD)
//	    }),
//	)
//	provider := openai.New(openai.WithAPIKey("sk-..."))
//	tracer := llmtrace.NewTracer("my-service")
//	resp, err := tracer.Chat(ctx, req, provider, llmtrace.WithCallMiddleware(mw))
func WithTokenCount(opts ...TokenCountOption) llmtrace.Middleware {
	cfg := defaultConfig(opts)

	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			start := time.Now()

			// Resolve model name
			model := req.Model

			// Pre-call: validate context window
			maxTokens := 0
			if req.MaxTokens != nil {
				maxTokens = *req.MaxTokens
			}

			// Convert llmtrace.Message to tokencount.Message
			messages := make([]Message, len(req.Messages))
			for i, m := range req.Messages {
				messages[i] = Message{Role: string(m.Role), Content: m.Content}
			}

			check := cfg.Manager.ValidateRequest(model, messages, maxTokens)

			// Reject if overflow configured
			if cfg.RejectOnOverflow && !check.FitsContext {
				cfg.Stats.RejectedRequests.Add(1)
				cfg.Stats.TotalRequests.Add(1)

				if cfg.OnUsage != nil {
					cfg.OnUsage(TokenUsageEvent{
						Model:             model,
						InputTokens:       check.InputTokens,
						ContextUsageRatio: check.UsageRatio,
						PreCallCheck:      &check,
						Duration:          time.Since(start),
						Rejected:          true,
						Error:             &ErrContextOverflow{Model: model, InputTokens: check.InputTokens, ContextSize: check.Model.ContextWindow},
					})
				}

				return nil, &ErrContextOverflow{
					Model:       model,
					InputTokens: check.InputTokens,
					ContextSize: check.Model.ContextWindow,
				}
			}

			// Execute the actual call
			resp, err := next(ctx, req)
			duration := time.Since(start)

			// Post-call: extract or estimate token usage
			var inputTokens, outputTokens int
			var costUSD float64

			if err == nil && resp != nil {
				inputTokens = resp.Usage.InputTokens
				outputTokens = resp.Usage.OutputTokens

				// Estimate if provider didn't return usage
				if cfg.EstimateIfMissing && inputTokens == 0 && outputTokens == 0 {
					inputTokens = check.InputTokens
					// Estimate output from content length
					if resp.Content != "" {
						outputTokens = EstimateTokens(resp.Content, check.Model.CharsPerToken)
					}
				}

				// Calculate cost
				costUSD, _ = cfg.Manager.EstimateCost(model, inputTokens, outputTokens)

				// Update cumulative stats
				cfg.Stats.TotalInputTokens.Add(int64(inputTokens))
				cfg.Stats.TotalOutputTokens.Add(int64(outputTokens))
				cfg.Stats.TotalTokens.Add(int64(inputTokens + outputTokens))
				cfg.Stats.TotalCostMicros.Add(int64(costUSD * 1e6))
			}

			cfg.Stats.TotalRequests.Add(1)

			// Callback
			if cfg.OnUsage != nil {
				usageRatio := check.UsageRatio
				if inputTokens > 0 && check.Model.ContextWindow > 0 {
					usageRatio = float64(inputTokens) / float64(check.Model.ContextWindow)
				}

				cfg.OnUsage(TokenUsageEvent{
					Model:             model,
					InputTokens:       inputTokens,
					OutputTokens:      outputTokens,
					EstimatedCostUSD:  costUSD,
					ContextUsageRatio: usageRatio,
					PreCallCheck:      &check,
					Duration:          duration,
					Error:             err,
				})
			}

			return resp, err
		}
	}
}

// WithStreamTokenCount returns a stream middleware that tracks token usage
// from streaming responses. It reads the final chunk's Usage field.
func WithStreamTokenCount(opts ...TokenCountOption) llmtrace.StreamMiddleware {
	cfg := defaultConfig(opts)

	return func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			start := time.Now()
			model := req.Model

			// Pre-call validation
			maxTokens := 0
			if req.MaxTokens != nil {
				maxTokens = *req.MaxTokens
			}

			messages := make([]Message, len(req.Messages))
			for i, m := range req.Messages {
				messages[i] = Message{Role: string(m.Role), Content: m.Content}
			}

			check := cfg.Manager.ValidateRequest(model, messages, maxTokens)

			if cfg.RejectOnOverflow && !check.FitsContext {
				cfg.Stats.RejectedRequests.Add(1)
				cfg.Stats.TotalRequests.Add(1)

				if cfg.OnUsage != nil {
					cfg.OnUsage(TokenUsageEvent{
						Model:             model,
						InputTokens:       check.InputTokens,
						ContextUsageRatio: check.UsageRatio,
						PreCallCheck:      &check,
						Duration:          time.Since(start),
						Rejected:          true,
						Error:             &ErrContextOverflow{Model: model, InputTokens: check.InputTokens, ContextSize: check.Model.ContextWindow},
					})
				}

				return nil, &ErrContextOverflow{
					Model:       model,
					InputTokens: check.InputTokens,
					ContextSize: check.Model.ContextWindow,
				}
			}

			ch, err := next(ctx, req)
			if err != nil {
				cfg.Stats.TotalRequests.Add(1)
				if cfg.OnUsage != nil {
					cfg.OnUsage(TokenUsageEvent{
						Model:        model,
						PreCallCheck: &check,
						Duration:     time.Since(start),
						Error:        err,
					})
				}
				return ch, err
			}

			// Wrap the channel to intercept the final chunk
			wrapped := make(chan llmtrace.StreamChunk)
			go func() {
				defer close(wrapped)
				var totalContent string
				var finalUsage *llmtrace.Usage

				for chunk := range ch {
					totalContent += chunk.Content
					if chunk.Usage != nil {
						finalUsage = chunk.Usage
					}
					wrapped <- chunk
				}

				duration := time.Since(start)

				// Extract usage from final chunk
				var inputTokens, outputTokens int
				if finalUsage != nil {
					inputTokens = finalUsage.InputTokens
					outputTokens = finalUsage.OutputTokens
				}

				// Estimate if missing
				if cfg.EstimateIfMissing && inputTokens == 0 && outputTokens == 0 {
					inputTokens = check.InputTokens
					if totalContent != "" {
						outputTokens = EstimateTokens(totalContent, check.Model.CharsPerToken)
					}
				}

				costUSD, _ := cfg.Manager.EstimateCost(model, inputTokens, outputTokens)

				// Update stats
				cfg.Stats.TotalInputTokens.Add(int64(inputTokens))
				cfg.Stats.TotalOutputTokens.Add(int64(outputTokens))
				cfg.Stats.TotalTokens.Add(int64(inputTokens + outputTokens))
				cfg.Stats.TotalRequests.Add(1)
				cfg.Stats.TotalCostMicros.Add(int64(costUSD * 1e6))

				if cfg.OnUsage != nil {
					usageRatio := check.UsageRatio
					if inputTokens > 0 && check.Model.ContextWindow > 0 {
						usageRatio = float64(inputTokens) / float64(check.Model.ContextWindow)
					}

					cfg.OnUsage(TokenUsageEvent{
						Model:             model,
						InputTokens:       inputTokens,
						OutputTokens:      outputTokens,
						EstimatedCostUSD:  costUSD,
						ContextUsageRatio: usageRatio,
						PreCallCheck:      &check,
						Duration:          duration,
					})
				}
			}()

			return wrapped, nil
		}
	}
}
