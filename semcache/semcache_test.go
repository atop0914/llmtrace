package semcache

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// --- BowEmbedder tests ---

func TestBowEmbedder_Dimension(t *testing.T) {
	tests := []struct {
		dim  int
		want int
	}{
		{256, 256},
		{128, 128},
		{0, 256},  // default
		{-1, 256}, // default
	}
	for _, tt := range tests {
		e := NewBowEmbedder(tt.dim)
		if got := e.Dimension(); got != tt.want {
			t.Errorf("NewBowEmbedder(%d).Dimension() = %d, want %d", tt.dim, got, tt.want)
		}
	}
}

func TestBowEmbedder_Embed(t *testing.T) {
	e := NewBowEmbedder(64)

	vec, err := e.Embed("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 64 {
		t.Fatalf("expected 64 dimensions, got %d", len(vec))
	}

	// Should be L2-normalized
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if math.Abs(norm-1.0) > 1e-9 {
		t.Errorf("expected L2 norm ≈ 1.0, got %f", norm)
	}
}

func TestBowEmbedder_EmbedEmpty(t *testing.T) {
	e := NewBowEmbedder(64)
	vec, err := e.Embed("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, v := range vec {
		if v != 0 {
			t.Errorf("expected all zeros for empty input, got vec[%d]=%f", i, v)
			break
		}
	}
}

func TestBowEmbedder_Deterministic(t *testing.T) {
	e := NewBowEmbedder(128)
	text := "what is the capital of France"

	v1, _ := e.Embed(text)
	v2, _ := e.Embed(text)

	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("non-deterministic: v1[%d]=%f, v2[%d]=%f", i, v1[i], i, v2[i])
		}
	}
}

func TestBowEmbedder_SimilarTextsHighSimilarity(t *testing.T) {
	e := NewBowEmbedder(256)

	v1, _ := e.Embed("what is the Go programming language")
	v2, _ := e.Embed("tell me about the Go programming language")
	v3, _ := e.Embed("how to make chocolate cake")

	sim12 := cosineSimilarity(v1, v2)
	sim13 := cosineSimilarity(v1, v3)

	t.Logf("Similar queries: %.4f", sim12)
	t.Logf("Different queries: %.4f", sim13)

	if sim12 <= sim13 {
		t.Errorf("expected similar queries to have higher similarity: sim12=%.4f, sim13=%.4f", sim12, sim13)
	}
}

// --- Tokenize tests ---

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello world", 2},
		{"Hello, World!", 2},
		{"one-two-three", 3}, // hyphens are separators: "one", "two", "three"
		{"", 0},
		{"   ", 0},
		{"123 abc 456", 3},
	}
	for _, tt := range tests {
		tokens := tokenize(tt.input)
		if len(tokens) != tt.want {
			t.Errorf("tokenize(%q) = %d tokens, want %d: %v", tt.input, len(tokens), tt.want, tokens)
		}
	}
}

func TestTokenize_Lowercase(t *testing.T) {
	tokens := tokenize("Hello WORLD Go")
	for _, tok := range tokens {
		for _, r := range tok {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("expected lowercase, got %q in %q", string(r), tok)
			}
		}
	}
}

// --- CosineSimilarity tests ---

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical", []float64{1, 0, 0}, []float64{1, 0, 0}, 1.0},
		{"orthogonal", []float64{1, 0, 0}, []float64{0, 1, 0}, 0.0},
		{"opposite", []float64{1, 0}, []float64{-1, 0}, -1.0},
		{"empty", nil, nil, 0},
		{"diff lengths", []float64{1, 0}, []float64{1, 0, 0}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cosineSimilarity = %f, want %f", got, tt.want)
			}
		})
	}
}

// --- Cache tests ---

func newTestCache(threshold float64, maxEntries int) *Cache {
	return New(Config{
		Embedder:   NewBowEmbedder(128),
		Threshold:  threshold,
		MaxEntries: maxEntries,
	})
}

func TestCache_BasicGetSet(t *testing.T) {
	cache := newTestCache(0.5, 100)

	// Miss
	resp, sim, ok := cache.Get("hello world")
	if ok {
		t.Errorf("expected miss, got hit with sim=%.4f resp=%v", sim, resp)
	}

	// Set
	cache.Set("hello world", "cached response")

	// Hit
	resp, sim, ok = cache.Get("hello world")
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if resp.(string) != "cached response" {
		t.Errorf("expected 'cached response', got %v", resp)
	}
	if sim < 0.99 {
		t.Errorf("exact match should have similarity ≈ 1.0, got %.4f", sim)
	}
}

func TestCache_SemanticMatch(t *testing.T) {
	// Lower threshold to allow semantic matches
	cache := newTestCache(0.3, 100)

	cache.Set("what is the Go programming language", "Go is a statically typed language")

	// Semantically similar query
	resp, sim, ok := cache.Get("tell me about Go language")
	if !ok {
		t.Fatalf("expected semantic hit (sim=%.4f)", sim)
	}
	t.Logf("Semantic match similarity: %.4f", sim)
	if resp.(string) != "Go is a statically typed language" {
		t.Errorf("unexpected response: %v", resp)
	}
}

func TestCache_ThresholdEnforcement(t *testing.T) {
	cache := newTestCache(0.99, 100) // very high threshold

	cache.Set("the quick brown fox", "jumped")

	// Different query should not match at very high threshold
	resp, sim, ok := cache.Get("completely unrelated cooking recipe")
	if ok {
		t.Errorf("expected miss at threshold 0.99, got hit with sim=%.4f resp=%v", sim, resp)
	}
	_ = resp
}

func TestCache_TTL(t *testing.T) {
	cache := New(Config{
		Embedder:   NewBowEmbedder(64),
		Threshold:  0.5,
		MaxEntries: 100,
		TTL:        50 * time.Millisecond,
	})

	cache.Set("hello", "world")

	// Immediate hit
	_, _, ok := cache.Get("hello")
	if !ok {
		t.Fatal("expected hit before TTL")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	_, _, ok = cache.Get("hello")
	if ok {
		t.Error("expected miss after TTL expiry")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	cache := newTestCache(0.5, 3) // max 3 entries

	cache.Set("a", "1")
	cache.Set("b", "2")
	cache.Set("c", "3")

	if cache.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", cache.Len())
	}

	// Adding 4th should evict "a"
	cache.Set("d", "4")

	if cache.Len() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", cache.Len())
	}

	// "a" should be evicted
	_, _, ok := cache.Get("a")
	if ok {
		t.Error("expected 'a' to be evicted")
	}

	// "b", "c", "d" should still be there
	for _, q := range []string{"b", "c", "d"} {
		_, _, ok = cache.Get(q)
		if !ok {
			t.Errorf("expected '%s' to still be cached", q)
		}
	}
}

func TestCache_LRUBump(t *testing.T) {
	cache := newTestCache(0.5, 3)

	cache.Set("a", "1")
	cache.Set("b", "2")
	cache.Set("c", "3")

	// Access "a" to bump it to end of LRU
	cache.Get("a")

	// Now "b" should be evicted (oldest unused)
	cache.Set("d", "4")

	_, _, ok := cache.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted (was LRU)")
	}
	_, _, ok = cache.Get("a")
	if !ok {
		t.Error("expected 'a' to survive (was bumped)")
	}
}

func TestCache_MaxEntriesZero(t *testing.T) {
	cache := newTestCache(0.5, 0) // no limit

	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("query-%d", i), i)
	}

	if cache.Len() != 100 {
		t.Errorf("expected 100 entries with no limit, got %d", cache.Len())
	}
}

func TestCache_Stats(t *testing.T) {
	cache := newTestCache(0.5, 100)

	s := cache.Stats()
	if s.Hits != 0 || s.Misses != 0 || s.Size != 0 {
		t.Errorf("empty cache stats: %+v", s)
	}
	if s.HitRate() != 0 {
		t.Errorf("empty cache HitRate should be 0, got %f", s.HitRate())
	}

	cache.Set("q1", "r1")

	// Hit
	cache.Get("q1")
	s = cache.Stats()
	if s.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", s.Hits)
	}
	if s.Size != 1 {
		t.Errorf("expected size 1, got %d", s.Size)
	}

	// Miss
	cache.Get("totally different query xyz")
	s = cache.Stats()
	if s.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", s.Misses)
	}

	if s.HitRate() != 0.5 {
		t.Errorf("expected HitRate 0.5, got %f", s.HitRate())
	}

	if s.AvgScore <= 0 || s.AvgScore > 1 {
		t.Errorf("unexpected AvgScore: %f", s.AvgScore)
	}
}

func TestCache_Clear(t *testing.T) {
	cache := newTestCache(0.5, 100)

	cache.Set("a", "1")
	cache.Get("a") // create a hit

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", cache.Len())
	}
	s := cache.Stats()
	if s.Hits != 0 || s.Misses != 0 {
		t.Errorf("stats not reset: %+v", s)
	}
}

func TestCache_Delete(t *testing.T) {
	cache := newTestCache(0.5, 100)

	cache.Set("hello", "world")
	cache.Delete("hello")

	_, _, ok := cache.Get("hello")
	if ok {
		t.Error("expected miss after delete")
	}
}

func TestCache_Purge(t *testing.T) {
	cache := New(Config{
		Embedder:   NewBowEmbedder(64),
		Threshold:  0.5,
		MaxEntries: 100,
		TTL:        30 * time.Millisecond,
	})

	cache.Set("a", "1")
	cache.Set("b", "2")
	time.Sleep(40 * time.Millisecond)
	cache.Set("c", "3") // still alive

	removed := cache.Purge()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if cache.Len() != 1 {
		t.Errorf("expected 1 remaining, got %d", cache.Len())
	}
}

func TestCache_TopK(t *testing.T) {
	cache := newTestCache(0.0, 100) // accept all similarities

	cache.Set("the quick brown fox", "1")
	cache.Set("a lazy brown dog", "2")
	cache.Set("cooking pasta with tomatoes", "3")

	matches := cache.TopK("brown fox", 2)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// First should be most similar
	if matches[0].Similarity < matches[1].Similarity {
		t.Errorf("matches not sorted: [0]=%.4f, [1]=%.4f", matches[0].Similarity, matches[1].Similarity)
	}

	for _, m := range matches {
		t.Logf("TopK: query=%q sim=%.4f", m.Query, m.Similarity)
	}
}

// --- Thread safety ---

func TestCache_ConcurrentAccess(t *testing.T) {
	cache := newTestCache(0.5, 1000)

	var wg sync.WaitGroup
	// 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				query := fmt.Sprintf("writer-%d-query-%d", id, j)
				cache.Set(query, fmt.Sprintf("response-%d-%d", id, j))
			}
		}(i)
	}
	wg.Wait()

	// 10 readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				query := fmt.Sprintf("writer-%d-query-%d", id, j)
				cache.Get(query)
			}
		}(i)
	}
	wg.Wait()

	if cache.Len() != 1000 {
		t.Errorf("expected 1000 entries, got %d", cache.Len())
	}
}

func TestCache_ConcurrentSetGetDelete(t *testing.T) {
	cache := newTestCache(0.5, 500)
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(3)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cache.Set(fmt.Sprintf("q-%d-%d", id, j), j)
			}
		}(i)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cache.Get(fmt.Sprintf("q-%d-%d", id, j))
			}
		}(i)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cache.Delete(fmt.Sprintf("q-%d-%d", id, j))
			}
		}(i)
	}
	wg.Wait()
	// No panic/race = success
}

// --- Benchmarks ---

func BenchmarkBowEmbedder_Embed(b *testing.B) {
	e := NewBowEmbedder(256)
	text := "What is the Go programming language and how does it compare to Rust?"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Embed(text)
	}
}

func BenchmarkCache_Get_Hit(b *testing.B) {
	cache := newTestCache(0.5, 10000)
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("query number %d about Go programming", i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(fmt.Sprintf("query number %d about Go programming", i%1000))
	}
}

func BenchmarkCache_Get_Miss(b *testing.B) {
	cache := newTestCache(0.99, 1000)
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("query %d", i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("completely different unique query that will not match anything")
	}
}

func BenchmarkCache_Set(b *testing.B) {
	cache := newTestCache(0.5, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(fmt.Sprintf("benchmark query %d", i), i)
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	dim := 256
	a := make([]float64, dim)
	bb := make([]float64, dim)
	for i := range a {
		a[i] = float64(i) / float64(dim)
		bb[i] = float64(dim-i) / float64(dim)
	}
	normalize(a)
	normalize(bb)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSimilarity(a, bb)
	}
}
