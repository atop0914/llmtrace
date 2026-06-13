package llmtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheConfig configures the response cache behavior.
type CacheConfig struct {
	// MaxEntries is the maximum number of cached responses. 0 means no limit.
	MaxEntries int

	// TTL is the time-to-live for cached entries. 0 means entries never expire.
	TTL time.Duration

	// KeyFunc allows custom cache key generation. If nil, DefaultCacheKey is used.
	KeyFunc func(req *Request) string

	// OnHit is called when a cache hit occurs. Optional.
	OnHit func(req *Request, resp *Response)

	// OnMiss is called when a cache miss occurs. Optional.
	OnMiss func(req *Request)
}

// cacheEntry holds a cached response with metadata.
type cacheEntry struct {
	response  *Response
	createdAt time.Time
}

// ResponseCache is a thread-safe LRU cache for LLM responses.
// It stores responses keyed by request content hash, with optional TTL and size limits.
//
// A ResponseCache is safe for concurrent use by multiple goroutines.
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	order   []string // LRU order: most recently used at the end
	config  CacheConfig
	hits    int64
	misses  int64
}

// NewResponseCache creates a new response cache with the given configuration.
//
// Example:
//
//	cache := llmtrace.NewResponseCache(llmtrace.CacheConfig{
//	    MaxEntries: 1000,
//	    TTL:        5 * time.Minute,
//	})
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithCache(cache)),
//	)
func NewResponseCache(cfg CacheConfig) *ResponseCache {
	return &ResponseCache{
		entries: make(map[string]*cacheEntry),
		config:  cfg,
	}
}

// Get retrieves a cached response for the given request.
// Returns the response and true if found and not expired, nil and false otherwise.
func (c *ResponseCache) Get(req *Request) (*Response, bool) {
	key := c.keyFor(req)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}

	// Check TTL expiration
	if c.config.TTL > 0 && time.Since(entry.createdAt) > c.config.TTL {
		delete(c.entries, key)
		c.removeFromOrder(key)
		c.misses++
		return nil, false
	}

	// Move to end (most recently used)
	c.moveToEnd(key)
	c.hits++

	return entry.response, true
}

// Set stores a response in the cache for the given request.
func (c *ResponseCache) Set(req *Request, resp *Response) {
	key := c.keyFor(req)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if c.config.MaxEntries > 0 && len(c.entries) >= c.config.MaxEntries {
		if _, exists := c.entries[key]; !exists {
			c.evictOldest()
		}
	}

	c.entries[key] = &cacheEntry{
		response:  resp,
		createdAt: time.Now(),
	}
	c.moveToEnd(key)
}

// Delete removes a specific entry from the cache.
func (c *ResponseCache) Delete(req *Request) {
	key := c.keyFor(req)

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	c.removeFromOrder(key)
}

// Clear removes all entries from the cache.
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.order = nil
	c.hits = 0
	c.misses = 0
}

// Len returns the number of entries in the cache.
func (c *ResponseCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stats returns cache hit/miss statistics.
func (c *ResponseCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{
		Hits:   c.hits,
		Misses: c.misses,
		Size:   len(c.entries),
	}
}

// CacheStats holds cache performance statistics.
type CacheStats struct {
	Hits   int64
	Misses int64
	Size   int
}

// HitRate returns the cache hit rate as a percentage (0.0-1.0).
func (s CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// keyFor generates a cache key for the request.
func (c *ResponseCache) keyFor(req *Request) string {
	if c.config.KeyFunc != nil {
		return c.config.KeyFunc(req)
	}
	return DefaultCacheKey(req)
}

// evictOldest removes the least recently used entry. Must be called with lock held.
func (c *ResponseCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	delete(c.entries, oldest)
	c.order = c.order[1:]
}

// moveToEnd moves a key to the end of the LRU order list. Must be called with lock held.
func (c *ResponseCache) moveToEnd(key string) {
	c.removeFromOrder(key)
	c.order = append(c.order, key)
}

// removeFromOrder removes a key from the LRU order list. Must be called with lock held.
func (c *ResponseCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// DefaultCacheKey generates a deterministic cache key from the request content.
// It hashes the model, messages, temperature, top_p, max_tokens, and stop sequences.
func DefaultCacheKey(req *Request) string {
	var b strings.Builder

	b.WriteString("model=")
	b.WriteString(req.Model)

	b.WriteString("|messages=")
	for _, m := range req.Messages {
		b.WriteString(string(m.Role))
		b.WriteString(":")
		b.WriteString(m.Content)
		b.WriteString(";")
	}

	if req.Temperature != nil {
		b.WriteString("|temp=")
		fmt.Fprintf(&b, "%.4f", *req.Temperature)
	}

	if req.TopP != nil {
		b.WriteString("|top_p=")
		fmt.Fprintf(&b, "%.4f", *req.TopP)
	}

	if req.MaxTokens != nil {
		b.WriteString("|max_tokens=")
		fmt.Fprintf(&b, "%d", *req.MaxTokens)
	}

	if len(req.Stop) > 0 {
		stops := make([]string, len(req.Stop))
		copy(stops, req.Stop)
		sort.Strings(stops)
		b.WriteString("|stop=")
		b.WriteString(strings.Join(stops, ","))
	}

	hash := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(hash[:])
}

// WithCache returns a Middleware that caches completion responses.
// On cache hit, the stored response is returned without calling the underlying function.
// On cache miss, the request proceeds normally and the response is cached.
//
// Example:
//
//	cache := llmtrace.NewResponseCache(llmtrace.CacheConfig{
//	    MaxEntries: 500,
//	    TTL:        10 * time.Minute,
//	})
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(llmtrace.WithCache(cache)),
//	)
func WithCache(cache *ResponseCache) Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			if resp, ok := cache.Get(req); ok {
				if cache.config.OnHit != nil {
					cache.config.OnHit(req, resp)
				}
				return resp, nil
			}

			if cache.config.OnMiss != nil {
				cache.config.OnMiss(req)
			}

			resp, err := next(ctx, req)
			if err == nil && resp != nil {
				cache.Set(req, resp)
			}
			return resp, err
		}
	}
}

// WithStreamCache returns a StreamMiddleware that caches streaming responses.
// The stream is fully consumed, collected into a single response, cached, and
// then re-emitted as a single-chunk stream to the caller.
//
// Note: This converts a streaming response into a cached non-streaming response.
// The cached response is returned as a single StreamChunk with the full content.
func WithStreamCache(cache *ResponseCache) StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			if resp, ok := cache.Get(req); ok {
				if cache.config.OnHit != nil {
					cache.config.OnHit(req, resp)
				}
				// Return cached response as a single-chunk stream
				ch := make(chan StreamChunk, 1)
				ch <- StreamChunk{
					Content: resp.Content,
					Usage:   &resp.Usage,
				}
				close(ch)
				return ch, nil
			}

			if cache.config.OnMiss != nil {
				cache.config.OnMiss(req)
			}

			// Consume the full stream
			stream, err := next(ctx, req)
			if err != nil {
				return nil, err
			}

			// Collect all chunks in background
			collected := make(chan StreamChunk, 1)
			go func() {
				defer close(collected)
				var content strings.Builder
				var usage *Usage
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
				finalResp := &Response{
					Content: content.String(),
					Model:   req.Model,
				}
				if usage != nil {
					finalResp.Usage = *usage
				}
				cache.Set(req, finalResp)

				// Re-emit as single chunk
				collected <- StreamChunk{
					Content: content.String(),
					Usage:   usage,
				}
			}()

			return collected, nil
		}
	}
}
