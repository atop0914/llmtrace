// Package semcache provides semantic caching for LLM responses.
//
// Unlike exact-match caching (which hashes request content), semantic caching
// uses embedding similarity to find cached responses for semantically equivalent
// queries. For example, "What is Go?" and "Tell me about the Go language" would
// match even though the exact text differs.
//
// The cache is pluggable — any embedding provider can be used via the Embedder
// interface. A built-in Bag-of-Words embedder is provided for zero-dependency
// use cases; production users should plug in real embedding models (OpenAI,
// Cohere, local models, etc.).
//
// Usage:
//
//	embedder := semcache.NewBowEmbedder()
//	cache := semcache.New(semcache.Config{
//	    Embedder:  embedder,
//	    Threshold: 0.85,
//	    MaxEntries: 1000,
//	    TTL:        10 * time.Minute,
//	})
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(semcache.WithSemanticCache(cache)),
//	)
package semcache

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Embedder computes a vector embedding for a text string.
// Implementations can wrap any embedding API (OpenAI, Cohere, local models).
type Embedder interface {
	// Embed returns a dense float64 vector for the given text.
	// The dimension must be consistent across calls.
	Embed(text string) ([]float64, error)

	// Dimension returns the embedding vector dimension.
	Dimension() int
}

// BowEmbedder is a simple Bag-of-Words embedder that requires no external APIs.
// It tokenizes text into lowercase words and produces a fixed-dimension TF vector
// using a hashing trick. Suitable for demos and testing; for production use,
// plug in a real embedding model.
type BowEmbedder struct {
	dim int
}

// NewBowEmbedder creates a Bag-of-Words embedder with the given dimension.
// Higher dimensions reduce hash collisions. 256 is a reasonable default.
func NewBowEmbedder(dim int) *BowEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &BowEmbedder{dim: dim}
}

func (b *BowEmbedder) Dimension() int { return b.dim }

func (b *BowEmbedder) Embed(text string) ([]float64, error) {
	vec := make([]float64, b.dim)
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return vec, nil
	}
	for _, tok := range tokens {
		h := fnvHash(tok)
		idx := int(h % uint64(b.dim))
		if idx < 0 {
			idx = -idx
		}
		vec[idx]++
	}
	// L2 normalize
	normalize(vec)
	return vec, nil
}

// tokenize splits text into lowercase word tokens.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	// Split on non-alphanumeric
	var tokens []string
	var buf strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf.WriteRune(r)
		} else if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

// fnvHash computes a simple FNV-1a hash for a string.
func fnvHash(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// normalize applies L2 normalization in-place.
func normalize(vec []float64) {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
}

// Config configures the semantic cache.
type Config struct {
	// Embedder computes embeddings for requests. Required.
	Embedder Embedder

	// Threshold is the minimum cosine similarity for a cache hit (0.0-1.0).
	// Default: 0.90 (very similar). Lower values allow more fuzzy matches.
	Threshold float64

	// MaxEntries is the maximum number of cached entries. 0 = no limit.
	MaxEntries int

	// TTL is the time-to-live for cached entries. 0 = never expire.
	TTL time.Duration

	// OnHit is called when a semantic cache hit occurs. Optional.
	OnHit func(query string, similarity float64, cachedResponse any)

	// OnMiss is called when no similar entry is found. Optional.
	OnMiss func(query string, bestSimilarity float64)

	// ExtractQuery extracts the query text from a request-like object.
	// Defaults to concatenating all message contents.
	ExtractQuery func(req any) string
}

// entry holds a cached embedding + response.
type entry struct {
	embedding []float64
	response  any // stored as any to allow generic use
	createdAt time.Time
	query     string // original query text for debugging
}

// CacheStats holds semantic cache performance statistics.
type CacheStats struct {
	Hits     int64
	Misses   int64
	Size     int
	AvgScore float64 // rolling average similarity of hits
}

// HitRate returns the cache hit rate as a percentage (0.0-1.0).
func (s CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Cache is a thread-safe semantic cache that stores embeddings alongside
// cached responses and uses cosine similarity for retrieval.
type Cache struct {
	mu         sync.RWMutex
	config     Config
	entries    []entry     // ordered by insertion for LRU
	vectors    [][]float64 // parallel array of embeddings for fast search
	hits       int64
	misses     int64
	totalScore float64 // sum of hit similarities
}

// New creates a new semantic cache with the given configuration.
func New(cfg Config) *Cache {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.90
	}
	if cfg.Threshold > 1.0 {
		cfg.Threshold = 1.0
	}
	if cfg.Embedder == nil {
		panic("semcache: Config.Embedder is required")
	}
	return &Cache{
		config: cfg,
	}
}

// Get retrieves a cached response for the given query text.
// Returns the response, similarity score, and true if a match above the
// threshold is found. Returns nil, 0, false on miss.
func (c *Cache) Get(query string) (any, float64, bool) {
	vec, err := c.config.Embedder.Embed(query)
	if err != nil {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, 0, false
	}

	c.mu.RLock()
	// Search for best match
	bestIdx := -1
	bestSim := 0.0
	now := time.Now()

	for i, e := range c.entries {
		// Check TTL
		if c.config.TTL > 0 && now.Sub(e.createdAt) > c.config.TTL {
			continue
		}
		sim := cosineSimilarity(vec, c.vectors[i])
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	// Save response while still holding read lock
	var savedResponse any
	var savedQuery string
	if bestIdx >= 0 && bestSim >= c.config.Threshold {
		savedResponse = c.entries[bestIdx].response
		savedQuery = c.entries[bestIdx].query
	}
	c.mu.RUnlock()

	if bestIdx >= 0 && bestSim >= c.config.Threshold {
		c.mu.Lock()
		// Re-validate: find entry by query (index may have shifted)
		validIdx := -1
		for i, e := range c.entries {
			if e.query == savedQuery {
				validIdx = i
				break
			}
		}
		if validIdx >= 0 {
			c.hits++
			c.totalScore += bestSim
			// Move to end for LRU
			e := c.entries[validIdx]
			v := c.vectors[validIdx]
			c.entries = append(c.entries[:validIdx], c.entries[validIdx+1:]...)
			c.vectors = append(c.vectors[:validIdx], c.vectors[validIdx+1:]...)
			c.entries = append(c.entries, e)
			c.vectors = append(c.vectors, v)
		}
		c.mu.Unlock()

		if validIdx >= 0 {
			if c.config.OnHit != nil {
				c.config.OnHit(query, bestSim, savedResponse)
			}
			return savedResponse, bestSim, true
		}
		// Entry was removed between RUnlock and Lock — treat as miss
	}

	c.mu.Lock()
	c.misses++
	c.mu.Unlock()

	if c.config.OnMiss != nil {
		c.config.OnMiss(query, bestSim)
	}
	return nil, bestSim, false
}

// Set stores a response in the semantic cache, associated with the given query text.
func (c *Cache) Set(query string, response any) {
	vec, err := c.config.Embedder.Embed(query)
	if err != nil {
		return // silently skip on embed error
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if c.config.MaxEntries > 0 && len(c.entries) >= c.config.MaxEntries {
		c.entries = c.entries[1:]
		c.vectors = c.vectors[1:]
	}

	c.entries = append(c.entries, entry{
		embedding: vec,
		response:  response,
		createdAt: time.Now(),
		query:     query,
	})
	c.vectors = append(c.vectors, vec)
}

// Delete removes all entries whose query matches the given text exactly.
func (c *Cache) Delete(query string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var kept []entry
	var keptVecs [][]float64
	for i, e := range c.entries {
		if e.query != query {
			kept = append(kept, e)
			keptVecs = append(keptVecs, c.vectors[i])
		}
	}
	c.entries = kept
	c.vectors = keptVecs
}

// Clear removes all entries and resets statistics.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
	c.vectors = nil
	c.hits = 0
	c.misses = 0
	c.totalScore = 0
}

// Len returns the number of entries in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stats returns cache performance statistics.
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := CacheStats{
		Hits:   c.hits,
		Misses: c.misses,
		Size:   len(c.entries),
	}
	if c.hits > 0 {
		s.AvgScore = c.totalScore / float64(c.hits)
	}
	return s
}

// Purge removes expired entries. Call periodically to free memory.
func (c *Cache) Purge() int {
	if c.config.TTL <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var kept []entry
	var keptVecs [][]float64
	removed := 0
	for i, e := range c.entries {
		if now.Sub(e.createdAt) > c.config.TTL {
			removed++
		} else {
			kept = append(kept, e)
			keptVecs = append(keptVecs, c.vectors[i])
		}
	}
	c.entries = kept
	c.vectors = keptVecs
	return removed
}

// TopK returns the top K most similar cached entries for a query.
// Useful for debugging and analytics.
func (c *Cache) TopK(query string, k int) []Match {
	vec, err := c.config.Embedder.Embed(query)
	if err != nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	type scored struct {
		index int
		sim   float64
	}
	var scored_entries []scored
	for i, e := range c.entries {
		if c.config.TTL > 0 && time.Since(e.createdAt) > c.config.TTL {
			continue
		}
		sim := cosineSimilarity(vec, c.vectors[i])
		scored_entries = append(scored_entries, scored{index: i, sim: sim})
	}

	sort.Slice(scored_entries, func(i, j int) bool {
		return scored_entries[i].sim > scored_entries[j].sim
	})

	if k > len(scored_entries) {
		k = len(scored_entries)
	}

	result := make([]Match, k)
	for i := 0; i < k; i++ {
		e := c.entries[scored_entries[i].index]
		result[i] = Match{
			Query:      e.query,
			Similarity: scored_entries[i].sim,
			Response:   e.response,
			CreatedAt:  e.createdAt,
		}
	}
	return result
}

// Match represents a semantic cache match returned by TopK.
type Match struct {
	Query      string
	Similarity float64
	Response   any
	CreatedAt  time.Time
}

// cosineSimilarity computes the cosine similarity between two L2-normalized vectors.
// Both vectors must have the same length.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	// Vectors are L2-normalized, so dot product is cosine similarity
	return dot
}
