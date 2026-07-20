package healthcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", cfg.Timeout)
	}
	if !cfg.AggregateStatus {
		t.Error("expected AggregateStatus=true")
	}
}

func TestNew_ZeroConfig(t *testing.T) {
	hc := New(Config{})
	if hc.cfg.Timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", hc.cfg.Timeout)
	}
}

func TestLiveHandler_AlwaysUp(t *testing.T) {
	hc := New(DefaultConfig())

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	hc.LiveHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusUp {
		t.Errorf("expected status 'up', got %q", resp.Status)
	}
}

func TestReadyHandler_NoChecks(t *testing.T) {
	hc := New(DefaultConfig())

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusUp {
		t.Errorf("expected status 'up', got %q", resp.Status)
	}
	if resp.Duration == "" {
		t.Error("expected non-empty duration")
	}
}

func TestReadyHandler_AllPass(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("database", func(ctx context.Context) error {
		return nil
	})
	hc.AddReadinessCheck("cache", func(ctx context.Context) error {
		return nil
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusUp {
		t.Errorf("expected status 'up', got %q", resp.Status)
	}
	if resp.Checks["database"].Status != StatusUp {
		t.Errorf("expected database check 'up', got %q", resp.Checks["database"].Status)
	}
	if resp.Checks["cache"].Status != StatusUp {
		t.Errorf("expected cache check 'up', got %q", resp.Checks["cache"].Status)
	}
}

func TestReadyHandler_OneFails(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("database", func(ctx context.Context) error {
		return nil
	})
	hc.AddReadinessCheck("cache", func(ctx context.Context) error {
		return errors.New("connection refused")
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusDown {
		t.Errorf("expected status 'down', got %q", resp.Status)
	}
	if resp.Checks["database"].Status != StatusUp {
		t.Errorf("expected database check 'up', got %q", resp.Checks["database"].Status)
	}
	if resp.Checks["cache"].Status != StatusDown {
		t.Errorf("expected cache check 'down', got %q", resp.Checks["cache"].Status)
	}
	if resp.Checks["cache"].Message != "connection refused" {
		t.Errorf("expected cache message 'connection refused', got %q", resp.Checks["cache"].Message)
	}
}

func TestReadyHandler_AllFail(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("db", func(ctx context.Context) error {
		return errors.New("db down")
	})
	hc.AddReadinessCheck("queue", func(ctx context.Context) error {
		return errors.New("queue unreachable")
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusDown {
		t.Errorf("expected status 'down', got %q", resp.Status)
	}
	if len(resp.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(resp.Checks))
	}
}

func TestReadyHandler_Timeout(t *testing.T) {
	cfg := Config{
		Timeout:         50 * time.Millisecond,
		AggregateStatus: true,
	}
	hc := New(cfg)
	hc.AddReadinessCheck("slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Checks["slow"].Status != StatusDown {
		t.Errorf("expected slow check 'down', got %q", resp.Checks["slow"].Status)
	}
	if !strings.Contains(resp.Checks["slow"].Message, "context") {
		t.Errorf("expected context error message, got %q", resp.Checks["slow"].Message)
	}
}

func TestReadyHandler_CheckUsesRequestContext(t *testing.T) {
	hc := New(DefaultConfig())

	var ctxCancelled atomic.Bool
	hc.AddReadinessCheck("ctx-check", func(ctx context.Context) error {
		<-ctx.Done()
		ctxCancelled.Store(true)
		return ctx.Err()
	})

	// Create a request with a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := httptest.NewRequest("GET", "/readyz", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !ctxCancelled.Load() {
		t.Error("expected context cancellation to propagate to check")
	}
}

func TestReadyHandler_AggregateStatusDisabled(t *testing.T) {
	cfg := Config{
		Timeout:         5 * time.Second,
		AggregateStatus: false,
	}
	hc := New(cfg)
	hc.AddReadinessCheck("failing", func(ctx context.Context) error {
		return errors.New("broken")
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	// With AggregateStatus=false, top-level status is always "up"
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with aggregate disabled, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusUp {
		t.Errorf("expected top-level 'up' with aggregate disabled, got %q", resp.Status)
	}
	// Individual check still shows down
	if resp.Checks["failing"].Status != StatusDown {
		t.Errorf("expected individual check 'down', got %q", resp.Checks["failing"].Status)
	}
}

func TestAddReadinessCheck_Replace(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("db", func(ctx context.Context) error {
		return errors.New("old")
	})
	hc.AddReadinessCheck("db", func(ctx context.Context) error {
		return nil
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after replacing check, got %d", rec.Code)
	}
}

func TestRemoveCheck(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("db", func(ctx context.Context) error {
		return errors.New("fail")
	})
	hc.RemoveCheck("db")

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after removing check, got %d", rec.Code)
	}
}

func TestRemoveCheck_NonExistent(t *testing.T) {
	hc := New(DefaultConfig())
	// Should not panic
	hc.RemoveCheck("nonexistent")
}

func TestCheckNames(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("db", func(ctx context.Context) error { return nil })
	hc.AddReadinessCheck("cache", func(ctx context.Context) error { return nil })
	hc.AddReadinessCheck("queue", func(ctx context.Context) error { return nil })

	names := hc.CheckNames()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"db", "cache", "queue"} {
		if !nameSet[expected] {
			t.Errorf("expected name %q in check names", expected)
		}
	}
}

func TestHandler_Routes(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("ok", func(ctx context.Context) error { return nil })

	handler := hc.Handler()

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusOK},
		{"/unknown", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s: expected %d, got %d", tt.path, tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestHandler_JSONContentType(t *testing.T) {
	hc := New(DefaultConfig())
	handler := hc.Handler()

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content type, got %q", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options: nosniff")
	}
}

func TestUptime(t *testing.T) {
	hc := New(DefaultConfig())
	time.Sleep(10 * time.Millisecond)

	uptime := hc.Uptime()
	if uptime < 10*time.Millisecond {
		t.Errorf("expected uptime >= 10ms, got %v", uptime)
	}
}

func TestLiveHandler_JSONFormat(t *testing.T) {
	hc := New(DefaultConfig())

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	hc.LiveHandler(rec, req)

	body := rec.Body.String()
	// Verify it's valid JSON
	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
}

func TestReadyHandler_Concurrent(t *testing.T) {
	hc := New(DefaultConfig())
	hc.AddReadinessCheck("db", func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	hc.AddReadinessCheck("cache", func(ctx context.Context) error {
		return nil
	})

	handler := hc.Handler()

	// Run multiple concurrent requests
	done := make(chan int, 20)
	for i := 0; i < 20; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/readyz", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			done <- rec.Code
		}()
	}

	for i := 0; i < 20; i++ {
		code := <-done
		if code != http.StatusOK {
			t.Errorf("concurrent request: expected 200, got %d", code)
		}
	}
}

func TestReadyHandler_ManyChecks(t *testing.T) {
	hc := New(DefaultConfig())

	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("check-%03d", i)
		hc.AddReadinessCheck(name, func(ctx context.Context) error {
			return nil
		})
	}

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	hc.ReadyHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Checks) != 50 {
		t.Errorf("expected 50 check results, got %d", len(resp.Checks))
	}
}
