package llmtrace

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultCacheKey(t *testing.T) {
	temp := 0.7
	maxTok := 100

	req1 := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Hello"}},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	req2 := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Hello"}},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	// Same request should produce same key
	key1 := DefaultCacheKey(req1)
	key2 := DefaultCacheKey(req2)
	if key1 != key2 {
		t.Errorf("same request produced different keys: %s vs %s", key1, key2)
	}

	// Different content should produce different key
	req3 := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Different"}},
		Temperature: &temp,
	}
	key3 := DefaultCacheKey(req3)
	if key1 == key3 {
		t.Error("different requests produced same key")
	}

	// Different model should produce different key
	req4 := &Request{
		Model:       "claude-3-opus",
		Messages:    []Message{{Role: RoleUser, Content: "Hello"}},
		Temperature: &temp,
	}
	key4 := DefaultCacheKey(req4)
	if key1 == key4 {
		t.Error("different models produced same key")
	}
}

func TestDefaultCacheKey_StopOrder(t *testing.T) {
	// Stop sequences should be order-independent
	req1 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "test"}},
		Stop:     []string{"stop1", "stop2"},
	}
	req2 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "test"}},
		Stop:     []string{"stop2", "stop1"},
	}
	key1 := DefaultCacheKey(req1)
	key2 := DefaultCacheKey(req2)
	if key1 != key2 {
		t.Errorf("stop order should not matter: %s vs %s", key1, key2)
	}
}

func TestDefaultCacheKey_NilPointers(t *testing.T) {
	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
		// Temperature, TopP, MaxTokens all nil
	}
	key := DefaultCacheKey(req)
	if key == "" {
		t.Error("key should not be empty")
	}
}

func TestResponseCache_SetGet(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	resp := &Response{
		ID:      "resp-1",
		Content: "Hi there!",
		Model:   "gpt-4o",
		Usage:   Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
	}

	// Miss
	if _, ok := cache.Get(req); ok {
		t.Error("expected cache miss")
	}

	// Set
	cache.Set(req, resp)

	// Hit
	got, ok := cache.Get(req)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Content != resp.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, resp.Content)
	}
	if got.ID != resp.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, resp.ID)
	}
}

func TestResponseCache_TTL(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		TTL: 50 * time.Millisecond,
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	resp := &Response{Content: "Hi!"}

	cache.Set(req, resp)

	// Should hit immediately
	if _, ok := cache.Get(req); !ok {
		t.Error("expected cache hit before TTL")
	}

	// Wait for TTL
	time.Sleep(60 * time.Millisecond)

	// Should miss after TTL
	if _, ok := cache.Get(req); ok {
		t.Error("expected cache miss after TTL")
	}
}

func TestResponseCache_MaxEntries(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		MaxEntries: 2,
	})

	for i := 0; i < 3; i++ {
		req := &Request{
			Model:    "gpt-4o",
			Messages: []Message{{Role: RoleUser, Content: fmt.Sprintf("msg-%d", i)}},
		}
		resp := &Response{Content: fmt.Sprintf("resp-%d", i)}
		cache.Set(req, resp)
	}

	if cache.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", cache.Len())
	}

	// First entry should be evicted
	req0 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "msg-0"}},
	}
	if _, ok := cache.Get(req0); ok {
		t.Error("expected first entry to be evicted")
	}

	// Last two should still be there
	req2 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "msg-2"}},
	}
	if _, ok := cache.Get(req2); !ok {
		t.Error("expected last entry to be present")
	}
}

func TestResponseCache_LRUEviction(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		MaxEntries: 2,
	})

	req0 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "msg-0"}},
	}
	req1 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "msg-1"}},
	}
	req2 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "msg-2"}},
	}

	cache.Set(req0, &Response{Content: "resp-0"})
	cache.Set(req1, &Response{Content: "resp-1"})

	// Access req0 to make it recently used
	cache.Get(req0)

	// Add req2, should evict req1 (least recently used)
	cache.Set(req2, &Response{Content: "resp-2"})

	if _, ok := cache.Get(req0); !ok {
		t.Error("req0 should still be present (was recently accessed)")
	}
	if _, ok := cache.Get(req1); ok {
		t.Error("req1 should have been evicted (least recently used)")
	}
	if _, ok := cache.Get(req2); !ok {
		t.Error("req2 should be present (just added)")
	}
}

func TestResponseCache_Clear(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "Hi!"})

	if cache.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", cache.Len())
	}

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected 0 entries after clear, got %d", cache.Len())
	}

	stats := cache.Stats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("stats should be reset after clear: %+v", stats)
	}
}

func TestResponseCache_Delete(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "Hi!"})

	cache.Delete(req)

	if _, ok := cache.Get(req); ok {
		t.Error("expected cache miss after delete")
	}
}

func TestResponseCache_Stats(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// 2 misses
	cache.Get(req)
	cache.Get(req)

	// Set and 2 hits
	cache.Set(req, &Response{Content: "Hi!"})
	cache.Get(req)
	cache.Get(req)

	stats := cache.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.Size != 1 {
		t.Errorf("expected size 1, got %d", stats.Size)
	}

	if rate := stats.HitRate(); rate != 0.5 {
		t.Errorf("expected hit rate 0.5, got %f", rate)
	}
}

func TestCacheStats_HitRate_ZeroTotal(t *testing.T) {
	stats := CacheStats{}
	if rate := stats.HitRate(); rate != 0 {
		t.Errorf("expected 0 hit rate for empty stats, got %f", rate)
	}
}

func TestResponseCache_CustomKeyFunc(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		KeyFunc: func(req *Request) string {
			return "custom:" + req.Model
		},
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "Hi!"})

	// Different messages but same model should hit
	req2 := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Different"}},
	}
	if _, ok := cache.Get(req2); !ok {
		t.Error("expected cache hit with custom key func")
	}
}

func TestResponseCache_Concurrent(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		MaxEntries: 100,
		TTL:        time.Second,
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &Request{
				Model:    "gpt-4o",
				Messages: []Message{{Role: RoleUser, Content: fmt.Sprintf("msg-%d", id%10)}},
			}
			resp := &Response{Content: fmt.Sprintf("resp-%d", id)}

			cache.Set(req, resp)
			cache.Get(req)
			cache.Len()
			cache.Stats()
		}(i)
	}
	wg.Wait()
}

func TestWithCache(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})
	var callCount int

	provider := func(ctx context.Context, req *Request) (*Response, error) {
		callCount++
		return &Response{
			Content: fmt.Sprintf("response-%d", callCount),
			Model:   req.Model,
			Usage:   Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		}, nil
	}

	mw := WithCache(cache)
	fn := mw(provider)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// First call - miss
	resp1, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second call - hit
	resp2, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 call (cached), got %d", callCount)
	}
	if resp2.Content != resp1.Content {
		t.Errorf("cached response mismatch: %q vs %q", resp2.Content, resp1.Content)
	}
}

func TestWithCache_ErrorNotCached(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})
	var callCount int

	provider := func(ctx context.Context, req *Request) (*Response, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("api error")
		}
		return &Response{Content: "ok"}, nil
	}

	mw := WithCache(cache)
	fn := mw(provider)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// First call - error (should not cache)
	_, err := fn(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}

	// Second call - should call provider again
	resp, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (error not cached), got %d", callCount)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}
}

func TestWithCache_Callbacks(t *testing.T) {
	var hits, misses int

	cache := NewResponseCache(CacheConfig{
		OnHit: func(req *Request, resp *Response) {
			hits++
		},
		OnMiss: func(req *Request) {
			misses++
		},
	})

	provider := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "ok"}, nil
	}

	mw := WithCache(cache)
	fn := mw(provider)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	fn(context.Background(), req) // miss
	fn(context.Background(), req) // hit
	fn(context.Background(), req) // hit

	if misses != 1 {
		t.Errorf("expected 1 miss, got %d", misses)
	}
	if hits != 2 {
		t.Errorf("expected 2 hits, got %d", hits)
	}
}

func TestWithCache_NilResponse(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	provider := func(ctx context.Context, req *Request) (*Response, error) {
		return nil, nil // nil response, no error
	}

	mw := WithCache(cache)
	fn := mw(provider)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	fn(context.Background(), req)

	// Should not cache nil response
	if cache.Len() != 0 {
		t.Error("nil response should not be cached")
	}
}

func TestWithStreamCache(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})
	var callCount int32

	streamFn := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		atomic.AddInt32(&callCount, 1)
		ch := make(chan StreamChunk, 3)
		ch <- StreamChunk{Content: "Hello "}
		ch <- StreamChunk{Content: "World"}
		ch <- StreamChunk{Usage: &Usage{InputTokens: 5, OutputTokens: 5, TotalTokens: 10}}
		close(ch)
		return ch, nil
	}

	mw := WithStreamCache(cache)
	fn := mw(streamFn)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// First call - miss, collect stream
	stream1, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var content1 string
	for chunk := range stream1 {
		content1 += chunk.Content
	}
	if content1 != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", content1)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second call - hit, returns cached
	stream2, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var content2 string
	var usage *Usage
	for chunk := range stream2 {
		content2 += chunk.Content
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if content2 != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", content2)
	}
	if usage == nil || usage.TotalTokens != 10 {
		t.Error("expected usage in cached stream response")
	}

	// Provider should only be called once
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected still 1 call (cached), got %d", callCount)
	}
}

func TestWithStreamCache_ErrorNotCached(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})
	var callCount int32

	streamFn := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		count := atomic.AddInt32(&callCount, 1)
		ch := make(chan StreamChunk, 2)
		if count == 1 {
			ch <- StreamChunk{Error: fmt.Errorf("stream error")}
		} else {
			ch <- StreamChunk{Content: "ok"}
		}
		close(ch)
		return ch, nil
	}

	mw := WithStreamCache(cache)
	fn := mw(streamFn)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// First call - error in stream
	stream1, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for range stream1 {
		// consume
	}

	// Second call - should call provider again
	stream2, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range stream2 {
		if chunk.Content != "ok" {
			t.Errorf("expected 'ok', got %q", chunk.Content)
		}
	}

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestWithStreamCache_Callbacks(t *testing.T) {
	var hits, misses int

	cache := NewResponseCache(CacheConfig{
		OnHit: func(req *Request, resp *Response) {
			hits++
		},
		OnMiss: func(req *Request) {
			misses++
		},
	})

	streamFn := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		ch <- StreamChunk{Content: "data"}
		close(ch)
		return ch, nil
	}

	mw := WithStreamCache(cache)
	fn := mw(streamFn)

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	// Miss
	stream1, _ := fn(context.Background(), req)
	for range stream1 {
	}

	// Hit
	stream2, _ := fn(context.Background(), req)
	for range stream2 {
	}

	if misses != 1 {
		t.Errorf("expected 1 miss, got %d", misses)
	}
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}
}

func TestResponseCache_UpdateExisting(t *testing.T) {
	cache := NewResponseCache(CacheConfig{})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}

	cache.Set(req, &Response{Content: "v1"})
	cache.Set(req, &Response{Content: "v2"})

	got, ok := cache.Get(req)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Content != "v2" {
		t.Errorf("expected 'v2', got %q", got.Content)
	}
	if cache.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.Len())
	}
}

func TestResponseCache_NoTTL(t *testing.T) {
	cache := NewResponseCache(CacheConfig{
		// TTL = 0 means no expiration
	})

	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "forever"})

	// Should still be there (no TTL)
	if _, ok := cache.Get(req); !ok {
		t.Error("expected cache hit with no TTL")
	}
}

func BenchmarkDefaultCacheKey(b *testing.B) {
	req := &Request{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: RoleUser, Content: "Hello, how are you today?"}},
		Temperature: Float64Ptr(0.7),
		MaxTokens:   IntPtr(100),
		Stop:        []string{"END", "STOP"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DefaultCacheKey(req)
	}
}

func BenchmarkResponseCache_Get(b *testing.B) {
	cache := NewResponseCache(CacheConfig{MaxEntries: 1000})
	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "Hi!"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(req)
	}
}

func BenchmarkResponseCache_Set(b *testing.B) {
	cache := NewResponseCache(CacheConfig{MaxEntries: 10000})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := &Request{
			Model:    "gpt-4o",
			Messages: []Message{{Role: RoleUser, Content: fmt.Sprintf("msg-%d", i)}},
		}
		cache.Set(req, &Response{Content: "ok"})
	}
}

func BenchmarkWithCache_Hit(b *testing.B) {
	cache := NewResponseCache(CacheConfig{})
	req := &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	cache.Set(req, &Response{Content: "Hi!"})

	provider := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "Hi!"}, nil
	}
	fn := WithCache(cache)(provider)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(ctx, req)
	}
}
