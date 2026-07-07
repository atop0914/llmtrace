package streammetric

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func TestWithStreamMetrics(t *testing.T) {
	var mu sync.Mutex
	var captured []Metrics
	var capturedModels []string

	mw := WithStreamMetrics(func(req *llmtrace.Request, m Metrics) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, m)
		capturedModels = append(capturedModels, req.Model)
	})

	// Create a mock stream function
	streamFn := func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
		ch := make(chan llmtrace.StreamChunk)
		go func() {
			defer close(ch)
			ch <- llmtrace.StreamChunk{Content: "Hello"}
			time.Sleep(5 * time.Millisecond)
			ch <- llmtrace.StreamChunk{Content: "Hello world"}
			time.Sleep(5 * time.Millisecond)
			ch <- llmtrace.StreamChunk{
				Content: "Hello world!",
				Usage: &llmtrace.Usage{
					InputTokens:  5,
					OutputTokens: 3,
					TotalTokens:  8,
				},
			}
		}()
		return ch, nil
	}

	// Apply middleware
	wrappedFn := mw(streamFn)

	req := &llmtrace.Request{
		Model: "test-model",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "hi"},
		},
	}

	ch, err := wrappedFn(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain the channel
	var chunks int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		chunks++
	}

	if chunks != 3 {
		t.Errorf("expected 3 chunks, got %d", chunks)
	}

	// Wait for async callback
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(captured) != 1 {
		t.Fatalf("expected 1 metrics callback, got %d", len(captured))
	}
	if capturedModels[0] != "test-model" {
		t.Errorf("expected model 'test-model', got %q", capturedModels[0])
	}
	if captured[0].ChunkCount != 3 {
		t.Errorf("expected 3 chunks in metrics, got %d", captured[0].ChunkCount)
	}
	if captured[0].TotalTokens != 3 {
		t.Errorf("expected 3 output tokens, got %d", captured[0].TotalTokens)
	}
	if captured[0].TTFT <= 0 {
		t.Errorf("expected positive TTFT, got %v", captured[0].TTFT)
	}
}

func TestWithStreamMetrics_Error(t *testing.T) {
	mw := WithStreamMetrics(func(req *llmtrace.Request, m Metrics) {
		t.Error("callback should not be called on error")
	})

	expectedErr := context.DeadlineExceeded
	streamFn := func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
		return nil, expectedErr
	}

	wrappedFn := mw(streamFn)
	_, err := wrappedFn(context.Background(), &llmtrace.Request{Model: "x"})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWithStreamMetrics_EmptyStream(t *testing.T) {
	var mu sync.Mutex
	var captured Metrics

	mw := WithStreamMetrics(func(req *llmtrace.Request, m Metrics) {
		mu.Lock()
		defer mu.Unlock()
		captured = m
	})

	streamFn := func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
		ch := make(chan llmtrace.StreamChunk)
		close(ch)
		return ch, nil
	}

	wrappedFn := mw(streamFn)
	ch, err := wrappedFn(context.Background(), &llmtrace.Request{Model: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for range ch {
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if captured.ChunkCount != 0 {
		t.Errorf("expected 0 chunks for empty stream, got %d", captured.ChunkCount)
	}
}

func TestWithStreamMetrics_ChainWithOtherMiddleware(t *testing.T) {
	var order []string

	// First middleware: timing
	timingMW := llmtrace.StreamMiddleware(func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			order = append(order, "timing-start")
			ch, err := next(ctx, req)
			if err != nil {
				return nil, err
			}
			out := make(chan llmtrace.StreamChunk)
			go func() {
				defer close(out)
				for c := range ch {
					out <- c
				}
				order = append(order, "timing-end")
			}()
			return out, nil
		}
	})

	// Second middleware: metrics
	metricsMW := WithStreamMetrics(func(req *llmtrace.Request, m Metrics) {
		order = append(order, "metrics-callback")
	})

	streamFn := func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
		ch := make(chan llmtrace.StreamChunk, 1)
		ch <- llmtrace.StreamChunk{Content: "hi"}
		close(ch)
		return ch, nil
	}

	// Chain: timing -> metrics -> stream
	wrappedFn := llmtrace.ChainStream(timingMW, metricsMW)(streamFn)
	ch, err := wrappedFn(context.Background(), &llmtrace.Request{Model: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
	}

	time.Sleep(200 * time.Millisecond)

	// The order should be: timing-start, timing-end, metrics-callback
	// But since timing wraps metrics, timing-end happens after metrics-end
	if len(order) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %v", len(order), order)
	}
	if order[0] != "timing-start" {
		t.Errorf("expected first event 'timing-start', got %q", order[0])
	}
}
