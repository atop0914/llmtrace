package correlation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HeaderName != HeaderRequestID {
		t.Errorf("HeaderName = %q, want %q", cfg.HeaderName, HeaderRequestID)
	}
	if !cfg.SetResponseHeader {
		t.Error("SetResponseHeader should default to true")
	}
	if cfg.GenerateID == nil {
		t.Error("GenerateID should not be nil")
	}
	if len(cfg.ExtraHeaders) != 2 {
		t.Errorf("ExtraHeaders count = %d, want 2", len(cfg.ExtraHeaders))
	}
}

func TestNew_DefaultConfig(t *testing.T) {
	c := New(Config{})
	if c.cfg.HeaderName != HeaderRequestID {
		t.Errorf("HeaderName = %q, want %q", c.cfg.HeaderName, HeaderRequestID)
	}
	if c.cfg.GenerateID == nil {
		t.Error("GenerateID should be set by New()")
	}
}

func TestMiddleware_GenerateID(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Error("expected correlation ID to be generated")
	}
	if len(capturedID) != 32 {
		t.Errorf("ID length = %d, want 32", len(capturedID))
	}

	// Check response header
	respID := rec.Header().Get(HeaderRequestID)
	if respID != capturedID {
		t.Errorf("response header ID = %q, want %q", respID, capturedID)
	}
}

func TestMiddleware_ExtractFromPrimaryHeader(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "my-custom-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "my-custom-id" {
		t.Errorf("ID = %q, want %q", capturedID, "my-custom-id")
	}
}

func TestMiddleware_ExtractFromExtraHeaders(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	tests := []struct {
		name   string
		header string
		value  string
	}{
		{"X-Correlation-ID", HeaderCorrelationID, "corr-123"},
		{"X-Trace-ID", HeaderTraceID, "trace-456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set(tt.header, tt.value)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if capturedID != tt.value {
				t.Errorf("ID = %q, want %q", capturedID, tt.value)
			}
		})
	}
}

func TestMiddleware_PrimaryHeaderTakesPrecedence(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "primary")
	req.Header.Set(HeaderCorrelationID, "secondary")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "primary" {
		t.Errorf("ID = %q, want %q", capturedID, "primary")
	}
}

func TestMiddleware_DisableGeneration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DisableGeneration = true
	corr := New(cfg)

	var capturedID string
	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "" {
		t.Errorf("ID = %q, want empty (generation disabled)", capturedID)
	}
}

func TestMiddleware_DisableGeneration_WithExistingID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DisableGeneration = true
	corr := New(cfg)

	var capturedID string
	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "existing-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "existing-id" {
		t.Errorf("ID = %q, want %q", capturedID, "existing-id")
	}
}

func TestMiddleware_CustomHeaderName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeaderName = "X-Custom-ID"
	corr := New(cfg)

	var capturedID string
	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-ID", "custom-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "custom-123" {
		t.Errorf("ID = %q, want %q", capturedID, "custom-123")
	}

	// Response should have the custom header
	respID := rec.Header().Get("X-Custom-ID")
	if respID != "custom-123" {
		t.Errorf("response header = %q, want %q", respID, "custom-123")
	}
}

func TestMiddleware_TrimWhitespace(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "  trimmed-id  ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "trimmed-id" {
		t.Errorf("ID = %q, want %q", capturedID, "trimmed-id")
	}
}

func TestMiddleware_PropagateHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PropagateHeaders = []string{"X-Tenant-ID", "X-User-ID"}
	corr := New(cfg)

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Tenant-ID", "tenant-abc")
	req.Header.Set("X-User-ID", "user-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Tenant-ID") != "tenant-abc" {
		t.Errorf("X-Tenant-ID not propagated")
	}
	if rec.Header().Get("X-User-ID") != "user-xyz" {
		t.Errorf("X-User-ID not propagated")
	}
}

func TestFuncMiddleware(t *testing.T) {
	corr := New(DefaultConfig())
	var capturedID string

	handler := corr.FuncMiddleware(func(w http.ResponseWriter, r *http.Request) {
		capturedID = IDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HeaderRequestID, "func-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "func-id" {
		t.Errorf("ID = %q, want %q", capturedID, "func-id")
	}
}

func TestClientMiddleware(t *testing.T) {
	corr := New(DefaultConfig())
	var receivedID string

	// Backend server that captures the correlation ID
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedID = r.Header.Get(HeaderRequestID)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Client with correlation middleware
	client := &http.Client{
		Transport: corr.ClientMiddleware(http.DefaultTransport),
	}

	// Create request with correlation ID in context
	req, _ := http.NewRequest("GET", backend.URL, nil)
	ctx := ContextWithID(req.Context(), "test-correlation-id")
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedID != "test-correlation-id" {
		t.Errorf("backend received ID = %q, want %q", receivedID, "test-correlation-id")
	}
}

func TestClientMiddleware_NoIDInContext(t *testing.T) {
	corr := New(DefaultConfig())
	var receivedHeader string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get(HeaderRequestID)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	client := &http.Client{
		Transport: corr.ClientMiddleware(http.DefaultTransport),
	}

	req, _ := http.NewRequest("GET", backend.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedHeader != "" {
		t.Errorf("expected no header, got %q", receivedHeader)
	}
}

func TestContextWithID_And_IDFromContext(t *testing.T) {
	ctx := context.Background()

	// No ID initially
	if id := IDFromContext(ctx); id != "" {
		t.Errorf("expected empty ID, got %q", id)
	}

	// Store and retrieve
	ctx = ContextWithID(ctx, "test-id")
	if id := IDFromContext(ctx); id != "test-id" {
		t.Errorf("ID = %q, want %q", id, "test-id")
	}

	// Overwrite
	ctx = ContextWithID(ctx, "new-id")
	if id := IDFromContext(ctx); id != "new-id" {
		t.Errorf("ID = %q, want %q", id, "new-id")
	}
}

func TestGenerateHexID(t *testing.T) {
	id := GenerateHexID()
	if len(id) != 32 {
		t.Errorf("ID length = %d, want 32", len(id))
	}
	// Check it's valid hex
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex char: %c", c)
		}
	}

	// Two IDs should be different
	id2 := GenerateHexID()
	if id == id2 {
		t.Error("two generated IDs should not be equal")
	}
}

func TestGenerateShortID(t *testing.T) {
	id := GenerateShortID()
	if len(id) != 16 {
		t.Errorf("ID length = %d, want 16", len(id))
	}
}

func TestGeneratePrefixedID(t *testing.T) {
	gen := GeneratePrefixedID("req-")
	id := gen()
	if !strings.HasPrefix(id, "req-") {
		t.Errorf("ID = %q, should have prefix 'req-'", id)
	}
	if len(id) != 36 { // "req-" (4) + 32 hex
		t.Errorf("ID length = %d, want 36", len(id))
	}
}

func TestMiddleware_PreservesExistingResponseHeaders(t *testing.T) {
	corr := New(DefaultConfig())

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Custom") != "value" {
		t.Error("existing response headers should be preserved")
	}
	if rec.Header().Get(HeaderRequestID) == "" {
		t.Error("correlation ID should be in response")
	}
}

func TestMiddleware_StatusCodePreserved(t *testing.T) {
	corr := New(DefaultConfig())

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMiddleware_DoesNotOverrideEmptyID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DisableGeneration = true
	corr := New(cfg)

	handler := corr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// No ID should be in response when generation is disabled and no header present
	if id := rec.Header().Get(HeaderRequestID); id != "" {
		t.Errorf("expected no response header, got %q", id)
	}
}

func TestMiddleware_NilExtraHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExtraHeaders = nil // explicitly nil
	corr := New(cfg)

	// New() should set defaults
	if len(corr.cfg.ExtraHeaders) == 0 {
		t.Error("New() should set default ExtraHeaders when nil")
	}
}
