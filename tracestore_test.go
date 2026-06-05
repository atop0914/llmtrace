package llmtrace

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTraceStore_Add(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 5})

	store.Add(TraceRecord{
		Provider: "openai",
		Model:    "gpt-4o",
		Status:   "success",
	})

	if store.Len() != 1 {
		t.Fatalf("expected Len()=1, got %d", store.Len())
	}
}

func TestTraceStore_Eviction(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 3})

	for i := 0; i < 5; i++ {
		store.Add(TraceRecord{
			Provider: fmt.Sprintf("provider-%d", i),
			Model:    "test",
			Status:   "success",
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	if store.Len() != 3 {
		t.Fatalf("expected Len()=3, got %d", store.Len())
	}

	// Should have the 3 newest records
	all := store.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}

	// Oldest surviving record should be provider-2
	if all[0].Provider != "provider-2" {
		t.Errorf("expected provider-2, got %s", all[0].Provider)
	}
	// Newest should be provider-4
	if all[2].Provider != "provider-4" {
		t.Errorf("expected provider-4, got %s", all[2].Provider)
	}
}

func TestTraceStore_Query(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 100})

	now := time.Now()
	records := []TraceRecord{
		{Provider: "openai", Model: "gpt-4o", Status: "success", StartedAt: now.Add(-3 * time.Hour)},
		{Provider: "openai", Model: "gpt-3.5-turbo", Status: "error", StartedAt: now.Add(-2 * time.Hour)},
		{Provider: "anthropic", Model: "claude-3-opus", Status: "success", StartedAt: now.Add(-1 * time.Hour)},
		{Provider: "openai", Model: "gpt-4o", Status: "success", StartedAt: now},
	}

	for _, r := range records {
		store.Add(r)
	}

	t.Run("by provider", func(t *testing.T) {
		results := store.Query(TraceQuery{Provider: "openai"})
		if len(results) != 3 {
			t.Errorf("expected 3 openai traces, got %d", len(results))
		}
	})

	t.Run("by model", func(t *testing.T) {
		results := store.Query(TraceQuery{Model: "gpt-4o"})
		if len(results) != 2 {
			t.Errorf("expected 2 gpt-4o traces, got %d", len(results))
		}
	})

	t.Run("by status", func(t *testing.T) {
		results := store.Query(TraceQuery{Status: "error"})
		if len(results) != 1 {
			t.Errorf("expected 1 error trace, got %d", len(results))
		}
		if results[0].Provider != "openai" {
			t.Errorf("expected openai, got %s", results[0].Provider)
		}
	})

	t.Run("by time range", func(t *testing.T) {
		results := store.Query(TraceQuery{
			Since: now.Add(-90 * time.Minute),
			Until: now.Add(-30 * time.Minute),
		})
		if len(results) != 1 {
			t.Errorf("expected 1 trace in range, got %d", len(results))
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		results := store.Query(TraceQuery{
			Provider: "openai",
			Status:   "success",
		})
		if len(results) != 2 {
			t.Errorf("expected 2 openai+success traces, got %d", len(results))
		}
	})

	t.Run("limit", func(t *testing.T) {
		results := store.Query(TraceQuery{Limit: 2})
		if len(results) != 2 {
			t.Errorf("expected 2 traces with limit, got %d", len(results))
		}
	})

	t.Run("sort desc", func(t *testing.T) {
		results := store.Query(TraceQuery{SortDesc: true})
		if len(results) < 2 {
			t.Fatal("need at least 2 results")
		}
		if results[0].StartedAt.Before(results[1].StartedAt) {
			t.Errorf("expected descending order")
		}
	})

	t.Run("limit with sort desc", func(t *testing.T) {
		results := store.Query(TraceQuery{SortDesc: true, Limit: 2})
		if len(results) != 2 {
			t.Fatalf("expected 2, got %d", len(results))
		}
		// Should be the 2 newest
		if !results[0].StartedAt.After(results[1].StartedAt) || results[0].StartedAt != now {
			t.Errorf("expected newest first")
		}
	})
}

func TestTraceStore_Clear(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 10})

	for i := 0; i < 5; i++ {
		store.Add(TraceRecord{Provider: "test", Status: "success"})
	}

	store.Clear()

	if store.Len() != 0 {
		t.Errorf("expected Len()=0 after clear, got %d", store.Len())
	}

	all := store.All()
	if len(all) != 0 {
		t.Errorf("expected 0 records after clear, got %d", len(all))
	}
}

func TestTraceStore_TraceSummary(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 100})

	store.Add(TraceRecord{
		Provider: "openai", Model: "gpt-4o", Status: "success",
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
		CostUSD: 0.01, LatencyMS: 500,
	})
	store.Add(TraceRecord{
		Provider: "anthropic", Model: "claude-3-opus", Status: "success",
		InputTokens: 200, OutputTokens: 100, TotalTokens: 300,
		CostUSD: 0.02, LatencyMS: 800,
	})
	store.Add(TraceRecord{
		Provider: "openai", Model: "gpt-4o", Status: "error",
		LatencyMS: 100, Error: "rate limit",
	})

	summary := store.TraceSummary()

	if summary.TotalTraces != 3 {
		t.Errorf("expected 3 traces, got %d", summary.TotalTraces)
	}
	if summary.TotalTokens != 450 {
		t.Errorf("expected 450 tokens, got %d", summary.TotalTokens)
	}
	if summary.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", summary.TotalErrors)
	}
	if summary.Providers["openai"] != 2 {
		t.Errorf("expected 2 openai traces, got %d", summary.Providers["openai"])
	}
	if summary.Providers["anthropic"] != 1 {
		t.Errorf("expected 1 anthropic trace, got %d", summary.Providers["anthropic"])
	}
	if summary.Models["gpt-4o"] != 2 {
		t.Errorf("expected 2 gpt-4o traces, got %d", summary.Models["gpt-4o"])
	}

	// Latency: (500 + 800 + 100) / 3 = 466.67
	expectedAvg := 1400.0 / 3.0
	if diff := summary.AvgLatencyMS - expectedAvg; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected avg latency ~%.2f, got %.2f", expectedAvg, summary.AvgLatencyMS)
	}

	if summary.MinLatencyMS != 100 {
		t.Errorf("expected min latency 100, got %.0f", summary.MinLatencyMS)
	}
	if summary.MaxLatencyMS != 800 {
		t.Errorf("expected max latency 800, got %.0f", summary.MaxLatencyMS)
	}
}

func TestTraceStore_Concurrent(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 1000})

	var wg sync.WaitGroup
	const goroutines = 20
	const opsPerGoroutine = 100

	// Concurrent writers
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				store.Add(TraceRecord{
					Provider: fmt.Sprintf("provider-%d", id%3),
					Model:    "test",
					Status:   "success",
				})
			}
		}(g)
	}

	// Concurrent readers
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				_ = store.Len()
				_ = store.Query(TraceQuery{Provider: "provider-0"})
				_ = store.All()
			}
		}()
	}

	wg.Wait()

	if store.Len() != 1000 {
		t.Errorf("expected 1000 traces, got %d", store.Len())
	}
}

func TestTraceStore_Middleware(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 100})

	provider := &mockProvider{
		name:         "openai",
		defaultModel: "gpt-4o",
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return &Response{
				ID:       "resp-123",
				Model:    "gpt-4o",
				Content:  "Hello!",
				Provider: "openai",
				Usage:    Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}

	tracer := NewTracer("test", WithProvider("openai"))

	// Call with middleware
	resp, err := tracer.Chat(context.Background(), &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}, provider, WithCallMiddleware(store.Middleware()))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected Hello!, got %s", resp.Content)
	}

	// Give middleware time to store
	time.Sleep(10 * time.Millisecond)

	if store.Len() != 1 {
		t.Fatalf("expected 1 trace, got %d", store.Len())
	}

	traces := store.All()
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}

	rec := traces[0]
	if rec.Status != "success" {
		t.Errorf("expected status success, got %s", rec.Status)
	}
	if rec.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", rec.Provider)
	}
	if rec.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", rec.Model)
	}
	if rec.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", rec.InputTokens)
	}
	if rec.OutputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", rec.OutputTokens)
	}
	if rec.ResponseID != "resp-123" {
		t.Errorf("expected resp-123, got %s", rec.ResponseID)
	}
}

func TestTraceStore_Middleware_Error(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 100})

	provider := &mockProvider{
		name:         "openai",
		defaultModel: "gpt-4o",
		completeFunc: func(ctx context.Context, req *Request) (*Response, error) {
			return nil, errors.New("rate limit exceeded")
		},
	}

	tracer := NewTracer("test", WithProvider("openai"))

	_, _ = tracer.Chat(context.Background(), &Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}, provider, WithCallMiddleware(store.Middleware()))

	time.Sleep(10 * time.Millisecond)

	if store.Len() != 1 {
		t.Fatalf("expected 1 trace, got %d", store.Len())
	}

	rec := store.All()[0]
	if rec.Status != "error" {
		t.Errorf("expected status error, got %s", rec.Status)
	}
	if rec.Error != "rate limit exceeded" {
		t.Errorf("expected error message, got %s", rec.Error)
	}
}

func TestTraceStore_IDGeneration(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 10})

	store.Add(TraceRecord{Provider: "a", Status: "success"})
	store.Add(TraceRecord{Provider: "b", Status: "success"})

	all := store.All()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	// IDs should be unique
	if all[0].ID == all[1].ID {
		t.Errorf("expected unique IDs, got %s and %s", all[0].ID, all[1].ID)
	}

	// Should have trace- prefix
	if all[0].ID[:6] != "trace-" {
		t.Errorf("expected trace- prefix, got %s", all[0].ID)
	}
}

func TestTraceStore_CustomID(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{MaxSize: 10})

	store.Add(TraceRecord{ID: "custom-id-123", Provider: "test", Status: "success"})

	all := store.All()
	if all[0].ID != "custom-id-123" {
		t.Errorf("expected custom-id-123, got %s", all[0].ID)
	}
}

func TestFormatTraceID(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{1, "trace-00000001"},
		{255, "trace-000000ff"},
		{4096, "trace-00001000"},
	}

	for _, tt := range tests {
		got := formatTraceID(tt.n)
		if got != tt.want {
			t.Errorf("formatTraceID(%d) = %s, want %s", tt.n, got, tt.want)
		}
	}
}

func TestMatchTrace(t *testing.T) {
	rec := &TraceRecord{
		Provider:  "openai",
		Model:     "gpt-4o",
		Status:    "success",
		StartedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name  string
		query TraceQuery
		want  bool
	}{
		{"no filters", TraceQuery{}, true},
		{"provider match", TraceQuery{Provider: "openai"}, true},
		{"provider no match", TraceQuery{Provider: "anthropic"}, false},
		{"model match", TraceQuery{Model: "gpt-4o"}, true},
		{"model no match", TraceQuery{Model: "gpt-3.5"}, false},
		{"status match", TraceQuery{Status: "success"}, true},
		{"status no match", TraceQuery{Status: "error"}, false},
		{"since before", TraceQuery{Since: time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)}, true},
		{"since after", TraceQuery{Since: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)}, false},
		{"until after", TraceQuery{Until: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)}, true},
		{"until before", TraceQuery{Until: time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchTrace(rec, tt.query)
			if got != tt.want {
				t.Errorf("matchTrace = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTraceStore_DefaultSize(t *testing.T) {
	store := NewTraceStore(TraceStoreConfig{})
	// Should have default max size of 10000
	// We can verify by adding up to that count conceptually
	if store.maxSize != 10000 {
		t.Errorf("expected default maxSize 10000, got %d", store.maxSize)
	}
}
