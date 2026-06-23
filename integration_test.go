// Package llmtrace_test provides integration tests for the full llmtrace stack.
//
// These tests exercise the complete flow: Tracer → Middleware → Provider → OTel spans,
// verifying that all components work together correctly.
package llmtrace_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// --- Mock provider for integration testing ---

type integrationProvider struct {
	name         string
	defaultModel string
	completeFunc func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error)
	streamFunc   func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error)
}

func (p *integrationProvider) Name() string            { return p.name }
func (p *integrationProvider) DefaultModel() string    { return p.defaultModel }
func (p *integrationProvider) SupportsStreaming() bool { return true }

func (p *integrationProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	if p.completeFunc != nil {
		return p.completeFunc(ctx, req)
	}
	return &llmtrace.Response{
		ID:           "mock-123",
		Model:        req.Model,
		Content:      "Hello from mock provider",
		FinishReason: "stop",
		Usage: llmtrace.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
		Provider: p.name,
	}, nil
}

func (p *integrationProvider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	if p.streamFunc != nil {
		return p.streamFunc(ctx, req)
	}
	ch := make(chan llmtrace.StreamChunk)
	go func() {
		defer close(ch)
		ch <- llmtrace.StreamChunk{Content: "Hello "}
		ch <- llmtrace.StreamChunk{Content: "world"}
		ch <- llmtrace.StreamChunk{
			Content: "",
			Usage: &llmtrace.Usage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		}
	}()
	return ch, nil
}

// --- Test helpers ---

func setupTracer(t *testing.T) (*llmtrace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		exporter.Reset()
	})
	return llmtrace.NewTracer("integration-test",
		llmtrace.WithProvider("mock"),
		llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
	), exporter
}

func sampleRequest() *llmtrace.Request {
	return &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
		MaxTokens: llmtrace.IntPtr(100),
	}
}

func spanHasAttr(spans tracetest.SpanStubs, key string, expected any) bool {
	for _, s := range spans {
		for _, a := range s.Attributes {
			if string(a.Key) == key {
				switch v := expected.(type) {
				case string:
					return a.Value.AsString() == v
				case int:
					return a.Value.AsInt64() == int64(v)
				case float64:
					return a.Value.AsFloat64() == v
				case bool:
					return a.Value.AsBool() == v
				}
			}
		}
	}
	return false
}

// --- Integration Tests ---

func TestIntegration_BasicCompleteFlow(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	req := sampleRequest()
	resp, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response
	if resp.Content != "Hello from mock provider" {
		t.Errorf("content = %q, want %q", resp.Content, "Hello from mock provider")
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("total tokens = %d, want 30", resp.Usage.TotalTokens)
	}

	// Verify OTel span
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Status.Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", span.Status.Code)
	}
	if !spanHasAttr(spans, "gen_ai.system", "mock") {
		t.Error("missing gen_ai.system attribute")
	}
	if !spanHasAttr(spans, "gen_ai.request.model", "gpt-4o-mini") {
		t.Error("missing gen_ai.request.model attribute")
	}
	if !spanHasAttr(spans, "gen_ai.usage.input_tokens", 10) {
		t.Error("missing or wrong gen_ai.usage.input_tokens")
	}
}

func TestIntegration_StreamingFlow(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	req := sampleRequest()
	ch, err := tracer.ChatStream(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk.Content)
	}

	// Verify chunks
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0] != "Hello " || chunks[1] != "world" {
		t.Errorf("unexpected chunks: %v", chunks)
	}

	// Verify OTel span
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", spans[0].Status.Code)
	}
}

func TestIntegration_StreamingWithError(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		streamFunc: func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			ch := make(chan llmtrace.StreamChunk)
			go func() {
				defer close(ch)
				ch <- llmtrace.StreamChunk{Content: "partial"}
				ch <- llmtrace.StreamChunk{Error: errors.New("stream failed")}
			}()
			return ch, nil
		},
	}

	req := sampleRequest()
	ch, err := tracer.ChatStream(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotError bool
	for chunk := range ch {
		if chunk.Error != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected stream error, got none")
	}

	// Verify span recorded error
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", spans[0].Status.Code)
	}
}

func TestIntegration_CompleteError(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return nil, errors.New("API error")
		},
	}

	req := sampleRequest()
	_, err := tracer.Chat(context.Background(), req, provider)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", spans[0].Status.Code)
	}
}

func TestIntegration_MiddlewareChain(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	var callOrder []string
	mw1 := func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			callOrder = append(callOrder, "mw1-before")
			resp, err := next(ctx, req)
			callOrder = append(callOrder, "mw1-after")
			return resp, err
		}
	}
	mw2 := func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			callOrder = append(callOrder, "mw2-before")
			resp, err := next(ctx, req)
			callOrder = append(callOrder, "mw2-after")
			return resp, err
		}
	}

	req := sampleRequest()
	resp, err := tracer.Chat(context.Background(), req, provider,
		llmtrace.WithCallMiddleware(llmtrace.Chain(mw1, mw2)),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from mock provider" {
		t.Errorf("unexpected content: %q", resp.Content)
	}

	// Verify middleware execution order (outermost first)
	expected := []string{"mw1-before", "mw2-before", "mw2-after", "mw1-after"}
	if len(callOrder) != len(expected) {
		t.Fatalf("call order length = %d, want %d", len(callOrder), len(expected))
	}
	for i, v := range expected {
		if callOrder[i] != v {
			t.Errorf("callOrder[%d] = %q, want %q", i, callOrder[i], v)
		}
	}

	// Verify span captured
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
}

func TestIntegration_CompleteHook(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	var hookCalled bool
	var hookResp *llmtrace.Response
	hook := func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response, err error) {
		hookCalled = true
		hookResp = resp
	}

	req := sampleRequest()
	_, err := tracer.Chat(context.Background(), req, provider,
		llmtrace.WithCallMiddleware(llmtrace.WithCompleteHook(hook)),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hookCalled {
		t.Error("hook was not called")
	}
	if hookResp == nil {
		t.Fatal("hook received nil response")
	}
	if hookResp.Content != "Hello from mock provider" {
		t.Errorf("hook response content = %q", hookResp.Content)
	}
}

func TestIntegration_TimingMiddleware(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			time.Sleep(10 * time.Millisecond)
			return &llmtrace.Response{
				Model:    req.Model,
				Content:  "done",
				Provider: "mock",
			}, nil
		},
	}

	var capturedDuration float64
	timing := llmtrace.WithTiming(func(req *llmtrace.Request, durationMS float64) {
		capturedDuration = durationMS
	})

	req := sampleRequest()
	_, err := tracer.Chat(context.Background(), req, provider,
		llmtrace.WithCallMiddleware(timing),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedDuration < 5 {
		t.Errorf("captured duration = %fms, want >= 5ms", capturedDuration)
	}
}

func TestIntegration_RetryMiddleware(t *testing.T) {
	tracer, _ := setupTracer(t)

	var attempts int
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			attempts++
			if attempts < 3 {
				// Use RetryableError to mark as retryable
				return nil, llmtrace.NewRetryableError(errors.New("transient error"))
			}
			return &llmtrace.Response{
				Model:    req.Model,
				Content:  "success",
				Provider: "mock",
				Usage:    llmtrace.Usage{InputTokens: 5, OutputTokens: 10, TotalTokens: 15},
			}, nil
		},
	}

	req := sampleRequest()
	resp, err := tracer.Chat(context.Background(), req, provider,
		llmtrace.WithCallRetry(llmtrace.RetryConfig{
			MaxRetries:      3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     10 * time.Millisecond,
			Multiplier:      1.5,
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "success" {
		t.Errorf("content = %q, want %q", resp.Content, "success")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestIntegration_CostCalculation(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model:   "gpt-4o",
				Content: "response",
				Usage: llmtrace.Usage{
					InputTokens:  1000,
					OutputTokens: 500,
					TotalTokens:  1500,
				},
				Provider: "openai",
			}, nil
		},
	}

	req := sampleRequest()
	req.Model = "gpt-4o"
	_, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Verify cost attribute exists and is > 0
	var costFound bool
	for _, a := range spans[0].Attributes {
		if string(a.Key) == "gen_ai.usage.cost_usd" {
			costFound = true
			if a.Value.AsFloat64() <= 0 {
				t.Errorf("cost = %f, want > 0", a.Value.AsFloat64())
			}
		}
	}
	if !costFound {
		t.Error("missing gen_ai.usage.cost_usd attribute")
	}
}

func TestIntegration_ContextCancellation(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &llmtrace.Response{Content: "done"}, nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := sampleRequest()
	_, err := tracer.Chat(ctx, req, provider)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestIntegration_ConcurrentCalls(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	const numCalls = 20
	var wg sync.WaitGroup
	errs := make(chan error, numCalls)

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := sampleRequest()
			_, err := tracer.Chat(context.Background(), req, provider)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent call failed: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != numCalls {
		t.Errorf("expected %d spans, got %d", numCalls, len(spans))
	}
}

func TestIntegration_RequestAttributes(t *testing.T) {
	tracer, exporter := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	req := &llmtrace.Request{
		Model:       "gpt-4o",
		Messages:    []llmtrace.Message{{Role: llmtrace.RoleUser, Content: "test"}},
		Temperature: llmtrace.Float64Ptr(0.7),
		TopP:        llmtrace.Float64Ptr(0.9),
		MaxTokens:   llmtrace.IntPtr(200),
	}

	_, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrs := make(map[string]attribute.Value)
	for _, a := range spans[0].Attributes {
		attrs[string(a.Key)] = a.Value
	}

	checks := []struct {
		key      string
		expected any
	}{
		{"gen_ai.system", "mock"},
		{"gen_ai.operation.name", "chat"},
		{"gen_ai.request.model", "gpt-4o"},
		{"gen_ai.request.temperature", 0.7},
		{"gen_ai.request.top_p", 0.9},
		{"gen_ai.request.max_tokens", 200},
	}
	for _, c := range checks {
		val, ok := attrs[c.key]
		if !ok {
			t.Errorf("missing attribute %q", c.key)
			continue
		}
		switch expected := c.expected.(type) {
		case string:
			if val.AsString() != expected {
				t.Errorf("%s = %q, want %q", c.key, val.AsString(), expected)
			}
		case float64:
			if val.AsFloat64() != expected {
				t.Errorf("%s = %f, want %f", c.key, val.AsFloat64(), expected)
			}
		case int:
			if val.AsInt64() != int64(expected) {
				t.Errorf("%s = %d, want %d", c.key, val.AsInt64(), expected)
			}
		}
	}
}

func TestIntegration_MultipleProviders(t *testing.T) {
	tracer, exporter := setupTracer(t)

	providers := map[string]*integrationProvider{
		"openai":    {name: "openai"},
		"anthropic": {name: "anthropic"},
		"gemini":    {name: "gemini"},
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			exporter.Reset()
			req := sampleRequest()
			resp, err := tracer.Chat(context.Background(), req, provider)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Content != "Hello from mock provider" {
				t.Errorf("unexpected content: %q", resp.Content)
			}

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}
		})
	}
}

func TestIntegration_EmptyMessages(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{name: "mock"}

	req := &llmtrace.Request{
		Model:    "gpt-4o-mini",
		Messages: []llmtrace.Message{},
	}

	// Should still work (provider decides how to handle empty messages)
	resp, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestIntegration_SystemMessage(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			// Verify system message is passed through
			if len(req.Messages) < 2 {
				return nil, errors.New("expected at least 2 messages")
			}
			if req.Messages[0].Role != llmtrace.RoleSystem {
				return nil, fmt.Errorf("first message role = %q, want system", req.Messages[0].Role)
			}
			return &llmtrace.Response{
				Model:   req.Model,
				Content: "Acknowledged system prompt",
				Usage:   llmtrace.Usage{InputTokens: 50, OutputTokens: 10, TotalTokens: 60},
			}, nil
		},
	}

	req := &llmtrace.Request{
		Model: "gpt-4o",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are a helpful assistant."},
			{Role: llmtrace.RoleUser, Content: "Hello"},
		},
	}

	resp, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "system prompt") {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestIntegration_StreamWithMiddleware(t *testing.T) {
	tracer, _ := setupTracer(t)
	provider := &integrationProvider{name: "mock", defaultModel: "gpt-4o-mini"}

	// Note: Stream doesn't support middleware in current implementation,
	// but we can verify the basic flow works
	req := sampleRequest()
	ch, err := tracer.ChatStream(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
	}
}

func TestIntegration_LargePayload(t *testing.T) {
	tracer, _ := setupTracer(t)

	// Create a large message
	longContent := strings.Repeat("This is a test sentence. ", 100)
	provider := &integrationProvider{
		name: "mock",
		completeFunc: func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			return &llmtrace.Response{
				Model:   req.Model,
				Content: "Processed " + fmt.Sprintf("%d chars", len(req.Messages[0].Content)),
				Usage: llmtrace.Usage{
					InputTokens:  len(req.Messages[0].Content) / 4,
					OutputTokens: 10,
					TotalTokens:  len(req.Messages[0].Content)/4 + 10,
				},
			}, nil
		},
	}

	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: llmtrace.RoleUser, Content: longContent}},
	}

	resp, err := tracer.Chat(context.Background(), req, provider)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "chars") {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}
