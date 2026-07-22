package moderation

import (
	"context"
	"fmt"

	"github.com/atop0914/llmtrace"
)

// contextKey is unexported to avoid collisions.
type contextKey struct{}

// WithResult attaches a moderation Result to the context.
func WithResult(ctx context.Context, r *Result) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// ResultFromContext retrieves the moderation Result from context, if any.
func ResultFromContext(ctx context.Context) (*Result, bool) {
	r, ok := ctx.Value(contextKey{}).(*Result)
	return r, ok
}

// blockedError is returned when content is blocked by moderation.
type blockedError struct {
	reason string
}

func (e *blockedError) Error() string {
	return fmt.Sprintf("content blocked by moderation: %s", e.reason)
}

// BlockedError returns true if the error is a moderation block.
func IsBlocked(err error) bool {
	_, ok := err.(*blockedError)
	return ok
}

// Middleware returns a llmtrace.Middleware that moderates user input
// before passing it to the next handler. If the content is blocked,
// it returns an error without calling next.
func Middleware(engine *Engine) llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			// Check user messages
			for _, msg := range req.Messages {
				if msg.Role == "user" || msg.Role == "system" {
					result, err := engine.CheckInput(ctx, msg.Content)
					if err != nil {
						return nil, err
					}
					ctx = WithResult(ctx, result)
					if !result.Allowed {
						return nil, &blockedError{reason: "input blocked by moderation policy"}
					}
				}
			}
			return next(ctx, req)
		}
	}
}

// OutputMiddleware returns a llmtrace.Middleware that moderates LLM output
// after the handler has produced a response. If the output is blocked,
// it returns an error. If redactions are needed, it modifies the response content.
func OutputMiddleware(engine *Engine) llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return resp, err
			}
			if resp == nil {
				return resp, err
			}
			result, err := engine.CheckOutput(ctx, resp.Content)
			if err != nil {
				return nil, err
			}
			if !result.Allowed {
				return nil, &blockedError{reason: "output blocked by moderation policy"}
			}
			// Apply redactions to output
			if result.Filtered != result.Original {
				resp.Content = result.Filtered
			}
			return resp, nil
		}
	}
}
