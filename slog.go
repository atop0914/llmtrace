package llmtrace

import (
	"context"
	"log/slog"
	"time"
)

// SlogConfig configures the slog logging middleware.
type SlogConfig struct {
	// Logger is the slog.Logger to use. If nil, slog.Default() is used.
	Logger *slog.Logger

	// Level controls the log level for completion messages.
	// Default: slog.LevelInfo
	Level slog.Level

	// ErrorLevel controls the log level for error messages.
	// Default: slog.LevelError
	ErrorLevel slog.Level

	// LogRequest enables logging of request details (model, message count).
	// Default: true
	LogRequest bool

	// LogResponse enables logging of response details (tokens, latency, cost).
	// Default: true
	LogResponse bool

	// LogErrors enables logging of errors.
	// Default: true
	LogErrors bool

	// SanitizeContent enables sanitization of sensitive data in logs.
	// When true, error messages and string attributes are passed through
	// the Sanitizer to redact API keys, tokens, emails, and other PII.
	// Default: true
	SanitizeContent bool

	// Sanitizer is the sanitizer instance to use for redacting sensitive data.
	// If nil and SanitizeContent is true, a default Sanitizer is created.
	// Set to nil explicitly with SanitizeContent=false to disable.
	Sanitizer *Sanitizer
}

// DefaultSlogConfig returns a SlogConfig with sensible defaults.
func DefaultSlogConfig() SlogConfig {
	return SlogConfig{
		Level:           slog.LevelInfo,
		ErrorLevel:      slog.LevelError,
		LogRequest:      true,
		LogResponse:     true,
		LogErrors:       true,
		SanitizeContent: true,
	}
}

// getSanitizer returns the configured sanitizer or a default one.
func (c SlogConfig) getSanitizer() *Sanitizer {
	if c.Sanitizer != nil {
		return c.Sanitizer
	}
	return NewSanitizer()
}

// sanitizeAttr sanitizes a string slog attribute if SanitizeContent is enabled.
func (c SlogConfig) sanitizeAttr(a slog.Attr) slog.Attr {
	if !c.SanitizeContent {
		return a
	}
	if a.Value.Kind() == slog.KindString {
		sanitized := c.getSanitizer().Sanitize(a.Value.String())
		return slog.String(a.Key, sanitized)
	}
	return a
}

// sanitizeAttrs sanitizes all string attributes in a slice.
func (c SlogConfig) sanitizeAttrs(attrs []slog.Attr) []slog.Attr {
	if !c.SanitizeContent {
		return attrs
	}
	for i := range attrs {
		attrs[i] = c.sanitizeAttr(attrs[i])
	}
	return attrs
}

// WithSlog returns a Middleware that logs LLM calls using structured logging.
//
// Usage:
//
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithSlog(llmtrace.DefaultSlogConfig())),
//	)
func WithSlog(cfg SlogConfig) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			logger := cfg.Logger
			if logger == nil {
				logger = slog.Default()
			}

			start := time.Now()

			// Log request start
			if cfg.LogRequest {
				attrs := []slog.Attr{
					slog.String("model", req.Model),
					slog.Int("message_count", len(req.Messages)),
				}
				if req.MaxTokens != nil {
					attrs = append(attrs, slog.Int("max_tokens", *req.MaxTokens))
				}
				if req.Temperature != nil {
					attrs = append(attrs, slog.Float64("temperature", *req.Temperature))
				}
				logger.LogAttrs(ctx, cfg.Level, "llm request started", attrs...)
			}

			resp, err := next(ctx, req)
			latency := time.Since(start)

			// Log errors
			if err != nil && cfg.LogErrors {
				errMsg := err.Error()
				if cfg.SanitizeContent {
					errMsg = cfg.getSanitizer().Sanitize(errMsg)
				}
				errorAttrs := []slog.Attr{
					slog.String("model", req.Model),
					slog.Duration("latency", latency),
					slog.String("error", errMsg),
				}
				// Add provider error details if available
				if pe, ok := err.(*ProviderError); ok {
					errorAttrs = append(errorAttrs,
						slog.String("provider", pe.Provider),
						slog.Int("status_code", pe.StatusCode),
						slog.String("error_code", pe.Code),
						slog.String("error_type", string(pe.Type)),
					)
				}
				logger.LogAttrs(ctx, cfg.ErrorLevel, "llm request failed", errorAttrs...)
				return nil, err
			}

			// Log response
			if cfg.LogResponse && resp != nil {
				respAttrs := []slog.Attr{
					slog.String("model", resp.Model),
					slog.String("provider", resp.Provider),
					slog.Duration("latency", latency),
					slog.Int("input_tokens", resp.Usage.InputTokens),
					slog.Int("output_tokens", resp.Usage.OutputTokens),
					slog.Int("total_tokens", resp.Usage.TotalTokens),
					slog.String("finish_reason", resp.FinishReason),
				}
				if resp.ID != "" {
					respAttrs = append(respAttrs, slog.String("response_id", resp.ID))
				}
				if cfg.SanitizeContent {
					respAttrs = cfg.sanitizeAttrs(respAttrs)
				}
				logger.LogAttrs(ctx, cfg.Level, "llm request completed", respAttrs...)
			}

			return resp, nil
		}
	}
}

// WithStreamSlog returns a StreamMiddleware that logs streaming LLM calls.
//
// Usage:
//
//	ch, err := tracer.ChatStream(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithStreamSlog(llmtrace.DefaultSlogConfig())),
//	)
func WithStreamSlog(cfg SlogConfig) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			logger := cfg.Logger
			if logger == nil {
				logger = slog.Default()
			}

			start := time.Now()

			// Log stream start
			if cfg.LogRequest {
				attrs := []slog.Attr{
					slog.String("model", req.Model),
					slog.Int("message_count", len(req.Messages)),
					slog.String("type", "stream"),
				}
				if req.MaxTokens != nil {
					attrs = append(attrs, slog.Int("max_tokens", *req.MaxTokens))
				}
				logger.LogAttrs(ctx, cfg.Level, "llm stream started", attrs...)
			}

			ch, err := next(ctx, req)
			if err != nil {
				if cfg.LogErrors {
					errMsg := err.Error()
					if cfg.SanitizeContent {
						errMsg = cfg.getSanitizer().Sanitize(errMsg)
					}
					logger.LogAttrs(ctx, cfg.ErrorLevel, "llm stream failed",
						slog.String("model", req.Model),
						slog.String("error", errMsg),
					)
				}
				return nil, err
			}

			// Wrap channel to log completion
			out := make(chan StreamChunk)
			go func() {
				defer close(out)
				var chunkCount int
				var lastUsage *Usage
				var streamErr error
				for chunk := range ch {
					if chunk.Error != nil {
						streamErr = chunk.Error
					}
					if chunk.Usage != nil {
						lastUsage = chunk.Usage
					}
					chunkCount++
					out <- chunk
				}

				latency := time.Since(start)

				if streamErr != nil && cfg.LogErrors {
					errMsg := streamErr.Error()
					if cfg.SanitizeContent {
						errMsg = cfg.getSanitizer().Sanitize(errMsg)
					}
					logger.LogAttrs(ctx, cfg.ErrorLevel, "llm stream error",
						slog.String("model", req.Model),
						slog.Duration("latency", latency),
						slog.Int("chunks_received", chunkCount),
						slog.String("error", errMsg),
					)
				} else if cfg.LogResponse {
					attrs := []slog.Attr{
						slog.String("model", req.Model),
						slog.Duration("latency", latency),
						slog.Int("chunks_received", chunkCount),
						slog.String("type", "stream"),
					}
					if lastUsage != nil {
						attrs = append(attrs,
							slog.Int("input_tokens", lastUsage.InputTokens),
							slog.Int("output_tokens", lastUsage.OutputTokens),
							slog.Int("total_tokens", lastUsage.TotalTokens),
						)
					}
					logger.LogAttrs(ctx, cfg.Level, "llm stream completed", attrs...)
				}
			}()

			return out, nil
		}
	}
}
