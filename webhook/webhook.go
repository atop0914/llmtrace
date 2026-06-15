// Package webhook provides HTTP webhook notifications for LLMTrace alerts.
//
// Webhook alerter sends POST requests to configured endpoints when alert
// events occur (budget exceeded, circuit breaker state changes, error spikes).
// It supports HMAC-SHA256 signature verification, configurable retry with
// exponential backoff, and event type filtering.
//
// Usage:
//
//	alerter := webhook.NewAlerter(webhook.Config{
//		Endpoints: []webhook.Endpoint{
//			{URL: "https://hooks.slack.com/...", Secret: "my-secret"},
//		},
//	})
//
//	// Use with budget tracker
//	budget := llmtrace.NewBudgetTracker(llmtrace.BudgetConfig{
//		OnAlert: func(alert llmtrace.BudgetAlert) {
//			alerter.Send(context.Background(), webhook.EventBudgetAlert, alert)
//		},
//	})
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// EventType categorizes alert events.
type EventType string

const (
	// EventBudgetAlert fires when a budget threshold is reached or exceeded.
	EventBudgetAlert EventType = "budget_alert"

	// EventCircuitBreaker fires when a circuit breaker changes state.
	EventCircuitBreaker EventType = "circuit_breaker"

	// EventRateLimit fires when rate limit is consistently being hit.
	EventRateLimit EventType = "rate_limit"

	// EventErrorSpike fires when error rate exceeds a threshold.
	EventErrorSpike EventType = "error_spike"

	// EventCustom is a user-defined event type.
	EventCustom EventType = "custom"
)

// Event represents a webhook alert event.
type Event struct {
	// Type is the event category.
	Type EventType `json:"type"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Severity is the alert severity (info, warning, critical).
	Severity string `json:"severity"`

	// Title is a short human-readable summary.
	Title string `json:"title"`

	// Message provides detailed information about the event.
	Message string `json:"message"`

	// Source identifies the component that generated the event.
	Source string `json:"source,omitempty"`

	// Metadata holds arbitrary key-value pairs for additional context.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Endpoint defines a webhook delivery target.
type Endpoint struct {
	// URL is the webhook endpoint URL.
	URL string

	// Secret is the HMAC-SHA256 signing secret. If empty, no signature is sent.
	Secret string

	// Headers are additional HTTP headers to include in the request.
	Headers map[string]string

	// Filter is a list of event types to deliver. If empty, all events are delivered.
	Filter []EventType
}

// Config configures the webhook Alerter.
type Config struct {
	// Endpoints is the list of webhook endpoints to deliver events to.
	Endpoints []Endpoint

	// HTTPClient is the HTTP client to use. Defaults to http.DefaultClient.
	HTTPClient *http.Client

	// MaxRetries is the maximum number of delivery retry attempts. Default: 3.
	MaxRetries int

	// InitialInterval is the initial retry delay. Default: 1 second.
	InitialInterval time.Duration

	// MaxInterval is the maximum retry delay. Default: 30 seconds.
	MaxInterval time.Duration

	// Multiplier is the backoff multiplier. Default: 2.0.
	Multiplier float64

	// OnDeliveryError is called when a webhook delivery fails after all retries.
	OnDeliveryError func(endpoint string, event Event, err error)

	// OnDeliverySuccess is called when a webhook delivery succeeds.
	OnDeliverySuccess func(endpoint string, event Event)
}

// Alerter sends webhook notifications for alert events.
// It is safe for concurrent use.
type Alerter struct {
	endpoints []Endpoint
	client    *http.Client
	retries   int
	initial   time.Duration
	maxInt    time.Duration
	multi     float64
	onErr     func(string, Event, error)
	onOK      func(string, Event)
}

// NewAlerter creates a new webhook Alerter with the given configuration.
func NewAlerter(cfg Config) *Alerter {
	a := &Alerter{
		endpoints: cfg.Endpoints,
		client:    cfg.HTTPClient,
		retries:   cfg.MaxRetries,
		initial:   cfg.InitialInterval,
		maxInt:    cfg.MaxInterval,
		multi:     cfg.Multiplier,
		onErr:     cfg.OnDeliveryError,
		onOK:      cfg.OnDeliverySuccess,
	}
	if a.client == nil {
		a.client = http.DefaultClient
	}
	if a.retries == 0 {
		a.retries = 3
	}
	if a.initial == 0 {
		a.initial = time.Second
	}
	if a.maxInt == 0 {
		a.maxInt = 30 * time.Second
	}
	if a.multi == 0 {
		a.multi = 2.0
	}
	return a
}

// Send delivers an event to all matching endpoints.
// It returns the number of endpoints that received the event successfully.
// Send is safe for concurrent use.
func (a *Alerter) Send(ctx context.Context, eventType EventType, data any) int {
	event := Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Metadata:  make(map[string]any),
	}

	// Populate event fields based on data type
	a.populateEvent(&event, eventType, data)

	return a.sendEvent(ctx, event)
}

// SendEvent delivers a pre-constructed event to all matching endpoints.
func (a *Alerter) SendEvent(ctx context.Context, event Event) int {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	return a.sendEvent(ctx, event)
}

// sendEvent delivers an event to all matching endpoints.
func (a *Alerter) sendEvent(ctx context.Context, event Event) int {
	var (
		mu      sync.Mutex
		success int
		wg      sync.WaitGroup
	)

	for _, ep := range a.endpoints {
		if !a.matchesFilter(ep, event.Type) {
			continue
		}

		wg.Add(1)
		go func(ep Endpoint) {
			defer wg.Done()
			err := a.deliver(ctx, ep, event)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if a.onErr != nil {
					a.onErr(ep.URL, event, err)
				}
			} else {
				success++
				if a.onOK != nil {
					a.onOK(ep.URL, event)
				}
			}
		}(ep)
	}

	wg.Wait()
	return success
}

// matchesFilter checks if an endpoint should receive this event type.
func (a *Alerter) matchesFilter(ep Endpoint, eventType EventType) bool {
	if len(ep.Filter) == 0 {
		return true // no filter = all events
	}
	for _, t := range ep.Filter {
		if t == eventType {
			return true
		}
	}
	return false
}

// nonRetriableError wraps an error to indicate it should not be retried.
type nonRetriableError struct {
	err error
}

func (e *nonRetriableError) Error() string { return e.err.Error() }
func (e *nonRetriableError) Unwrap() error { return e.err }

// deliver sends an event to a single endpoint with retry logic.
func (a *Alerter) deliver(ctx context.Context, ep Endpoint, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	var lastErr error
	delay := a.initial

	for attempt := 0; attempt <= a.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = time.Duration(float64(delay) * a.multi)
			if delay > a.maxInt {
				delay = a.maxInt
			}
		}

		// Check context before making request
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err = a.doRequest(ctx, ep, body)
		if err == nil {
			return nil
		}

		// Non-retriable errors are returned immediately
		if _, ok := err.(*nonRetriableError); ok {
			return err
		}

		lastErr = err
	}

	return fmt.Errorf("delivery failed after %d retries: %w", a.retries, lastErr)
}

// doRequest makes a single HTTP POST request to the endpoint.
func (a *Alerter) doRequest(ctx context.Context, ep Endpoint, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LLMTrace-Webhook/1.0")

	// Add HMAC signature if secret is configured
	if ep.Secret != "" {
		sig := computeHMAC(body, ep.Secret)
		req.Header.Set("X-LLMTrace-Signature", "sha256="+sig)
	}

	// Add custom headers
	for k, v := range ep.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // drain body for connection reuse

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// 4xx errors (except 429) are not retriable
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
		return &nonRetriableError{fmt.Errorf("non-retriable status %d", resp.StatusCode)}
	}

	return fmt.Errorf("retriable status %d", resp.StatusCode)
}

// populateEvent fills event fields from the data payload.
func (a *Alerter) populateEvent(event *Event, eventType EventType, data any) {
	switch eventType {
	case EventBudgetAlert:
		a.populateBudgetEvent(event, data)
	case EventCircuitBreaker:
		a.populateCircuitBreakerEvent(event, data)
	default:
		event.Source = "llmtrace"
		if data != nil {
			event.Metadata["data"] = data
		}
	}
}

// populateBudgetEvent fills event from a budget alert.
func (a *Alerter) populateBudgetEvent(event *Event, data any) {
	event.Source = "budget"
	switch d := data.(type) {
	case map[string]any:
		if name, ok := d["name"].(string); ok {
			event.Title = fmt.Sprintf("Budget Alert: %s", name)
		}
		if pct, ok := d["percentage"].(float64); ok {
			switch {
			case pct >= 100:
				event.Severity = "critical"
				event.Message = fmt.Sprintf("Budget exceeded (%.1f%%)", pct)
			case pct >= 90:
				event.Severity = "warning"
				event.Message = fmt.Sprintf("Budget at %.1f%%", pct)
			default:
				event.Severity = "info"
				event.Message = fmt.Sprintf("Budget at %.1f%%", pct)
			}
		}
		event.Metadata = d
	default:
		event.Title = "Budget Alert"
		event.Severity = "warning"
		event.Metadata["data"] = data
	}
}

// populateCircuitBreakerEvent fills event from a circuit breaker state change.
func (a *Alerter) populateCircuitBreakerEvent(event *Event, data any) {
	event.Source = "circuit_breaker"
	switch d := data.(type) {
	case map[string]any:
		if name, ok := d["name"].(string); ok {
			event.Title = fmt.Sprintf("Circuit Breaker: %s", name)
		}
		if state, ok := d["state"].(string); ok {
			switch state {
			case "open":
				event.Severity = "critical"
				event.Message = "Circuit breaker opened"
			case "half-open":
				event.Severity = "warning"
				event.Message = "Circuit breaker half-open (testing recovery)"
			case "closed":
				event.Severity = "info"
				event.Message = "Circuit breaker closed (recovered)"
			}
		}
		event.Metadata = d
	default:
		event.Title = "Circuit Breaker State Change"
		event.Severity = "warning"
		event.Metadata["data"] = data
	}
}

// ComputeHMAC computes HMAC-SHA256 of the payload using the given secret.
// This is exported so receivers can verify signatures.
func ComputeHMAC(payload []byte, secret string) string {
	return computeHMAC(payload, secret)
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC verifies an HMAC-SHA256 signature against the payload.
// Returns true if the signature is valid.
func VerifyHMAC(payload []byte, secret, signature string) bool {
	expected := computeHMAC(payload, secret)
	// Use constant-time comparison
	return hmac.Equal([]byte(signature), []byte(expected))
}

// EndpointCount returns the number of configured endpoints.
func (a *Alerter) EndpointCount() int {
	return len(a.endpoints)
}

// ExponentialBackoff calculates the delay for a given attempt number.
// Useful for custom retry logic.
func ExponentialBackoff(attempt int, initial time.Duration, max time.Duration, multiplier float64) time.Duration {
	if attempt <= 0 {
		return initial
	}
	delay := initial
	for i := 0; i < attempt; i++ {
		delay = time.Duration(math.Min(
			float64(delay)*multiplier,
			float64(max),
		))
	}
	return delay
}
