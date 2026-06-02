package llmtrace

import (
	"context"
)

// ChatOptions configures a single Chat/ChatStream call.
type ChatOptions struct {
	// Retry configures retry behavior. nil means no retries.
	Retry *RetryConfig

	// Middlewares to apply to this specific call.
	Middlewares []Middleware
}

// ChatOption configures a Chat call.
type ChatOption func(*ChatOptions)

// WithCallRetry enables retry with the given config for this call.
func WithCallRetry(cfg RetryConfig) ChatOption {
	return func(o *ChatOptions) {
		o.Retry = &cfg
	}
}

// WithCallMiddleware adds a middleware for this specific call.
func WithCallMiddleware(mw Middleware) ChatOption {
	return func(o *ChatOptions) {
		o.Middlewares = append(o.Middlewares, mw)
	}
}

// Chat is a convenience method that combines tracing, provider execution,
// retry, and middleware in a single call.
//
// Usage:
//
//	provider := openai.New(openai.WithAPIKey("sk-..."))
//	tracer := llmtrace.NewTracer("my-service")
//	resp, err := tracer.Chat(ctx, &llmtrace.Request{
//	    Model:    "gpt-4o",
//	    Messages: []llmtrace.Message{{Role: "user", Content: "Hello!"}},
//	}, provider)
func (t *Tracer) Chat(ctx context.Context, req *Request, p Provider, opts ...ChatOption) (*Response, error) {
	cfg := &ChatOptions{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build the function chain: provider.Complete -> middlewares -> tracer
	fn := p.Complete
	if len(cfg.Middlewares) > 0 {
		chain := Chain(cfg.Middlewares...)
		fn = chain(fn)
	}

	// Wrap with retry if configured
	if cfg.Retry != nil {
		origFn := fn
		fn = func(ctx context.Context, req *Request) (*Response, error) {
			return WithRetryResult(ctx, *cfg.Retry, func(ctx context.Context) (*Response, error) {
				return origFn(ctx, req)
			})
		}
	}

	return t.Complete(ctx, req, fn)
}

// ChatStream is the streaming variant of Chat.
func (t *Tracer) ChatStream(ctx context.Context, req *Request, p Provider, opts ...ChatOption) (<-chan StreamChunk, error) {
	cfg := &ChatOptions{}
	for _, opt := range opts {
		opt(cfg)
	}

	fn := p.Stream

	return t.Stream(ctx, req, fn)
}
