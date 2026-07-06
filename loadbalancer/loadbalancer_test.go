package loadbalancer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

// mockProvider is a test provider that tracks calls and can be configured to fail.
type mockProvider struct {
	name         string
	model        string
	completeFunc func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error)
	streamFunc   func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error)
	calls        atomic.Int64
}

func (p *mockProvider) Name() string           { return p.name }
func (p *mockProvider) DefaultModel() string    { return p.model }
func (p *mockProvider) SupportsStreaming() bool { return p.streamFunc != nil }

func (p *mockProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	p.calls.Add(1)
	if p.completeFunc != nil {
		return p.completeFunc(ctx, req)
	}
	return &llmtrace.Response{
		Content:  "ok",
		Provider: p.name,
		Model:    req.Model,
	}, nil
}

func (p *mockProvider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	p.calls.Add(1)
	if p.streamFunc != nil {
		return p.streamFunc(ctx, req)
	}
	ch := make(chan llmtrace.StreamChunk, 1)
	ch <- llmtrace.StreamChunk{Content: "stream-ok"}
	close(ch)
	return ch, nil
}

func defaultStreamFunc(_ context.Context, _ *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	ch := make(chan llmtrace.StreamChunk, 1)
	ch <- llmtrace.StreamChunk{Content: "default-stream"}
	close(ch)
	return ch, nil
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{name: name, model: "test-model", streamFunc: defaultStreamFunc}
}

func newSimpleMockProvider(name string) *mockProvider {
	return &mockProvider{name: name, model: "test-model"}
}

func newFailingProvider(name string) *mockProvider {
	return &mockProvider{
		name:  name,
		model: "test-model",
		streamFunc: func(_ context.Context, _ *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			return nil, errors.New("stream error")
		},
		completeFunc: func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
			return nil, errors.New("provider error")
		},
	}
}

func newSlowProvider(name string, delay time.Duration) *mockProvider {
	return &mockProvider{
		name:  name,
		model: "test-model",
		streamFunc: defaultStreamFunc,
		completeFunc: func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
			time.Sleep(delay)
			return &llmtrace.Response{
				Content:  "slow-ok",
				Provider: name,
			}, nil
		},
	}
}

func testRequest() *llmtrace.Request {
	return &llmtrace.Request{
		Model: "test-model",
		Messages: []llmtrace.Message{
			{Role: "user", Content: "hello"},
		},
	}
}

// --- Basic tests ---

func TestNew_NoEndpoints(t *testing.T) {
	lb := New()
	_, err := lb.Complete(context.Background(), testRequest())
	if !errors.Is(err, ErrNoEndpoints) {
		t.Errorf("expected ErrNoEndpoints, got %v", err)
	}
}

func TestNew_Defaults(t *testing.T) {
	ep := NewEndpoint("test", newMockProvider("test"))
	lb := New(WithEndpoints(ep))
	if lb.Name() != "loadbalancer-round-robin" {
		t.Errorf("expected round-robin strategy by default, got %s", lb.Name())
	}
	lb.Stop()
}

func TestName_Strategies(t *testing.T) {
	tests := []struct {
		strategy Strategy
		want     string
	}{
		{RoundRobin, "loadbalancer-round-robin"},
		{LeastLatency, "loadbalancer-least-latency"},
		{Random, "loadbalancer-random"},
		{Weighted, "loadbalancer-weighted"},
		{Strategy(99), "loadbalancer"},
	}
	for _, tt := range tests {
		lb := New(WithStrategy(tt.strategy), WithEndpoints(NewEndpoint("e", newMockProvider("m"))))
		if got := lb.Name(); got != tt.want {
			t.Errorf("strategy %d: got %q, want %q", tt.strategy, got, tt.want)
		}
		lb.Stop()
	}
}

// --- Round Robin ---

func TestRoundRobin_DistributesEvenly(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")
	p3 := newMockProvider("p3")

	lb := New(
		WithStrategy(RoundRobin),
		WithEndpoints(
			NewEndpoint("e1", p1),
			NewEndpoint("e2", p2),
			NewEndpoint("e3", p3),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	for i := 0; i < 9; i++ {
		_, err := lb.Complete(ctx, testRequest())
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	if c := p1.calls.Load(); c != 3 {
		t.Errorf("p1 calls: got %d, want 3", c)
	}
	if c := p2.calls.Load(); c != 3 {
		t.Errorf("p2 calls: got %d, want 3", c)
	}
	if c := p3.calls.Load(); c != 3 {
		t.Errorf("p3 calls: got %d, want 3", c)
	}
}

// --- Least Latency ---

func TestLeastLatency_PrefersFastest(t *testing.T) {
	fast := newMockProvider("fast")
	slow := newSlowProvider("slow", 50*time.Millisecond)

	lb := New(
		WithStrategy(LeastLatency),
		WithEndpoints(
			NewEndpoint("fast", fast),
			NewEndpoint("slow", slow),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()

	// First call: no latency data, either could be chosen
	lb.Complete(ctx, testRequest())
	time.Sleep(10 * time.Millisecond)

	// Subsequent calls should prefer the fast provider
	for i := 0; i < 5; i++ {
		lb.Complete(ctx, testRequest())
	}

	fastCalls := fast.calls.Load()
	slowCalls := slow.calls.Load()

	if fastCalls <= slowCalls {
		t.Errorf("fast (%d) should have more calls than slow (%d)", fastCalls, slowCalls)
	}
}

// --- Random ---

func TestRandom_SelectsEndpoints(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")

	lb := New(
		WithStrategy(Random),
		WithEndpoints(
			NewEndpoint("e1", p1),
			NewEndpoint("e2", p2),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	for i := 0; i < 100; i++ {
		lb.Complete(ctx, testRequest())
	}

	if p1.calls.Load() == 0 || p2.calls.Load() == 0 {
		t.Errorf("both providers should receive calls: p1=%d, p2=%d",
			p1.calls.Load(), p2.calls.Load())
	}
}

// --- Weighted ---

func TestWeighted_DistributesByWeight(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")

	lb := New(
		WithStrategy(Weighted),
		WithEndpoints(
			&Endpoint{Name: "e1", Provider: p1, Weight: 3, healthy: true},
			&Endpoint{Name: "e2", Provider: p2, Weight: 1, healthy: true},
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	for i := 0; i < 400; i++ {
		lb.Complete(ctx, testRequest())
	}

	c1 := p1.calls.Load()
	c2 := p2.calls.Load()

	ratio := float64(c1) / float64(c2)
	if ratio < 2.0 || ratio > 4.0 {
		t.Errorf("weight ratio: got %.2f (c1=%d, c2=%d), want ~3.0", ratio, c1, c2)
	}
}

// --- Health tracking ---

func TestEndpoint_HealthTracking(t *testing.T) {
	ep := NewEndpoint("test", newMockProvider("test"))
	if !ep.IsHealthy() {
		t.Fatal("new endpoint should be healthy")
	}

	for i := 0; i < 3; i++ {
		ep.recordError()
	}
	if ep.IsHealthy() {
		t.Fatal("endpoint should be unhealthy after 3 consecutive failures")
	}

	ep.recordSuccess(10 * time.Millisecond)
	if !ep.IsHealthy() {
		t.Fatal("endpoint should be healthy after success")
	}
}

func TestEndpoint_Stats(t *testing.T) {
	ep := NewEndpoint("test", newMockProvider("test"))
	ep.recordSuccess(100 * time.Millisecond)
	ep.recordSuccess(200 * time.Millisecond)
	ep.recordError()

	stats := ep.Stats()
	if stats.Name != "test" {
		t.Errorf("name: got %q, want %q", stats.Name, "test")
	}
	if stats.TotalCalls != 3 {
		t.Errorf("total calls: got %d, want 3", stats.TotalCalls)
	}
	if stats.TotalErrors != 1 {
		t.Errorf("total errors: got %d, want 1", stats.TotalErrors)
	}
	if stats.ErrorRate < 0.32 || stats.ErrorRate > 0.34 {
		t.Errorf("error rate: got %.2f, want ~0.33", stats.ErrorRate)
	}
	if !stats.Healthy {
		t.Error("should still be healthy (1 error, threshold is 3)")
	}
}

// --- Failover ---

func TestFailover_OnError(t *testing.T) {
	primary := newFailingProvider("primary")
	backup := newMockProvider("backup")

	lb := New(
		WithEndpoints(
			NewEndpoint("primary", primary),
			NewEndpoint("backup", backup),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	resp, err := lb.Complete(ctx, testRequest())
	if err != nil {
		t.Fatalf("expected success via failover, got %v", err)
	}
	if resp.Provider != "backup" {
		t.Errorf("expected backup provider, got %s", resp.Provider)
	}
}

func TestFailover_AllFail(t *testing.T) {
	p1 := newFailingProvider("p1")
	p2 := newFailingProvider("p2")

	lb := New(
		WithEndpoints(
			NewEndpoint("p1", p1),
			NewEndpoint("p2", p2),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	_, err := lb.Complete(ctx, testRequest())
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

// --- No healthy endpoints ---

func TestNoHealthyEndpoints(t *testing.T) {
	ep1 := NewEndpoint("e1", newMockProvider("p1"))
	ep2 := NewEndpoint("e2", newMockProvider("p2"))

	for i := 0; i < 3; i++ {
		ep1.recordError()
		ep2.recordError()
	}

	lb := New(
		WithEndpoints(ep1, ep2),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	_, err := lb.Complete(context.Background(), testRequest())
	if !errors.Is(err, ErrNoHealthyEndpoints) {
		t.Errorf("expected ErrNoHealthyEndpoints, got %v", err)
	}
}

// --- Streaming ---

func TestStream_Success(t *testing.T) {
	p := newMockProvider("test")
	lb := New(
		WithEndpoints(NewEndpoint("e", p)),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ch, err := lb.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk.Content)
	}

	if len(chunks) != 1 || chunks[0] != "default-stream" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestStream_Failover(t *testing.T) {
	primary := &mockProvider{
		name:  "primary",
		model: "test-model",
		streamFunc: func(_ context.Context, _ *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			return nil, errors.New("stream error")
		},
	}
	backup := newMockProvider("backup")

	lb := New(
		WithEndpoints(
			NewEndpoint("primary", primary),
			NewEndpoint("backup", backup),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ch, err := lb.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("expected failover success, got %v", err)
	}

	var content string
	for chunk := range ch {
		content += chunk.Content
	}
	if content != "default-stream" {
		t.Errorf("expected default-stream, got %q", content)
	}
}

// --- Provider interface ---

func TestDefaultModel(t *testing.T) {
	p := newMockProvider("test")
	p.model = "gpt-4o"
	lb := New(
		WithEndpoints(NewEndpoint("e", p)),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	if m := lb.DefaultModel(); m != "gpt-4o" {
		t.Errorf("got %q, want %q", m, "gpt-4o")
	}
}

func TestSupportsStreaming(t *testing.T) {
	p := newMockProvider("test")
	lb := New(
		WithEndpoints(NewEndpoint("e", p)),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	if !lb.SupportsStreaming() {
		t.Error("should support streaming")
	}
}

func TestSupportsStreaming_NoStreamFunc(t *testing.T) {
	p := newSimpleMockProvider("test")
	lb := New(
		WithEndpoints(NewEndpoint("e", p)),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	if lb.SupportsStreaming() {
		t.Error("should not support streaming without streamFunc")
	}
}

// --- Concurrent access ---

func TestConcurrentAccess(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")

	lb := New(
		WithStrategy(RoundRobin),
		WithEndpoints(
			NewEndpoint("e1", p1),
			NewEndpoint("e2", p2),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := lb.Complete(context.Background(), testRequest())
			if err != nil {
				t.Errorf("concurrent call failed: %v", err)
			}
		}()
	}
	wg.Wait()

	total := p1.calls.Load() + p2.calls.Load()
	if total != 50 {
		t.Errorf("total calls: got %d, want 50", total)
	}
}

// --- Endpoints() ---

func TestEndpoints_ReturnsStats(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")

	lb := New(
		WithEndpoints(
			NewEndpoint("e1", p1),
			NewEndpoint("e2", p2),
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	stats := lb.Endpoints()
	if len(stats) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(stats))
	}
	if stats[0].Name != "e1" || stats[1].Name != "e2" {
		t.Errorf("unexpected names: %q, %q", stats[0].Name, stats[1].Name)
	}
}

// --- Stop ---

func TestStop_Idempotent(t *testing.T) {
	lb := New(
		WithEndpoints(NewEndpoint("e", newMockProvider("p"))),
		WithHealthCheckInterval(0),
	)
	lb.Stop()
	lb.Stop()
	lb.Stop()
}

// --- Edge cases ---

func TestWeighted_ZeroWeight(t *testing.T) {
	p1 := newMockProvider("p1")
	p2 := newMockProvider("p2")

	lb := New(
		WithStrategy(Weighted),
		WithEndpoints(
			&Endpoint{Name: "e1", Provider: p1, Weight: 0, healthy: true},
			&Endpoint{Name: "e2", Provider: p2, Weight: 0, healthy: true},
		),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	for i := 0; i < 20; i++ {
		lb.Complete(ctx, testRequest())
	}

	c1 := p1.calls.Load()
	c2 := p2.calls.Load()
	diff := c1 - c2
	if diff < 0 {
		diff = -diff
	}
	if diff > 10 {
		t.Errorf("unequal distribution with zero weights: c1=%d, c2=%d", c1, c2)
	}
}

func TestSelectFallback_SameName(t *testing.T) {
	p := newFailingProvider("only")
	ep := NewEndpoint("only", p)

	lb := New(
		WithEndpoints(ep),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	_, err := lb.Complete(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error with single failing endpoint")
	}
}

func TestDefaultModel_NoHealthy(t *testing.T) {
	ep := NewEndpoint("e", newMockProvider("p"))
	ep.recordError()
	ep.recordError()
	ep.recordError()

	lb := New(
		WithEndpoints(ep),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	if m := lb.DefaultModel(); m != "test-model" {
		t.Errorf("got %q, want %q", m, "test-model")
	}
}

func TestEndpoint_AvgLatency(t *testing.T) {
	ep := NewEndpoint("test", newMockProvider("test"))

	ep.recordSuccess(100 * time.Millisecond)
	if ep.AvgLatency() != 100*time.Millisecond {
		t.Errorf("first latency: got %v, want 100ms", ep.AvgLatency())
	}

	ep.recordSuccess(200 * time.Millisecond)
	lat := ep.AvgLatency()
	if lat < 110*time.Millisecond || lat > 130*time.Millisecond {
		t.Errorf("EMA latency: got %v, want ~120ms", lat)
	}
}
