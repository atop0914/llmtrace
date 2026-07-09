package batch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

// mockProvider is a test provider that returns configurable responses.
type mockProvider struct {
	name          string
	completeFunc  func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error)
	defaultModel  string
}

func (m *mockProvider) Name() string            { return m.name }
func (m *mockProvider) DefaultModel() string     { return m.defaultModel }
func (m *mockProvider) SupportsStreaming() bool   { return false }
func (m *mockProvider) Stream(_ context.Context, _ *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &llmtrace.Response{
		ID:           "test-id",
		Model:        req.Model,
		Content:      "test response",
		FinishReason: "stop",
		Usage: llmtrace.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
		Latency:  10 * time.Millisecond,
		Provider: m.name,
	}, nil
}

// --- Unit Tests ---

func TestNew(t *testing.T) {
	provider := &mockProvider{name: "test"}
	b := New(provider)
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.provider != provider {
		t.Error("provider not set")
	}
	if b.config.MaxConcurrency != 10 {
		t.Errorf("default MaxConcurrency = %d, want 10", b.config.MaxConcurrency)
	}
}

func TestNewWithOptions(t *testing.T) {
	provider := &mockProvider{name: "test"}
	progressCalled := false
	b := New(provider,
		WithMaxConcurrency(3),
		WithTimeout(5*time.Second),
		WithPerItemTimeout(2*time.Second),
		WithErrorHandling(ErrorCancel),
		WithOnProgress(func(item int, result *Result) {
			progressCalled = true
		}),
		WithName("my-batch"),
	)

	if b.config.MaxConcurrency != 3 {
		t.Errorf("MaxConcurrency = %d, want 3", b.config.MaxConcurrency)
	}
	if b.config.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", b.config.Timeout)
	}
	if b.config.PerItemTimeout != 2*time.Second {
		t.Errorf("PerItemTimeout = %v, want 2s", b.config.PerItemTimeout)
	}
	if b.config.OnError != ErrorCancel {
		t.Error("OnError not set to ErrorCancel")
	}
	if b.config.OnProgress == nil {
		t.Error("OnProgress not set")
	}
	if b.config.Name != "my-batch" {
		t.Errorf("Name = %q, want %q", b.config.Name, "my-batch")
	}
	_ = progressCalled
}

func TestRun_Empty(t *testing.T) {
	provider := &mockProvider{name: "test"}
	b := New(provider)
	resp, err := b.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Metrics.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", resp.Metrics.TotalRequests)
	}
	if len(resp.Items) != 0 {
		t.Errorf("Items len = %d, want 0", len(resp.Items))
	}
}

func TestRun_SingleRequest(t *testing.T) {
	provider := &mockProvider{name: "test"}
	b := New(provider)
	req := &llmtrace.Request{
		Model:    "gpt-4",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	}
	resp, err := b.Run(context.Background(), []*llmtrace.Request{req})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("Items len = %d, want 1", len(resp.Items))
	}
	if resp.Items[0].Error != nil {
		t.Fatalf("item error: %v", resp.Items[0].Error)
	}
	if resp.Items[0].Response.Content != "test response" {
		t.Errorf("content = %q, want %q", resp.Items[0].Response.Content, "test response")
	}
	if resp.Metrics.Successful != 1 {
		t.Errorf("Successful = %d, want 1", resp.Metrics.Successful)
	}
	if resp.Metrics.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", resp.Metrics.TotalTokens)
	}
}

func TestRun_MultipleRequests(t *testing.T) {
	provider := &mockProvider{name: "test"}
	b := New(provider, WithMaxConcurrency(3))

	requests := make([]*llmtrace.Request, 10)
	for i := range requests {
		requests[i] = &llmtrace.Request{
			Model:    "gpt-4",
			Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: fmt.Sprintf("msg %d", i)}},
		}
	}

	resp, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 10 {
		t.Fatalf("Items len = %d, want 10", len(resp.Items))
	}
	if resp.Metrics.TotalRequests != 10 {
		t.Errorf("TotalRequests = %d, want 10", resp.Metrics.TotalRequests)
	}
	if resp.Metrics.Successful != 10 {
		t.Errorf("Successful = %d, want 10", resp.Metrics.Successful)
	}
	if resp.Metrics.Failed != 0 {
		t.Errorf("Failed = %d, want 0", resp.Metrics.Failed)
	}
	if resp.Metrics.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", resp.Metrics.TotalTokens)
	}
	if resp.Metrics.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.Metrics.InputTokens)
	}
	if resp.Metrics.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", resp.Metrics.OutputTokens)
	}
	if resp.Metrics.TotalLatency <= 0 {
		t.Error("TotalLatency should be positive")
	}
	if resp.Metrics.AvgLatency <= 0 {
		t.Error("AvgLatency should be positive")
	}
	if resp.Canceled {
		t.Error("batch should not be canceled")
	}
}

func TestRun_PartialFailure(t *testing.T) {
	var callCount atomic.Int32
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			n := callCount.Add(1)
			if n%3 == 0 {
				return nil, fmt.Errorf("simulated error %d", n)
			}
			return &llmtrace.Response{
				Model:   req.Model,
				Content: fmt.Sprintf("response %d", n),
				Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			}, nil
		},
	}
	b := New(provider)

	requests := make([]*llmtrace.Request, 9)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	resp, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Metrics.Successful != 6 {
		t.Errorf("Successful = %d, want 6", resp.Metrics.Successful)
	}
	if resp.Metrics.Failed != 3 {
		t.Errorf("Failed = %d, want 3", resp.Metrics.Failed)
	}
	// TotalTokens should only count successful requests
	if resp.Metrics.TotalTokens != 180 {
		t.Errorf("TotalTokens = %d, want 180", resp.Metrics.TotalTokens)
	}
}

func TestRun_ErrorCancel(t *testing.T) {
	var callCount atomic.Int32
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			n := callCount.Add(1)
			if n == 2 {
				return nil, fmt.Errorf("fail on 2nd request")
			}
			// Slow requests to give time for cancel to propagate
			time.Sleep(50 * time.Millisecond)
			return &llmtrace.Response{
				Model:   req.Model,
				Content: "ok",
				Usage:   llmtrace.Usage{TotalTokens: 10},
			}, nil
		},
	}
	b := New(provider, WithErrorHandling(ErrorCancel), WithMaxConcurrency(1))

	requests := make([]*llmtrace.Request, 5)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	resp, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Canceled {
		t.Error("batch should be canceled")
	}
	if resp.Metrics.Failed == 0 {
		t.Error("should have at least 1 failure")
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return &llmtrace.Response{Model: req.Model}, nil
			}
		},
	}
	b := New(provider, WithMaxConcurrency(2))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	requests := make([]*llmtrace.Request, 5)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	resp, err := b.Run(ctx, requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All should either fail with context error or be nil (not started)
	for i, r := range resp.Items {
		if r == nil {
			continue
		}
		if r.Error == nil && r.Response == nil {
			t.Errorf("item %d: neither error nor response set", i)
		}
	}
	if !resp.Canceled {
		t.Error("batch should report as canceled")
	}
}

func TestRun_PerItemTimeout(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return &llmtrace.Response{
					Model: req.Model,
					Usage: llmtrace.Usage{TotalTokens: 10},
				}, nil
			}
		},
	}
	b := New(provider, WithPerItemTimeout(50*time.Millisecond))

	req := &llmtrace.Request{Model: "gpt-4"}
	resp, err := b.Run(context.Background(), []*llmtrace.Request{req})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Items[0].Error == nil {
		t.Error("expected timeout error")
	}
}

func TestRun_ProgressCallback(t *testing.T) {
	provider := &mockProvider{name: "test"}
	var completed atomic.Int32
	var mu sync.Mutex
	completedIndices := make([]int, 0, 5)

	b := New(provider, WithOnProgress(func(item int, result *Result) {
		completed.Add(1)
		mu.Lock()
		completedIndices = append(completedIndices, item)
		mu.Unlock()
	}))

	requests := make([]*llmtrace.Request, 5)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	_, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for all callbacks
	time.Sleep(50 * time.Millisecond)
	if got := completed.Load(); got != 5 {
		t.Errorf("progress callback called %d times, want 5", got)
	}
}

func TestRunItems_Metadata(t *testing.T) {
	provider := &mockProvider{name: "test"}
	b := New(provider)

	items := []*Item{
		{Request: &llmtrace.Request{Model: "gpt-4"}, Metadata: "item-0"},
		{Request: &llmtrace.Request{Model: "gpt-4"}, Metadata: 42},
		{Request: &llmtrace.Request{Model: "gpt-4"}, Metadata: nil},
	}

	resp, err := b.RunItems(context.Background(), items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Items[0].Metadata != "item-0" {
		t.Errorf("item 0 metadata = %v, want %q", resp.Items[0].Metadata, "item-0")
	}
	if resp.Items[1].Metadata != 42 {
		t.Errorf("item 1 metadata = %v, want 42", resp.Items[1].Metadata)
	}
	if resp.Items[2].Metadata != nil {
		t.Errorf("item 2 metadata = %v, want nil", resp.Items[2].Metadata)
	}
}

func TestRun_ConcurrencyLimiting(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			cur := concurrent.Add(1)
			// Track max concurrency
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			concurrent.Add(-1)
			return &llmtrace.Response{
				Model: req.Model,
				Usage: llmtrace.Usage{TotalTokens: 10},
			}, nil
		},
	}

	b := New(provider, WithMaxConcurrency(3))

	requests := make([]*llmtrace.Request, 20)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	_, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := maxConcurrent.Load(); got > 3 {
		t.Errorf("max concurrent = %d, want <= 3", got)
	}
}

func TestRun_OrderPreserved(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			// Simulate variable latency
			idx := 0
			fmt.Sscanf(req.Messages[0].Content, "msg %d", &idx)
			time.Sleep(time.Duration(idx*5) * time.Millisecond)
			return &llmtrace.Response{
				Model:   req.Model,
				Content: fmt.Sprintf("response-%d", idx),
				Usage:   llmtrace.Usage{TotalTokens: idx},
			}, nil
		},
	}

	b := New(provider, WithMaxConcurrency(5))

	requests := make([]*llmtrace.Request, 8)
	for i := range requests {
		requests[i] = &llmtrace.Request{
			Model:    "gpt-4",
			Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: fmt.Sprintf("msg %d", i)}},
		}
	}

	resp, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Results should be in original order despite different latencies
	for i, r := range resp.Items {
		if r.Index != i {
			t.Errorf("item %d: Index = %d, want %d", i, r.Index, i)
		}
		if r.Response == nil {
			t.Errorf("item %d: nil response", i)
			continue
		}
		expected := fmt.Sprintf("response-%d", i)
		if r.Response.Content != expected {
			t.Errorf("item %d: content = %q, want %q", i, r.Response.Content, expected)
		}
	}
}

func TestRun_AllFail(t *testing.T) {
	provider := &mockProvider{
		name: "test",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return nil, fmt.Errorf("always fail")
		},
	}
	b := New(provider)

	requests := []*llmtrace.Request{
		{Model: "gpt-4"},
		{Model: "gpt-4"},
	}

	resp, err := b.Run(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Metrics.Successful != 0 {
		t.Errorf("Successful = %d, want 0", resp.Metrics.Successful)
	}
	if resp.Metrics.Failed != 2 {
		t.Errorf("Failed = %d, want 2", resp.Metrics.Failed)
	}
	if resp.Metrics.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", resp.Metrics.TotalTokens)
	}
	if resp.Metrics.MinLatency != 0 {
		t.Errorf("MinLatency = %v, want 0", resp.Metrics.MinLatency)
	}
}

func TestComputeMetrics_Empty(t *testing.T) {
	m := computeMetrics(nil, 0)
	if m.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", m.TotalRequests)
	}
}

func TestComputeMetrics_AllNil(t *testing.T) {
	results := []*Result{nil, nil, nil}
	m := computeMetrics(results, 100*time.Millisecond)
	if m.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", m.TotalRequests)
	}
	if m.Successful != 0 || m.Failed != 0 {
		t.Errorf("Successful = %d, Failed = %d, want 0, 0", m.Successful, m.Failed)
	}
}

// --- Benchmarks ---

func BenchmarkRun_10Requests(b *testing.B) {
	benchmarkRun(b, 10, 5)
}

func BenchmarkRun_50Requests(b *testing.B) {
	benchmarkRun(b, 50, 10)
}

func BenchmarkRun_100Requests(b *testing.B) {
	benchmarkRun(b, 100, 20)
}

func benchmarkRun(b *testing.B, numRequests, concurrency int) {
	provider := &mockProvider{
		name: "bench",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model:  req.Model,
				Usage:  llmtrace.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			}, nil
		},
	}
	batcher := New(provider, WithMaxConcurrency(concurrency))

	requests := make([]*llmtrace.Request, numRequests)
	for i := range requests {
		requests[i] = &llmtrace.Request{Model: "gpt-4"}
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = batcher.Run(ctx, requests)
	}
}

func BenchmarkRunItems_WithMetadata(b *testing.B) {
	provider := &mockProvider{
		name: "bench",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model: req.Model,
				Usage: llmtrace.Usage{TotalTokens: 10},
			}, nil
		},
	}
	batcher := New(provider, WithMaxConcurrency(10))

	items := make([]*Item, 20)
	for i := range items {
		items[i] = &Item{
			Request:  &llmtrace.Request{Model: "gpt-4"},
			Metadata: i,
		}
	}

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = batcher.RunItems(ctx, items)
	}
}
