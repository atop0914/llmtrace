package tokencount

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

// mockProvider implements a minimal provider for testing middleware.
type mockProvider struct {
	completeFunc func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error)
	streamFunc   func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error)
}

func (m *mockProvider) Name() string                    { return "mock" }
func (m *mockProvider) DefaultModel() string             { return "gpt-4o" }
func (m *mockProvider) SupportsStreaming() bool           { return true }
func (m *mockProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &llmtrace.Response{
		Model:   req.Model,
		Content: "Hello!",
		Usage:   llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	ch := make(chan llmtrace.StreamChunk, 3)
	ch <- llmtrace.StreamChunk{Content: "Hello"}
	ch <- llmtrace.StreamChunk{Content: " world"}
	ch <- llmtrace.StreamChunk{Content: "!", Usage: &llmtrace.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}
	close(ch)
	return ch, nil
}

func TestWithTokenCount_BasicTracking(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
		WithManager(NewManager()),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello, how are you?"}},
	}

	resp, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	snap := stats.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 10 {
		t.Errorf("TotalInputTokens = %d, want 10", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 5 {
		t.Errorf("TotalOutputTokens = %d, want 5", snap.TotalOutputTokens)
	}
	if snap.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", snap.TotalTokens)
	}
}

func TestWithTokenCount_MultipleRequests(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	for i := 0; i < 5; i++ {
		_, err := fn(context.Background(), &llmtrace.Request{
			Model:    "gpt-4o",
			Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
		})
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	snap := stats.Snapshot()
	if snap.TotalRequests != 5 {
		t.Errorf("TotalRequests = %d, want 5", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 50 {
		t.Errorf("TotalInputTokens = %d, want 50", snap.TotalInputTokens)
	}
}

func TestWithTokenCount_RejectOnOverflow(t *testing.T) {
	stats := &TokenStats{}
	var rejected bool

	mw := WithTokenCount(
		WithTokenStats(stats),
		WithRejectOnOverflow(),
		WithUsageCallback(func(e TokenUsageEvent) {
			if e.Rejected {
				rejected = true
			}
		}),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	// Create a message that exceeds gpt-4o's 128k context window
	// At 4 chars/token, we need > 512000 chars
	bigContent := strings.Repeat("x", 600000)
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: bigContent}},
	}

	_, err := fn(context.Background(), req)
	if err == nil {
		t.Fatal("expected context overflow error")
	}

	var overflowErr *ErrContextOverflow
	if !errors.As(err, &overflowErr) {
		t.Fatalf("expected ErrContextOverflow, got %T: %v", err, err)
	}
	if overflowErr.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", overflowErr.Model)
	}

	snap := stats.Snapshot()
	if snap.RejectedRequests != 1 {
		t.Errorf("RejectedRequests = %d, want 1", snap.RejectedRequests)
	}
	if !rejected {
		t.Error("expected rejected flag in callback")
	}
}

func TestWithTokenCount_OverflowAllowedByDefault(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
		// RejectOnOverflow NOT set
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	bigContent := strings.Repeat("x", 600000)
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: bigContent}},
	}

	resp, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error (overflow should be allowed): %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	snap := stats.Snapshot()
	if snap.RejectedRequests != 0 {
		t.Errorf("RejectedRequests = %d, want 0", snap.RejectedRequests)
	}
}

func TestWithTokenCount_UsageCallback(t *testing.T) {
	var events []TokenUsageEvent

	mw := WithTokenCount(
		WithUsageCallback(func(e TokenUsageEvent) {
			events = append(events, e)
		}),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", e.Model)
	}
	if e.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", e.InputTokens)
	}
	if e.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", e.OutputTokens)
	}
	if e.Duration <= 0 {
		t.Error("expected positive duration")
	}
	if e.PreCallCheck == nil {
		t.Error("expected PreCallCheck to be set")
	}
}

func TestWithTokenCount_EstimateIfMissing(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
		WithEstimateIfMissing(true),
	)

	// Provider returns no usage data
	provider := &mockProvider{
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model:   req.Model,
				Content: "This is a response with some content to estimate",
			}, nil
		},
	}
	fn := mw(provider.Complete)

	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello, how are you today?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap := stats.Snapshot()
	// Should have estimated tokens based on input text length
	if snap.TotalInputTokens == 0 {
		t.Error("expected estimated input tokens > 0")
	}
	if snap.TotalOutputTokens == 0 {
		t.Error("expected estimated output tokens > 0")
	}
}

func TestWithTokenCount_NoEstimate(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
		WithEstimateIfMissing(false),
	)

	provider := &mockProvider{
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model:   req.Model,
				Content: "Response without usage",
			}, nil
		},
	}
	fn := mw(provider.Complete)

	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap := stats.Snapshot()
	if snap.TotalInputTokens != 0 {
		t.Errorf("TotalInputTokens = %d, want 0 (no estimate)", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 0 {
		t.Errorf("TotalOutputTokens = %d, want 0 (no estimate)", snap.TotalOutputTokens)
	}
}

func TestWithTokenCount_ErrorPassthrough(t *testing.T) {
	stats := &TokenStats{}
	var gotErr bool

	mw := WithTokenCount(
		WithTokenStats(stats),
		WithUsageCallback(func(e TokenUsageEvent) {
			if e.Error != nil {
				gotErr = true
			}
		}),
	)

	expectedErr := errors.New("provider error")
	provider := &mockProvider{
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return nil, expectedErr
		},
	}
	fn := mw(provider.Complete)

	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected provider error, got: %v", err)
	}
	if !gotErr {
		t.Error("expected error in callback")
	}

	snap := stats.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
	// No tokens tracked on error
	if snap.TotalInputTokens != 0 {
		t.Errorf("TotalInputTokens = %d, want 0", snap.TotalInputTokens)
	}
}

func TestWithTokenCount_UnknownModel(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	// Unknown model — validation returns error in CheckResult, but doesn't reject
	resp, err := fn(context.Background(), &llmtrace.Request{
		Model:    "unknown-model-xyz",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown model: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestWithTokenCount_UnknownModelReject(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(
		WithTokenStats(stats),
		WithRejectOnOverflow(),
	)

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	// Unknown model returns FitsContext=false with error message
	// With RejectOnOverflow, it should be rejected
	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "unknown-model-xyz",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for unknown model with RejectOnOverflow")
	}
}

func TestWithTokenCount_MaxTokensFromRequest(t *testing.T) {
	mw := WithTokenCount()

	provider := &mockProvider{}
	fn := mw(provider.Complete)

	maxT := 500
	resp, err := fn(context.Background(), &llmtrace.Request{
		Model:      "gpt-4o",
		Messages:   []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
		MaxTokens:  &maxT,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestWithTokenCount_SnapshotIsolation(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(WithTokenStats(stats))
	provider := &mockProvider{}
	fn := mw(provider.Complete)

	// Make a request
	_, _ = fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})

	snap1 := stats.Snapshot()

	// Make another request
	_, _ = fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello again"}},
	})

	snap2 := stats.Snapshot()

	// snap1 should be unchanged
	if snap1.TotalRequests != 1 {
		t.Errorf("snap1.TotalRequests = %d, want 1", snap1.TotalRequests)
	}
	if snap2.TotalRequests != 2 {
		t.Errorf("snap2.TotalRequests = %d, want 2", snap2.TotalRequests)
	}
}

func TestWithTokenCount_ConcurrentRequests(t *testing.T) {
	stats := &TokenStats{}
	mw := WithTokenCount(WithTokenStats(stats))
	provider := &mockProvider{}
	fn := mw(provider.Complete)

	const goroutines = 20
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = fn(context.Background(), &llmtrace.Request{
				Model:    "gpt-4o",
				Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
			})
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	snap := stats.Snapshot()
	if snap.TotalRequests != int64(goroutines) {
		t.Errorf("TotalRequests = %d, want %d", snap.TotalRequests, goroutines)
	}
	if snap.TotalInputTokens != int64(goroutines*10) {
		t.Errorf("TotalInputTokens = %d, want %d", snap.TotalInputTokens, goroutines*10)
	}
}

// --- Stream middleware tests ---

func TestWithStreamTokenCount_BasicTracking(t *testing.T) {
	stats := &TokenStats{}
	mw := WithStreamTokenCount(
		WithTokenStats(stats),
	)

	provider := &mockProvider{}
	fn := mw(provider.Stream)

	ch, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	// Wait a bit for goroutine to finish
	time.Sleep(50 * time.Millisecond)

	snap := stats.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
	if snap.TotalInputTokens != 10 {
		t.Errorf("TotalInputTokens = %d, want 10", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 5 {
		t.Errorf("TotalOutputTokens = %d, want 5", snap.TotalOutputTokens)
	}
}

func TestWithStreamTokenCount_RejectOnOverflow(t *testing.T) {
	stats := &TokenStats{}
	mw := WithStreamTokenCount(
		WithTokenStats(stats),
		WithRejectOnOverflow(),
	)

	provider := &mockProvider{}
	fn := mw(provider.Stream)

	bigContent := strings.Repeat("x", 600000)
	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: bigContent}},
	})
	if err == nil {
		t.Fatal("expected context overflow error")
	}

	var overflowErr *ErrContextOverflow
	if !errors.As(err, &overflowErr) {
		t.Fatalf("expected ErrContextOverflow, got %T: %v", err, err)
	}

	snap := stats.Snapshot()
	if snap.RejectedRequests != 1 {
		t.Errorf("RejectedRequests = %d, want 1", snap.RejectedRequests)
	}
}

func TestWithStreamTokenCount_UsageCallback(t *testing.T) {
	var events []TokenUsageEvent

	mw := WithStreamTokenCount(
		WithUsageCallback(func(e TokenUsageEvent) {
			events = append(events, e)
		}),
	)

	provider := &mockProvider{}
	fn := mw(provider.Stream)

	ch, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for range ch {
	}
	time.Sleep(50 * time.Millisecond)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", events[0].InputTokens)
	}
}

func TestWithStreamTokenCount_EstimateMissing(t *testing.T) {
	stats := &TokenStats{}
	mw := WithStreamTokenCount(
		WithTokenStats(stats),
		WithEstimateIfMissing(true),
	)

	// Stream without usage data
	provider := &mockProvider{
		streamFunc: func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			ch := make(chan llmtrace.StreamChunk, 2)
			ch <- llmtrace.StreamChunk{Content: "Some response"}
			ch <- llmtrace.StreamChunk{Content: " content here"}
			close(ch)
			return ch, nil
		},
	}
	fn := mw(provider.Stream)

	ch, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello there friend"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}
	time.Sleep(50 * time.Millisecond)

	snap := stats.Snapshot()
	if snap.TotalInputTokens == 0 {
		t.Error("expected estimated input tokens > 0")
	}
	if snap.TotalOutputTokens == 0 {
		t.Error("expected estimated output tokens > 0")
	}
}

func TestWithStreamTokenCount_ProviderError(t *testing.T) {
	stats := &TokenStats{}
	mw := WithStreamTokenCount(
		WithTokenStats(stats),
	)

	expectedErr := errors.New("stream error")
	provider := &mockProvider{
		streamFunc: func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			return nil, expectedErr
		},
	}
	fn := mw(provider.Stream)

	_, err := fn(context.Background(), &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected stream error, got: %v", err)
	}

	snap := stats.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", snap.TotalRequests)
	}
}

func TestWithStreamTokenCount_ConcurrentDrain(t *testing.T) {
	stats := &TokenStats{}
	mw := WithStreamTokenCount(WithTokenStats(stats))
	provider := &mockProvider{}
	fn := mw(provider.Stream)

	const goroutines = 10
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			ch, err := fn(context.Background(), &llmtrace.Request{
				Model:    "gpt-4o",
				Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello"}},
			})
			if err != nil {
				return
			}
			for range ch {
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
	time.Sleep(100 * time.Millisecond)

	snap := stats.Snapshot()
	if snap.TotalRequests != int64(goroutines) {
		t.Errorf("TotalRequests = %d, want %d", snap.TotalRequests, goroutines)
	}
}

// --- TokenStats tests ---

func TestTokenStats_Snapshot(t *testing.T) {
	stats := &TokenStats{}
	stats.TotalInputTokens.Add(100)
	stats.TotalOutputTokens.Add(50)
	stats.TotalTokens.Add(150)
	stats.TotalRequests.Add(3)
	stats.TotalCostMicros.Add(1234) // $0.001234
	stats.RejectedRequests.Add(1)

	snap := stats.Snapshot()
	if snap.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", snap.TotalInputTokens)
	}
	if snap.TotalOutputTokens != 50 {
		t.Errorf("TotalOutputTokens = %d, want 50", snap.TotalOutputTokens)
	}
	if snap.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", snap.TotalRequests)
	}
	if snap.RejectedRequests != 1 {
		t.Errorf("RejectedRequests = %d, want 1", snap.RejectedRequests)
	}
	// Cost is float64 from micros
	if snap.TotalCostUSD < 0.0012 || snap.TotalCostUSD > 0.0013 {
		t.Errorf("TotalCostUSD = %f, want ~0.001234", snap.TotalCostUSD)
	}
}

// --- ErrContextOverflow tests ---

func TestErrContextOverflow_Error(t *testing.T) {
	err := &ErrContextOverflow{
		Model:       "gpt-4o",
		InputTokens: 200000,
		ContextSize: 128000,
	}
	msg := err.Error()
	if !strings.Contains(msg, "gpt-4o") {
		t.Errorf("error message should contain model name: %s", msg)
	}
	if !strings.Contains(msg, "200000") {
		t.Errorf("error message should contain input tokens: %s", msg)
	}
	if !strings.Contains(msg, "128000") {
		t.Errorf("error message should contain context size: %s", msg)
	}
}

func TestErrContextOverflow_As(t *testing.T) {
	var err error = &ErrContextOverflow{Model: "test", InputTokens: 100, ContextSize: 50}
	var target *ErrContextOverflow
	if !errors.As(err, &target) {
		t.Error("errors.As should work with ErrContextOverflow")
	}
	if target.Model != "test" {
		t.Errorf("Model = %q, want test", target.Model)
	}
}

// --- Benchmark ---

func BenchmarkWithTokenCount(b *testing.B) {
	mw := WithTokenCount()
	provider := &mockProvider{}
	fn := mw(provider.Complete)

	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello, how are you today?"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fn(context.Background(), req)
	}
}

func BenchmarkWithTokenCount_Parallel(b *testing.B) {
	mw := WithTokenCount()
	provider := &mockProvider{}
	fn := mw(provider.Complete)

	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "Hello, how are you today?"}},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = fn(context.Background(), req)
		}
	})
}
