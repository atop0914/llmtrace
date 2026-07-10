package semcache

import (
	"context"
	"strings"

	"github.com/atop0914/llmtrace"
)

// extractQueryFromRequest concatenates all message contents from an llmtrace.Request.
func extractQueryFromRequest(req *llmtrace.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

// WithSemanticCache returns an llmtrace.Middleware that applies semantic caching.
//
// On a cache hit (similarity >= threshold), the cached response is returned
// immediately without calling the LLM. On a miss, the request proceeds normally
// and the response is cached for future semantic matches.
//
// Example:
//
//	cache := semcache.New(semcache.Config{
//	    Embedder:   semcache.NewBowEmbedder(256),
//	    Threshold:  0.85,
//	    MaxEntries: 1000,
//	    TTL:        10 * time.Minute,
//	})
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(semcache.WithSemanticCache(cache)),
//	)
func WithSemanticCache(cache *Cache) llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			query := extractQueryFromRequest(req)

			if cachedResp, sim, ok := cache.Get(query); ok {
				if cache.config.OnHit != nil {
					cache.config.OnHit(query, sim, cachedResp)
				}
				return cachedResp.(*llmtrace.Response), nil
			}

			if cache.config.OnMiss != nil {
				cache.config.OnMiss(query, 0)
			}

			resp, err := next(ctx, req)
			if err == nil && resp != nil {
				cache.Set(query, resp)
			}
			return resp, err
		}
	}
}

// WithSemanticStreamCache returns an llmtrace.StreamMiddleware that applies
// semantic caching to streaming responses. The stream is fully consumed,
// cached, and re-emitted as a single-chunk stream on cache miss.
// On cache hit, the cached response is returned as a single chunk.
func WithSemanticStreamCache(cache *Cache) llmtrace.StreamMiddleware {
	return func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			query := extractQueryFromRequest(req)

			if cachedResp, sim, ok := cache.Get(query); ok {
				if cache.config.OnHit != nil {
					cache.config.OnHit(query, sim, cachedResp)
				}
				resp := cachedResp.(*llmtrace.Response)
				ch := make(chan llmtrace.StreamChunk, 1)
				ch <- llmtrace.StreamChunk{
					Content: resp.Content,
					Usage:   &resp.Usage,
				}
				close(ch)
				return ch, nil
			}

			if cache.config.OnMiss != nil {
				cache.config.OnMiss(query, 0)
			}

			// Consume the stream and cache the result
			stream, err := next(ctx, req)
			if err != nil {
				return nil, err
			}

			collected := make(chan llmtrace.StreamChunk, 1)
			go func() {
				defer close(collected)
				var content strings.Builder
				var usage *llmtrace.Usage
				for chunk := range stream {
					if chunk.Error != nil {
						collected <- chunk
						return
					}
					content.WriteString(chunk.Content)
					if chunk.Usage != nil {
						usage = chunk.Usage
					}
				}

				// Cache the collected response
				finalResp := &llmtrace.Response{
					Content: content.String(),
					Model:   req.Model,
				}
				if usage != nil {
					finalResp.Usage = *usage
				}
				cache.Set(query, finalResp)

				// Re-emit as single chunk
				collected <- llmtrace.StreamChunk{
					Content: content.String(),
					Usage:   usage,
				}
			}()

			return collected, nil
		}
	}
}
