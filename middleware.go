package llmtrace

import (
	"context"
	"time"
)

// Middleware wraps a CompleteFunc with additional behavior.
// Middlewares can add logging, rate limiting, caching, etc.
type Middleware func(next CompleteFunc) CompleteFunc

// StreamMiddleware wraps a StreamFunc with additional behavior.
type StreamMiddleware func(next StreamFunc) StreamFunc

// Chain applies middlewares in order (first middleware is outermost).
func Chain(middlewares ...Middleware) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// ChainStream applies stream middlewares in order.
func ChainStream(middlewares ...StreamMiddleware) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// CompleteHook is called after each completion with the request, response, and error.
type CompleteHook func(ctx context.Context, req *Request, resp *Response, err error)

// WithCompleteHook returns a middleware that calls hook after each request.
func WithCompleteHook(hook CompleteHook) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			resp, err := next(ctx, req)
			hook(ctx, req, resp, err)
			return resp, err
		}
	}
}

// WithTiming returns a middleware that calls callback with the duration of each call.
func WithTiming(callback func(req *Request, durationMS float64)) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			callback(req, float64(time.Since(start).Milliseconds()))
			return resp, err
		}
	}
}
