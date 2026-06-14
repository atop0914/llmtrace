package llmtrace

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestChain_Order(t *testing.T) {
	var order []int

	mw1 := func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			order = append(order, 1)
			resp, err := next(ctx, req)
			order = append(order, 4)
			return resp, err
		}
	}

	mw2 := func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			order = append(order, 2)
			resp, err := next(ctx, req)
			order = append(order, 3)
			return resp, err
		}
	}

	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "ok"}, nil
	}

	chain := Chain(mw1, mw2)
	fn := chain(inner)

	_, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect: 1 (mw1 before), 2 (mw2 before), 3 (mw2 after), 4 (mw1 after)
	if len(order) != 4 {
		t.Fatalf("order length = %d, want 4", len(order))
	}
	for i, v := range []int{1, 2, 3, 4} {
		if order[i] != v {
			t.Errorf("order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

func TestChain_Empty(t *testing.T) {
	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "direct"}, nil
	}

	chain := Chain()
	fn := chain(inner)
	resp, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "direct" {
		t.Errorf("Content = %q, want %q", resp.Content, "direct")
	}
}

func TestChainStream_Order(t *testing.T) {
	var order []int

	mw1 := func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			order = append(order, 1)
			return next(ctx, req)
		}
	}

	mw2 := func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			order = append(order, 2)
			return next(ctx, req)
		}
	}

	inner := func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
		ch := make(chan StreamChunk, 1)
		ch <- StreamChunk{Content: "ok"}
		close(ch)
		return ch, nil
	}

	chain := ChainStream(mw1, mw2)
	fn := chain(inner)

	_, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("order = %v, want [1 2]", order)
	}
}

func TestWithCompleteHook(t *testing.T) {
	var mu sync.Mutex
	var captured *Response
	var capturedErr error

	hook := func(ctx context.Context, req *Request, resp *Response, err error) {
		mu.Lock()
		defer mu.Unlock()
		captured = resp
		capturedErr = err
	}

	mw := WithCompleteHook(hook)
	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "hello"}, nil
	}

	fn := mw(inner)
	resp, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if captured != resp {
		t.Error("hook should receive the response")
	}
	if capturedErr != nil {
		t.Errorf("hook error = %v, want nil", capturedErr)
	}
}

func TestWithCompleteHook_WithError(t *testing.T) {
	var mu sync.Mutex
	var capturedErr error

	hook := func(ctx context.Context, req *Request, resp *Response, err error) {
		mu.Lock()
		defer mu.Unlock()
		capturedErr = err
	}

	mw := WithCompleteHook(hook)
	expectedErr := errors.New("api failure")
	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return nil, expectedErr
	}

	fn := mw(inner)
	_, err := fn(context.Background(), &Request{Model: "test"})
	if err != expectedErr {
		t.Fatalf("error = %v, want %v", err, expectedErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedErr != expectedErr {
		t.Errorf("hook error = %v, want %v", capturedErr, expectedErr)
	}
}

func TestWithTiming(t *testing.T) {
	type timingResult struct {
		model    string
		duration float64
	}
	var mu sync.Mutex
	var results []timingResult

	callback := func(req *Request, durationMS float64) {
		mu.Lock()
		defer mu.Unlock()
		results = append(results, timingResult{model: req.Model, duration: durationMS})
	}

	mw := WithTiming(callback)
	inner := func(ctx context.Context, req *Request) (*Response, error) {
		return &Response{Content: "ok"}, nil
	}

	fn := mw(inner)
	_, err := fn(context.Background(), &Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].model != "gpt-4o" {
		t.Errorf("model = %q, want %q", results[0].model, "gpt-4o")
	}
	if results[0].duration < 0 {
		t.Errorf("duration = %f, want >= 0", results[0].duration)
	}
}

func TestChain_WithMultipleMiddlewares(t *testing.T) {
	var log []string

	logging := func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			log = append(log, "before:"+req.Model)
			resp, err := next(ctx, req)
			log = append(log, "after:"+req.Model)
			return resp, err
		}
	}

	auth := func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			log = append(log, "auth-check")
			return next(ctx, req)
		}
	}

	inner := func(ctx context.Context, req *Request) (*Response, error) {
		log = append(log, "execute")
		return &Response{Content: "ok"}, nil
	}

	chain := Chain(logging, auth)
	fn := chain(inner)

	_, err := fn(context.Background(), &Request{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"before:test", "auth-check", "execute", "after:test"}
	if len(log) != len(expected) {
		t.Fatalf("log length = %d, want %d", len(log), len(expected))
	}
	for i, v := range expected {
		if log[i] != v {
			t.Errorf("log[%d] = %q, want %q", i, log[i], v)
		}
	}
}
