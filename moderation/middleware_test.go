package moderation

import (
	"context"
	"testing"

	"github.com/atop0914/llmtrace"
)

func mockComplete(content string) llmtrace.CompleteFunc {
	return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		return &llmtrace.Response{Content: content}, nil
	}
}

func TestMiddleware_AllowsCleanInput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"forbidden"}, ActionBlock, SeverityHigh, false))

	mw := Middleware(engine)
	handler := mw(mockComplete("response"))

	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: "user", Content: "hello world"},
		},
	}
	resp, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "response" {
		t.Errorf("content = %q, want %q", resp.Content, "response")
	}
}

func TestMiddleware_BlocksHarmfulInput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"forbidden"}, ActionBlock, SeverityHigh, false))

	mw := Middleware(engine)
	handler := mw(mockComplete("should not reach"))

	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: "user", Content: "this is forbidden content"},
		},
	}
	_, err := handler(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked content")
	}
	if !IsBlocked(err) {
		t.Errorf("expected blocked error, got: %v", err)
	}
}

func TestMiddleware_ChecksSystemMessages(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"evil"}, ActionBlock, SeverityCritical, false))

	mw := Middleware(engine)
	handler := mw(mockComplete("ok"))

	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: "system", Content: "you are evil"},
			{Role: "user", Content: "hello"},
		},
	}
	_, err := handler(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked system message")
	}
}

func TestMiddleware_SkipsAssistantMessages(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"forbidden"}, ActionBlock, SeverityHigh, false))

	mw := Middleware(engine)
	handler := mw(mockComplete("ok"))

	// Assistant messages should not be checked
	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: "assistant", Content: "forbidden word here"},
			{Role: "user", Content: "hello"},
		},
	}
	resp, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("assistant messages should not be checked: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestMiddleware_PassesContext(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"ok"}, ActionLog, SeverityLow, false))

	mw := Middleware(engine)
	handler := mw(func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		// Check that moderation result is in context
		r, ok := ResultFromContext(ctx)
		if !ok {
			t.Error("expected moderation result in context")
		}
		if r == nil || len(r.Matches) == 0 {
			t.Error("expected matches in context result")
		}
		return &llmtrace.Response{Content: "ok"}, nil
	})

	req := &llmtrace.Request{
		Messages: []llmtrace.Message{
			{Role: "user", Content: "ok then"},
		},
	}
	handler(context.Background(), req)
}

func TestOutputMiddleware_AllowsCleanOutput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"harmful"}, ActionBlock, SeverityHigh, false))

	mw := OutputMiddleware(engine)
	handler := mw(mockComplete("safe response"))

	req := &llmtrace.Request{}
	resp, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "safe response" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestOutputMiddleware_BlocksHarmfulOutput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"harmful"}, ActionBlock, SeverityCritical, false))

	mw := OutputMiddleware(engine)
	handler := mw(mockComplete("this is harmful output"))

	req := &llmtrace.Request{}
	_, err := handler(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for harmful output")
	}
	if !IsBlocked(err) {
		t.Errorf("expected blocked error, got: %v", err)
	}
}

func TestOutputMiddleware_RedactsOutput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("secret", []string{"password123"}, ActionRedact, SeverityHigh, false))

	mw := OutputMiddleware(engine)
	handler := mw(mockComplete("your password is password123"))

	req := &llmtrace.Request{}
	resp, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == "your password is password123" {
		t.Error("expected output to be redacted")
	}
}

func TestOutputMiddleware_PropagatesHandlerError(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("bad", []string{"x"}, ActionBlock, SeverityHigh, false))

	expectedErr := &blockedError{reason: "handler error"}
	mw := OutputMiddleware(engine)
	handler := mw(func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		return nil, expectedErr
	})

	_, err := handler(context.Background(), &llmtrace.Request{})
	if err != expectedErr {
		t.Errorf("expected handler error to propagate, got: %v", err)
	}
}

func TestOutputMiddleware_NilResponse(t *testing.T) {
	engine := New(DefaultConfig())

	mw := OutputMiddleware(engine)
	handler := mw(func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		return nil, nil
	})

	resp, err := handler(context.Background(), &llmtrace.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response to pass through")
	}
}
