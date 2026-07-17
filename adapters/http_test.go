package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// setupTracer creates an in-memory OTel tracer for testing.
func setupTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	origTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
	})
	return exporter
}

func TestMiddleware_GeneratesRequestID(t *testing.T) {
	exporter := setupTracer(t)
	_ = exporter

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should generate a request ID
	reqID := rec.Header().Get(HeaderRequestID)
	if reqID == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}
	if len(reqID) != 32 {
		t.Errorf("expected request ID length 32, got %d", len(reqID))
	}
}

func TestMiddleware_PreservesIncomingRequestID(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	req.Header.Set(HeaderRequestID, "my-custom-id-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	reqID := rec.Header().Get(HeaderRequestID)
	if reqID != "my-custom-id-123" {
		t.Errorf("expected preserved request ID 'my-custom-id-123', got %q", reqID)
	}
}

func TestMiddleware_SetsRequestDataInContext(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := RequestDataFromContext(r.Context())
		if data == nil {
			t.Error("expected RequestData in context")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if data.RequestID == "" {
			t.Error("expected non-empty RequestID")
		}
		if data.StartedAt.IsZero() {
			t.Error("expected non-zero StartedAt")
		}

		// Test SetProvider and SetTokensUsed
		SetProvider(r.Context(), "openai")
		SetModel(r.Context(), "gpt-4")
		SetTokensUsed(r.Context(), 150)

		if data.Provider != "openai" {
			t.Errorf("expected provider 'openai', got %q", data.Provider)
		}
		if data.Model != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got %q", data.Model)
		}
		if data.TokensUsed != 150 {
			t.Errorf("expected 150 tokens, got %d", data.TokensUsed)
		}

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_CapturesStatusCode(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))

	req := httptest.NewRequest("POST", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
}

func TestMiddleware_CreatesOTelSpan(t *testing.T) {
	exporter := setupTracer(t)

	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	span := spans[0]
	if span.Name != "HTTP POST /v1/chat/completions" {
		t.Errorf("unexpected span name: %s", span.Name)
	}

	// Check attributes
	attrs := make(map[string]string)
	for _, a := range span.Attributes {
		attrs[string(a.Key)] = a.Value.AsString()
	}

	if attrs["http.method"] != "POST" {
		t.Errorf("expected http.method=POST, got %q", attrs["http.method"])
	}
	if attrs["http.target"] != "/v1/chat/completions" {
		t.Errorf("expected http.target=/v1/chat/completions, got %q", attrs["http.target"])
	}
	if attrs["http.user_agent"] != "test-agent/1.0" {
		t.Errorf("expected http.user_agent=test-agent/1.0, got %q", attrs["http.user_agent"])
	}
}

func TestMiddleware_PanicRecovery(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "internal server error") {
		t.Errorf("expected error message in body, got %q", body)
	}
	if !strings.Contains(body, "request_id") {
		t.Errorf("expected request_id in error body, got %q", body)
	}
}

func TestMiddleware_PanicRecoveryDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RecoverFromPanic = false

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("should not be recovered")
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate when recovery disabled")
		}
	}()

	handler.ServeHTTP(rec, req)
}

func TestMiddleware_ResponseHeaders(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := RequestDataFromContext(r.Context())
		data.Provider = "anthropic"
		data.TokensUsed = 256
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get(HeaderLLMProvider) != "anthropic" {
		t.Errorf("expected X-LLM-Provider=anthropic, got %q", rec.Header().Get(HeaderLLMProvider))
	}
	if rec.Header().Get(HeaderTokensUsed) != "256" {
		t.Errorf("expected X-Tokens-Used=256, got %q", rec.Header().Get(HeaderTokensUsed))
	}
	if rec.Header().Get(HeaderResponseTime) == "" {
		t.Error("expected X-Response-Time-Ms header to be set")
	}
}

func TestMiddleware_ResponseHeadersDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddResponseHeaders = false

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get(HeaderResponseTime) != "" {
		t.Error("expected no X-Response-Time-Ms header when disabled")
	}
}

func TestMiddleware_NoRequestIDGeneration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GenerateRequestID = false

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get(HeaderRequestID) != "" {
		t.Error("expected no X-Request-ID when generation disabled")
	}
}

func TestMiddleware_CustomSpanName(t *testing.T) {
	exporter := setupTracer(t)

	cfg := DefaultConfig()
	cfg.SpanNameFunc = func(r *http.Request) string {
		return "CustomSpan " + r.URL.Path
	}

	handler := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	if spans[0].Name != "CustomSpan /api/chat" {
		t.Errorf("expected 'CustomSpan /api/chat', got %q", spans[0].Name)
	}
}

func TestRequestDataFromContext_Nil(t *testing.T) {
	data := RequestDataFromContext(context.Background())
	if data != nil {
		t.Error("expected nil for empty context")
	}
}

func TestSetProvider_NoContext(t *testing.T) {
	// Should not panic when called on context without data
	SetProvider(context.Background(), "openai")
	SetModel(context.Background(), "gpt-4")
	SetTokensUsed(context.Background(), 100)
}

func TestMiddleware_ChainedHandlers(t *testing.T) {
	handler := Middleware(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inner handler can read request data
		data := RequestDataFromContext(r.Context())
		if data == nil {
			t.Error("expected data in inner handler")
		}

		// Simulate setting provider after routing
		SetProvider(r.Context(), "gemini")
		SetTokensUsed(r.Context(), 42)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))

	req := httptest.NewRequest("POST", "/v1/completions", strings.NewReader(`{"model":"gemini-pro"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get(HeaderLLMProvider) != "gemini" {
		t.Errorf("expected X-LLM-Provider=gemini, got %q", rec.Header().Get(HeaderLLMProvider))
	}
	if rec.Header().Get(HeaderTokensUsed) != "42" {
		t.Errorf("expected X-Tokens-Used=42, got %q", rec.Header().Get(HeaderTokensUsed))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TracerName != TracerName {
		t.Errorf("expected TracerName=%q, got %q", TracerName, cfg.TracerName)
	}
	if !cfg.GenerateRequestID {
		t.Error("expected GenerateRequestID=true")
	}
	if !cfg.AddResponseHeaders {
		t.Error("expected AddResponseHeaders=true")
	}
	if !cfg.RecoverFromPanic {
		t.Error("expected RecoverFromPanic=true")
	}
}

func TestGenerateRequestID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateRequestID()
		if ids[id] {
			t.Fatalf("duplicate request ID: %s", id)
		}
		ids[id] = true
	}
}
