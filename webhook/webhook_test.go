package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAlerter_Defaults(t *testing.T) {
	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: "https://example.com/hook"}},
	})

	if a.EndpointCount() != 1 {
		t.Fatalf("expected 1 endpoint, got %d", a.EndpointCount())
	}
	if a.retries != 3 {
		t.Errorf("expected 3 retries, got %d", a.retries)
	}
	if a.initial != time.Second {
		t.Errorf("expected 1s initial interval, got %v", a.initial)
	}
	if a.maxInt != 30*time.Second {
		t.Errorf("expected 30s max interval, got %v", a.maxInt)
	}
	if a.multi != 2.0 {
		t.Errorf("expected 2.0 multiplier, got %f", a.multi)
	}
}

func TestNewAlerter_CustomConfig(t *testing.T) {
	a := NewAlerter(Config{
		Endpoints:       []Endpoint{{URL: "https://example.com/hook"}},
		MaxRetries:      5,
		InitialInterval: 2 * time.Second,
		MaxInterval:     60 * time.Second,
		Multiplier:      3.0,
	})

	if a.retries != 5 {
		t.Errorf("expected 5 retries, got %d", a.retries)
	}
	if a.initial != 2*time.Second {
		t.Errorf("expected 2s initial, got %v", a.initial)
	}
	if a.maxInt != 60*time.Second {
		t.Errorf("expected 60s max, got %v", a.maxInt)
	}
	if a.multi != 3.0 {
		t.Errorf("expected 3.0 multiplier, got %f", a.multi)
	}
}

func TestSend_EventSerialization(t *testing.T) {
	var received Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL}},
	})

	data := map[string]any{
		"name":       "api-budget",
		"percentage": 95.5,
		"spent":      955.0,
		"limit":      1000.0,
	}

	count := a.Send(context.Background(), EventBudgetAlert, data)
	if count != 1 {
		t.Fatalf("expected 1 success, got %d", count)
	}

	if received.Type != EventBudgetAlert {
		t.Errorf("expected type %q, got %q", EventBudgetAlert, received.Type)
	}
	if received.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if received.Source != "budget" {
		t.Errorf("expected source %q, got %q", "budget", received.Source)
	}
}

func TestSend_BudgetEventSeverity(t *testing.T) {
	tests := []struct {
		name     string
		pct      float64
		severity string
	}{
		{"exceeded", 105.0, "critical"},
		{"at-limit", 100.0, "critical"},
		{"critical", 95.0, "warning"},
		{"warning", 90.0, "warning"},
		{"info", 50.0, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received Event
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &received)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})
			a.Send(context.Background(), EventBudgetAlert, map[string]any{
				"name":       "test",
				"percentage": tt.pct,
			})

			if received.Severity != tt.severity {
				t.Errorf("expected severity %q, got %q", tt.severity, received.Severity)
			}
		})
	}
}

func TestSend_CircuitBreakerEvent(t *testing.T) {
	tests := []struct {
		state    string
		severity string
		contains string
	}{
		{"open", "critical", "opened"},
		{"half-open", "warning", "half-open"},
		{"closed", "info", "recovered"},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			var received Event
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &received)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})
			a.Send(context.Background(), EventCircuitBreaker, map[string]any{
				"name":  "openai-breaker",
				"state": tt.state,
			})

			if received.Severity != tt.severity {
				t.Errorf("expected severity %q, got %q", tt.severity, received.Severity)
			}
			if received.Source != "circuit_breaker" {
				t.Errorf("expected source %q, got %q", "circuit_breaker", received.Source)
			}
		})
	}
}

func TestSend_HMACSignature(t *testing.T) {
	var receivedSig string
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-LLMTrace-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := "test-secret-key-12345"
	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL, Secret: secret}},
	})

	a.Send(context.Background(), EventCustom, nil)

	if receivedSig == "" {
		t.Fatal("expected HMAC signature header")
	}
	if len(receivedSig) < 8 || receivedSig[:7] != "sha256=" {
		t.Errorf("expected sha256= prefix, got %q", receivedSig)
	}

	sigHex := receivedSig[7:]
	if !VerifyHMAC(receivedBody, secret, sigHex) {
		t.Error("HMAC signature verification failed")
	}

	// Verify wrong secret fails
	if VerifyHMAC(receivedBody, "wrong-secret", sigHex) {
		t.Error("HMAC verification should fail with wrong secret")
	}
}

func TestSend_NoHMACWhenSecretEmpty(t *testing.T) {
	var receivedSig string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-LLMTrace-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL}}, // no Secret
	})

	a.Send(context.Background(), EventCustom, nil)

	if receivedSig != "" {
		t.Errorf("expected no signature header, got %q", receivedSig)
	}
}

func TestSend_CustomHeaders(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{
			URL: server.URL,
			Headers: map[string]string{
				"Authorization": "Bearer my-token",
			},
		}},
	})

	a.Send(context.Background(), EventCustom, nil)

	if authHeader != "Bearer my-token" {
		t.Errorf("expected auth header, got %q", authHeader)
	}
}

func TestSend_EventFiltering(t *testing.T) {
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{
			URL:    server.URL,
			Filter: []EventType{EventBudgetAlert}, // only budget alerts
		}},
	})

	// This should be filtered out
	a.Send(context.Background(), EventCircuitBreaker, nil)
	if atomic.LoadInt32(&count) != 0 {
		t.Error("expected circuit breaker event to be filtered")
	}

	// This should go through
	a.Send(context.Background(), EventBudgetAlert, map[string]any{"name": "test", "percentage": 95.0})
	if atomic.LoadInt32(&count) != 1 {
		t.Error("expected budget alert to be delivered")
	}
}

func TestSend_MultipleEndpoints(t *testing.T) {
	var count1, count2 int32
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count1, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count2, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{
			{URL: server1.URL},
			{URL: server2.URL},
		},
	})

	count := a.Send(context.Background(), EventCustom, nil)

	if count != 2 {
		t.Errorf("expected 2 successes, got %d", count)
	}
	if atomic.LoadInt32(&count1) != 1 {
		t.Error("expected server1 to receive 1 request")
	}
	if atomic.LoadInt32(&count2) != 1 {
		t.Error("expected server2 to receive 1 request")
	}
}

func TestSend_RetryOnServerError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints:       []Endpoint{{URL: server.URL}},
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     50 * time.Millisecond,
		Multiplier:      1.5,
	})

	count := a.Send(context.Background(), EventCustom, nil)

	if count != 1 {
		t.Errorf("expected 1 success, got %d", count)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestSend_NoRetryOnClientError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints:       []Endpoint{{URL: server.URL}},
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
	})

	count := a.Send(context.Background(), EventCustom, nil)

	if count != 0 {
		t.Errorf("expected 0 successes, got %d", count)
	}
	// 4xx errors should not be retried (except 429)
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry for 4xx), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestSend_RetryOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints:       []Endpoint{{URL: server.URL}},
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
	})

	count := a.Send(context.Background(), EventCustom, nil)

	if count != 1 {
		t.Errorf("expected 1 success, got %d", count)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestSend_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	a := NewAlerter(Config{
		Endpoints:       []Endpoint{{URL: server.URL}},
		MaxRetries:      3,
		InitialInterval: 10 * time.Millisecond,
	})

	count := a.Send(ctx, EventCustom, nil)

	if count != 0 {
		t.Errorf("expected 0 successes, got %d", count)
	}
}

func TestSend_Callbacks(t *testing.T) {
	t.Run("on_success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		var called bool
		var cbURL string
		a := NewAlerter(Config{
			Endpoints: []Endpoint{{URL: server.URL}},
			OnDeliverySuccess: func(endpoint string, event Event) {
				called = true
				cbURL = endpoint
			},
		})

		a.Send(context.Background(), EventCustom, nil)

		if !called {
			t.Error("expected success callback to be called")
		}
		if cbURL != server.URL {
			t.Errorf("expected callback URL %q, got %q", server.URL, cbURL)
		}
	})

	t.Run("on_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		var called bool
		a := NewAlerter(Config{
			Endpoints:       []Endpoint{{URL: server.URL}},
			MaxRetries:      0,
			InitialInterval: time.Millisecond,
			OnDeliveryError: func(endpoint string, event Event, err error) {
				called = true
			},
		})

		a.Send(context.Background(), EventCustom, nil)

		if !called {
			t.Error("expected error callback to be called")
		}
	})
}

func TestSendEvent_PreConstructed(t *testing.T) {
	var received Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})

	event := Event{
		Type:      EventCustom,
		Title:     "Custom Alert",
		Message:   "Something happened",
		Severity:  "warning",
		Source:    "my-app",
		Metadata:  map[string]any{"key": "value"},
	}

	count := a.SendEvent(context.Background(), event)
	if count != 1 {
		t.Fatalf("expected 1 success, got %d", count)
	}

	if received.Title != "Custom Alert" {
		t.Errorf("expected title %q, got %q", "Custom Alert", received.Title)
	}
	if received.Source != "my-app" {
		t.Errorf("expected source %q, got %q", "my-app", received.Source)
	}
	if received.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value")
	}
}

func TestSendEvent_AutoTimestamp(t *testing.T) {
	var received Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})

	before := time.Now().UTC()
	a.SendEvent(context.Background(), Event{Type: EventCustom})
	after := time.Now().UTC()

	if received.Timestamp.Before(before) || received.Timestamp.After(after) {
		t.Errorf("expected timestamp between %v and %v, got %v", before, after, received.Timestamp)
	}
}

func TestConcurrentSend(t *testing.T) {
	var total int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&total, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL}},
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Send(context.Background(), EventCustom, nil)
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&total) != 10 {
		t.Errorf("expected 10 requests, got %d", atomic.LoadInt32(&total))
	}
}

func TestHMACVerification(t *testing.T) {
	payload := []byte(`{"type":"test","message":"hello"}`)
	secret := "my-secret-key"

	sig := ComputeHMAC(payload, secret)

	if !VerifyHMAC(payload, secret, sig) {
		t.Error("expected valid signature to verify")
	}

	if VerifyHMAC(payload, "wrong-secret", sig) {
		t.Error("expected wrong secret to fail verification")
	}

	if VerifyHMAC([]byte("different payload"), secret, sig) {
		t.Error("expected different payload to fail verification")
	}
}

func TestExponentialBackoff(t *testing.T) {
	initial := 100 * time.Millisecond
	max := 5 * time.Second
	multiplier := 2.0

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 5000 * time.Millisecond}, // capped at max
		{10, 5000 * time.Millisecond}, // still capped
	}

	for _, tt := range tests {
		got := ExponentialBackoff(tt.attempt, initial, max, multiplier)
		if got != tt.expected {
			t.Errorf("ExponentialBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestExponentialBackoff_NegativeAttempt(t *testing.T) {
	got := ExponentialBackoff(-1, 100*time.Millisecond, 5*time.Second, 2.0)
	if got != 100*time.Millisecond {
		t.Errorf("expected initial duration for negative attempt, got %v", got)
	}
}

func TestEventJSONSerialization(t *testing.T) {
	event := Event{
		Type:      EventBudgetAlert,
		Timestamp: time.Date(2026, 6, 15, 20, 30, 0, 0, time.UTC),
		Severity:  "critical",
		Title:     "Budget Exceeded",
		Message:   "API budget has been exceeded",
		Source:    "budget",
		Metadata: map[string]any{
			"budget_name": "api-budget",
			"spent":       1050.0,
			"limit":       1000.0,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != EventBudgetAlert {
		t.Errorf("expected type %q, got %q", EventBudgetAlert, decoded.Type)
	}
	if decoded.Severity != "critical" {
		t.Errorf("expected severity %q, got %q", "critical", decoded.Severity)
	}
	if decoded.Title != "Budget Exceeded" {
		t.Errorf("expected title %q, got %q", "Budget Exceeded", decoded.Title)
	}
	if decoded.Metadata["budget_name"] != "api-budget" {
		t.Errorf("expected metadata budget_name=api-budget")
	}
}

func TestUserAgentHeader(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})
	a.Send(context.Background(), EventCustom, nil)

	if userAgent != "LLMTrace-Webhook/1.0" {
		t.Errorf("expected User-Agent %q, got %q", "LLMTrace-Webhook/1.0", userAgent)
	}
}

func TestContentTypeHeader(t *testing.T) {
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})
	a.Send(context.Background(), EventCustom, nil)

	if contentType != "application/json" {
		t.Errorf("expected Content-Type %q, got %q", "application/json", contentType)
	}
}

func TestMultipleEndpointPartialFailure(t *testing.T) {
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer goodServer.Close()

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // non-retriable
	}))
	defer badServer.Close()

	var errCount int32
	a := NewAlerter(Config{
		Endpoints: []Endpoint{
			{URL: goodServer.URL},
			{URL: badServer.URL},
		},
		MaxRetries: 0,
		OnDeliveryError: func(endpoint string, event Event, err error) {
			atomic.AddInt32(&errCount, 1)
		},
	})

	count := a.Send(context.Background(), EventCustom, nil)

	if count != 1 {
		t.Errorf("expected 1 success, got %d", count)
	}
	if atomic.LoadInt32(&errCount) != 1 {
		t.Errorf("expected 1 error callback, got %d", atomic.LoadInt32(&errCount))
	}
}

func TestEndpointMultipleFilters(t *testing.T) {
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{
			URL:    server.URL,
			Filter: []EventType{EventBudgetAlert, EventCircuitBreaker},
		}},
	})

	a.Send(context.Background(), EventBudgetAlert, map[string]any{"name": "test", "percentage": 95.0})
	a.Send(context.Background(), EventCircuitBreaker, map[string]any{"name": "test", "state": "open"})
	a.Send(context.Background(), EventRateLimit, nil) // should be filtered

	if atomic.LoadInt32(&count) != 2 {
		t.Errorf("expected 2 deliveries, got %d", atomic.LoadInt32(&count))
	}
}

func TestCustomEventType(t *testing.T) {
	var received Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{Endpoints: []Endpoint{{URL: server.URL}}})
	a.Send(context.Background(), EventCustom, map[string]any{"key": "value"})

	if received.Type != EventCustom {
		t.Errorf("expected type %q, got %q", EventCustom, received.Type)
	}
	if received.Source != "llmtrace" {
		t.Errorf("expected source %q, got %q", "llmtrace", received.Source)
	}
}
