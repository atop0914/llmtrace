package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

func ptrFloat64(v float64) *float64 { return &v }
func ptrInt(v int) *int             { return &v }

var testReq = &llmtrace.Request{
	Model:    "gpt-4o",
	Messages: []llmtrace.Message{{Role: "user", Content: "Hello"}},
}

func successResp(content string) *llmtrace.Response {
	return &llmtrace.Response{
		ID:           "resp-123",
		Model:        "gpt-4o",
		Content:      content,
		FinishReason: "stop",
		Usage:        llmtrace.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		Latency:      100 * time.Millisecond,
		Provider:     "openai",
	}
}

// --- MinLength ---

func TestMinLength_Pass(t *testing.T) {
	e := MinLength(5)
	r := e.Eval(context.Background(), testReq, successResp("Hello World"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestMinLength_Fail(t *testing.T) {
	e := MinLength(100)
	r := e.Eval(context.Background(), testReq, successResp("Hi"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
	if !strings.Contains(r.Message, "< 100") {
		t.Errorf("unexpected message: %s", r.Message)
	}
}

func TestMinLength_Exact(t *testing.T) {
	e := MinLength(5)
	r := e.Eval(context.Background(), testReq, successResp("12345"))
	if !r.Passed {
		t.Errorf("expected pass at exact boundary, got fail: %s", r.Message)
	}
}

// --- MaxLength ---

func TestMaxLength_Pass(t *testing.T) {
	e := MaxLength(100)
	r := e.Eval(context.Background(), testReq, successResp("Hello"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestMaxLength_Fail(t *testing.T) {
	e := MaxLength(5)
	r := e.Eval(context.Background(), testReq, successResp("Hello World"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

func TestMaxLength_Exact(t *testing.T) {
	e := MaxLength(5)
	r := e.Eval(context.Background(), testReq, successResp("12345"))
	if !r.Passed {
		t.Errorf("expected pass at exact boundary, got fail: %s", r.Message)
	}
}

// --- Contains ---

func TestContains_Pass(t *testing.T) {
	e := Contains("world")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestContains_Fail(t *testing.T) {
	e := Contains("xyz")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

func TestContains_CaseSensitive(t *testing.T) {
	e := Contains("World")
	r := e.Eval(context.Background(), testReq, successResp("hello world"))
	if r.Passed {
		t.Error("expected fail for case mismatch")
	}
}

// --- ContainsAny ---

func TestContainsAny_Pass(t *testing.T) {
	e := ContainsAny("foo", "bar", "world")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestContainsAny_Fail(t *testing.T) {
	e := ContainsAny("foo", "bar", "baz")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

// --- NotContains ---

func TestNotContains_Pass(t *testing.T) {
	e := NotContains("sensitive")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestNotContains_Fail(t *testing.T) {
	e := NotContains("world")
	r := e.Eval(context.Background(), testReq, successResp("Hello world!"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

// --- ValidJSON ---

func TestValidJSON_Pass(t *testing.T) {
	e := ValidJSON()
	r := e.Eval(context.Background(), testReq, successResp(`{"name": "test", "value": 42}`))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestValidJSON_PassArray(t *testing.T) {
	e := ValidJSON()
	r := e.Eval(context.Background(), testReq, successResp(`[1, 2, 3]`))
	if !r.Passed {
		t.Errorf("expected pass for JSON array, got fail: %s", r.Message)
	}
}

func TestValidJSON_Fail(t *testing.T) {
	e := ValidJSON()
	r := e.Eval(context.Background(), testReq, successResp("not json at all"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
	if !strings.Contains(r.Message, "not valid JSON") {
		t.Errorf("unexpected message: %s", r.Message)
	}
}

// --- FinishReason ---

func TestFinishReason_Pass(t *testing.T) {
	e := FinishReason("stop", "length")
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestFinishReason_Fail(t *testing.T) {
	e := FinishReason("length")
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
	if !strings.Contains(r.Message, "expected one of") {
		t.Errorf("unexpected message: %s", r.Message)
	}
}

// --- RegexMatch ---

func TestRegexMatch_Pass(t *testing.T) {
	e := RegexMatch(`^\d{3}-\d{4}$`)
	r := e.Eval(context.Background(), testReq, successResp("555-1234"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestRegexMatch_Fail(t *testing.T) {
	e := RegexMatch(`^\d{3}-\d{4}$`)
	r := e.Eval(context.Background(), testReq, successResp("not a number"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

func TestRegexMatch_InvalidPattern(t *testing.T) {
	e := RegexMatch(`[invalid`)
	r := e.Eval(context.Background(), testReq, successResp("test"))
	if r.Passed {
		t.Error("expected fail for invalid pattern")
	}
	if !strings.Contains(r.Message, "invalid regex") {
		t.Errorf("unexpected message: %s", r.Message)
	}
}

// --- NonEmpty ---

func TestNonEmpty_Pass(t *testing.T) {
	e := NonEmpty()
	r := e.Eval(context.Background(), testReq, successResp("Hello"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestNonEmpty_Fail(t *testing.T) {
	e := NonEmpty()
	r := e.Eval(context.Background(), testReq, successResp(""))
	if r.Passed {
		t.Error("expected fail for empty content")
	}
}

func TestNonEmpty_WhitespaceOnly(t *testing.T) {
	e := NonEmpty()
	r := e.Eval(context.Background(), testReq, successResp("   \n\t  "))
	if r.Passed {
		t.Error("expected fail for whitespace-only content")
	}
}

// --- TokenLimit ---

func TestTokenLimit_Pass(t *testing.T) {
	e := TokenLimit(100)
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestTokenLimit_Fail(t *testing.T) {
	e := TokenLimit(10)
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

// --- ResponseID ---

func TestResponseID_Pass(t *testing.T) {
	e := ResponseID()
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestResponseID_Fail(t *testing.T) {
	e := ResponseID()
	resp := successResp("text")
	resp.ID = ""
	r := e.Eval(context.Background(), testReq, resp)
	if r.Passed {
		t.Error("expected fail for empty ID")
	}
}

// --- MaxLatency ---

func TestMaxLatency_Pass(t *testing.T) {
	e := MaxLatency(500 * time.Millisecond)
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestMaxLatency_Fail(t *testing.T) {
	e := MaxLatency(10 * time.Millisecond)
	r := e.Eval(context.Background(), testReq, successResp("text"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

// --- Custom ---

func TestCustom_Pass(t *testing.T) {
	e := Custom("starts_with_hello", func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) (bool, string) {
		ok := strings.HasPrefix(resp.Content, "Hello")
		return ok, "content starts with Hello"
	})
	r := e.Eval(context.Background(), testReq, successResp("Hello World"))
	if !r.Passed {
		t.Errorf("expected pass, got fail: %s", r.Message)
	}
}

func TestCustom_Fail(t *testing.T) {
	e := Custom("starts_with_hello", func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) (bool, string) {
		ok := strings.HasPrefix(resp.Content, "Hello")
		return ok, "content does not start with Hello"
	})
	r := e.Eval(context.Background(), testReq, successResp("Goodbye"))
	if r.Passed {
		t.Error("expected fail, got pass")
	}
}

// --- Suite ---

func TestSuite_AllPass(t *testing.T) {
	suite := NewSuite("test",
		MinLength(1),
		MaxLength(100),
		NonEmpty(),
		Contains("Hello"),
	)

	result := suite.Run(context.Background(), testReq, successResp("Hello World"))
	if !result.Passed {
		t.Error("expected suite to pass")
	}
	if result.Total != 4 {
		t.Errorf("expected 4 total, got %d", result.Total)
	}
	if result.PassCount != 4 {
		t.Errorf("expected 4 passed, got %d", result.PassCount)
	}
	if result.FailCount != 0 {
		t.Errorf("expected 0 failed, got %d", result.FailCount)
	}
}

func TestSuite_SomeFail(t *testing.T) {
	suite := NewSuite("test",
		MinLength(1),
		MaxLength(5),
		Contains("Hello"),
	)

	result := suite.Run(context.Background(), testReq, successResp("Hello World, this is a long response"))
	if result.Passed {
		t.Error("expected suite to fail")
	}
	if result.PassCount != 2 {
		t.Errorf("expected 2 passed, got %d", result.PassCount)
	}
	if result.FailCount != 1 {
		t.Errorf("expected 1 failed, got %d", result.FailCount)
	}
}

func TestSuite_NilResponse(t *testing.T) {
	suite := NewSuite("test", NonEmpty())
	result := suite.Run(context.Background(), testReq, nil)
	if result.Passed {
		t.Error("expected suite to fail with nil response")
	}
	if result.FailCount != 1 {
		t.Errorf("expected 1 failed, got %d", result.FailCount)
	}
}

func TestSuite_Empty(t *testing.T) {
	suite := NewSuite("empty")
	result := suite.Run(context.Background(), testReq, successResp("text"))
	if !result.Passed {
		t.Error("empty suite should pass")
	}
	if result.Total != 0 {
		t.Errorf("expected 0 total, got %d", result.Total)
	}
}

func TestSuite_Add(t *testing.T) {
	suite := NewSuite("dynamic")
	suite.Add(MinLength(1), MaxLength(100))
	if suite.Name() != "dynamic" {
		t.Errorf("expected name 'dynamic', got %q", suite.Name())
	}

	result := suite.Run(context.Background(), testReq, successResp("Hello"))
	if result.Total != 2 {
		t.Errorf("expected 2 evaluators, got %d", result.Total)
	}
}

func TestSuite_Duration(t *testing.T) {
	suite := NewSuite("timed", NonEmpty())
	result := suite.Run(context.Background(), testReq, successResp("Hello"))
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
	for _, r := range result.Results {
		if r.Duration < 0 {
			t.Error("expected non-negative individual duration")
		}
	}
}

// --- Validate ---

func TestValidate_Pass(t *testing.T) {
	suite := NewSuite("test", NonEmpty(), MinLength(1))
	result, err := suite.Validate(context.Background(), testReq, successResp("Hello"))
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass")
	}
}

func TestValidate_Fail(t *testing.T) {
	suite := NewSuite("test", MinLength(100), Contains("xyz"))
	_, err := suite.Validate(context.Background(), testReq, successResp("Hi"))
	if err == nil {
		t.Error("expected error")
	}

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Error("expected ValidationError type")
	}
	if ve.SuiteName != "test" {
		t.Errorf("expected suite name 'test', got %q", ve.SuiteName)
	}
	if len(ve.Failed) != 2 {
		t.Errorf("expected 2 failures, got %d", len(ve.Failed))
	}
}

func TestValidate_NilResponse(t *testing.T) {
	suite := NewSuite("test", NonEmpty())
	_, err := suite.Validate(context.Background(), testReq, nil)
	if err == nil {
		t.Error("expected error for nil response")
	}
}

// --- Middleware ---

func TestMiddleware_PassesThroughResponse(t *testing.T) {
	suite := NewSuite("mw-test", NonEmpty())
	mw := suite.Middleware()

	called := false
	inner := func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
		called = true
		return successResp("Hello"), nil
	}

	wrapped := mw(inner)
	resp, err := wrapped(context.Background(), testReq)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("inner function was not called")
	}
	if resp.Content != "Hello" {
		t.Errorf("expected 'Hello', got %q", resp.Content)
	}
}

func TestMiddleware_PassesThroughError(t *testing.T) {
	suite := NewSuite("mw-test", NonEmpty())
	mw := suite.Middleware()

	expectedErr := errors.New("api error")
	inner := func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
		return nil, expectedErr
	}

	wrapped := mw(inner)
	_, err := wrapped(context.Background(), testReq)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error to pass through, got: %v", err)
	}
}

func TestMiddleware_DoesNotAlterResponse(t *testing.T) {
	// Even if evals fail, middleware should not modify the response
	suite := NewSuite("mw-test", MinLength(1000)) // will fail
	mw := suite.Middleware()

	inner := func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
		return successResp("short"), nil
	}

	wrapped := mw(inner)
	resp, err := wrapped(context.Background(), testReq)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Content != "short" {
		t.Errorf("response should not be modified, got %q", resp.Content)
	}
}

// --- EvalFunc adapter ---

func TestEvalFunc(t *testing.T) {
	ef := EvalFunc{
		FnName: "test-fn",
		Fn: func(_ context.Context, _ *llmtrace.Request, _ *llmtrace.Response) Result {
			return Result{Passed: true, Message: "ok"}
		},
	}
	if ef.Name() != "test-fn" {
		t.Errorf("expected name 'test-fn', got %q", ef.Name())
	}
	r := ef.Eval(context.Background(), testReq, successResp("x"))
	if !r.Passed {
		t.Error("expected pass")
	}
}

// --- Benchmark ---

func BenchmarkSuite_Run(b *testing.B) {
	suite := NewSuite("bench",
		MinLength(1),
		MaxLength(4000),
		NonEmpty(),
		Contains("Hello"),
		ValidJSON(),
		FinishReason("stop"),
		RegexMatch(`Hello`),
		ResponseID(),
	)

	ctx := context.Background()
	resp := successResp(`{"greeting": "Hello World"}`)
	resp.FinishReason = "stop"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = suite.Run(ctx, testReq, resp)
	}
}

func BenchmarkMinLength(b *testing.B) {
	e := MinLength(10)
	ctx := context.Background()
	resp := successResp("Hello World, this is a test response")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Eval(ctx, testReq, resp)
	}
}

func BenchmarkValidJSON(b *testing.B) {
	e := ValidJSON()
	ctx := context.Background()
	resp := successResp(`{"name": "test", "value": 42, "nested": {"key": "val"}}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Eval(ctx, testReq, resp)
	}
}
