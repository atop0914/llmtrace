package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/atop0914/llmtrace/metrics"
)

// setupRegistry creates a test registry with sample metrics.
func setupRegistry() *metrics.Registry {
	reg := metrics.NewRegistry("llmtrace")

	// Requests counter
	reqTotal := reg.RegisterCounter("requests_total", "Total requests", []string{"provider", "model"})
	reqTotal.With("openai", "gpt-4o").Add(100)
	reqTotal.With("openai", "gpt-3.5-turbo").Add(50)
	reqTotal.With("anthropic", "claude-3-opus").Add(30)

	// Tokens counter
	tokensTotal := reg.RegisterCounter("tokens_total", "Total tokens", []string{"provider", "model"})
	tokensTotal.With("openai", "gpt-4o").Add(50000)
	tokensTotal.With("openai", "gpt-3.5-turbo").Add(20000)
	tokensTotal.With("anthropic", "claude-3-opus").Add(30000)

	inputTokens := reg.RegisterCounter("input_tokens_total", "Input tokens", []string{"provider", "model"})
	inputTokens.With("openai", "gpt-4o").Add(30000)
	inputTokens.With("openai", "gpt-3.5-turbo").Add(12000)
	inputTokens.With("anthropic", "claude-3-opus").Add(18000)

	outputTokens := reg.RegisterCounter("output_tokens_total", "Output tokens", []string{"provider", "model"})
	outputTokens.With("openai", "gpt-4o").Add(20000)
	outputTokens.With("openai", "gpt-3.5-turbo").Add(8000)
	outputTokens.With("anthropic", "claude-3-opus").Add(12000)

	// Cost counter
	costTotal := reg.RegisterCounter("cost_usd_total", "Total cost", []string{"provider", "model"})
	costTotal.With("openai", "gpt-4o").Add(1.50)
	costTotal.With("openai", "gpt-3.5-turbo").Add(0.10)
	costTotal.With("anthropic", "claude-3-opus").Add(0.90)

	// Active requests gauge
	activeReqs := reg.RegisterGauge("active_requests", "Active requests", []string{"provider"})
	activeReqs.With("openai").Set(3)
	activeReqs.With("anthropic").Set(1)

	// Errors counter
	errorsTotal := reg.RegisterCounter("errors_total", "Total errors", []string{"provider", "error_type"})
	errorsTotal.With("openai", "rate_limit").Add(5)
	errorsTotal.With("anthropic", "server_error").Add(2)

	// Latency histogram
	latency := reg.RegisterHistogram("request_duration_seconds", "Request latency", []string{"provider", "model"}, metrics.DurationBuckets())
	for i := 0; i < 100; i++ {
		latency.With("openai", "gpt-4o").Observe(0.05 + float64(i)*0.001)
	}
	for i := 0; i < 50; i++ {
		latency.With("openai", "gpt-3.5-turbo").Observe(0.02 + float64(i)*0.001)
	}
	for i := 0; i < 30; i++ {
		latency.With("anthropic", "claude-3-opus").Observe(0.1 + float64(i)*0.002)
	}

	return reg
}

func TestHandleOverview(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/overview", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp OverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.TotalRequests != 180 {
		t.Errorf("expected 180 requests, got %d", resp.TotalRequests)
	}
	if resp.TotalTokens != 100000 {
		t.Errorf("expected 100000 tokens, got %d", resp.TotalTokens)
	}
	if resp.InputTokens != 60000 {
		t.Errorf("expected 60000 input tokens, got %d", resp.InputTokens)
	}
	if resp.OutputTokens != 40000 {
		t.Errorf("expected 40000 output tokens, got %d", resp.OutputTokens)
	}
	if resp.TotalCostUSD < 2.49 || resp.TotalCostUSD > 2.51 {
		t.Errorf("expected ~2.50 cost, got %f", resp.TotalCostUSD)
	}
	if resp.ActiveRequests != 4 {
		t.Errorf("expected 4 active requests, got %d", resp.ActiveRequests)
	}
	if resp.TotalErrors != 7 {
		t.Errorf("expected 7 errors, got %d", resp.TotalErrors)
	}
	if resp.ProviderCount != 2 {
		t.Errorf("expected 2 providers, got %d", resp.ProviderCount)
	}
	if resp.ModelCount != 3 {
		t.Errorf("expected 3 models, got %d", resp.ModelCount)
	}
	if resp.AvgLatencyMS <= 0 {
		t.Errorf("expected positive avg latency, got %f", resp.AvgLatencyMS)
	}
	if resp.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestHandleProviders(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/providers", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	// Sorted alphabetically
	anthropic := resp.Providers[0]
	if anthropic.Name != "anthropic" {
		t.Errorf("expected anthropic, got %s", anthropic.Name)
	}
	if anthropic.Requests != 30 {
		t.Errorf("expected 30 requests for anthropic, got %d", anthropic.Requests)
	}
	if anthropic.ActiveRequests != 1 {
		t.Errorf("expected 1 active for anthropic, got %d", anthropic.ActiveRequests)
	}
	if anthropic.Errors != 2 {
		t.Errorf("expected 2 errors for anthropic, got %d", anthropic.Errors)
	}

	openai := resp.Providers[1]
	if openai.Name != "openai" {
		t.Errorf("expected openai, got %s", openai.Name)
	}
	if openai.Requests != 150 {
		t.Errorf("expected 150 requests for openai, got %d", openai.Requests)
	}
}

func TestHandleModels(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp ModelResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(resp.Models))
	}

	// Find models by name
	modelMap := make(map[string]ModelData)
	for _, m := range resp.Models {
		key := m.Provider + "/" + m.Model
		modelMap[key] = m
	}

	m, ok := modelMap["anthropic/claude-3-opus"]
	if !ok {
		t.Fatal("missing anthropic/claude-3-opus")
	}
	if m.Requests != 30 {
		t.Errorf("expected 30 requests, got %d", m.Requests)
	}

	m, ok = modelMap["openai/gpt-4o"]
	if !ok {
		t.Fatal("missing openai/gpt-4o")
	}
	if m.Requests != 100 {
		t.Errorf("expected 100 requests, got %d", m.Requests)
	}

	m, ok = modelMap["openai/gpt-3.5-turbo"]
	if !ok {
		t.Fatal("missing openai/gpt-3.5-turbo")
	}
	if m.Requests != 50 {
		t.Errorf("expected 50 requests, got %d", m.Requests)
	}
}

func TestHandleLatency(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/latency", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp LatencyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Providers) != 3 {
		t.Fatalf("expected 3 distributions, got %d", len(resp.Providers))
	}

	// Find by provider/model
	distMap := make(map[string]LatencyDistribution)
	for _, d := range resp.Providers {
		key := d.Provider + "/" + d.Model
		distMap[key] = d
	}

	d, ok := distMap["openai/gpt-4o"]
	if !ok {
		t.Fatal("missing openai/gpt-4o latency")
	}
	if d.Count != 100 {
		t.Errorf("expected 100 observations, got %d", d.Count)
	}
	if d.AvgMS <= 0 {
		t.Errorf("expected positive avg latency, got %f", d.AvgMS)
	}
	if len(d.Buckets) == 0 {
		t.Error("expected non-empty buckets")
	}
	if d.Buckets[0].UpperMS <= 0 {
		t.Errorf("expected positive upper_ms, got %f", d.Buckets[0].UpperMS)
	}
}

func TestHandleCosts(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/costs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp CostResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.TotalUSD < 2.49 || resp.TotalUSD > 2.51 {
		t.Errorf("expected ~2.50 total cost, got %f", resp.TotalUSD)
	}

	if len(resp.ByModel) != 3 {
		t.Fatalf("expected 3 cost entries, got %d", len(resp.ByModel))
	}

	// Sorted by cost descending
	if resp.ByModel[0].CostUSD < resp.ByModel[1].CostUSD {
		t.Error("expected sorted by cost descending")
	}

	// Check avg cost
	for _, cm := range resp.ByModel {
		if cm.Requests > 0 && cm.AvgCost <= 0 {
			t.Errorf("expected positive avg cost for %s/%s", cm.Provider, cm.Model)
		}
	}
}

func TestHandleErrors(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/errors", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.TotalErrors != 7 {
		t.Errorf("expected 7 total errors, got %d", resp.TotalErrors)
	}

	if len(resp.ByType) != 2 {
		t.Fatalf("expected 2 error types, got %d", len(resp.ByType))
	}

	if len(resp.ByProvider) != 2 {
		t.Fatalf("expected 2 error providers, got %d", len(resp.ByProvider))
	}

	// Check error type counts
	typeMap := make(map[string]int64)
	for _, et := range resp.ByType {
		typeMap[et.Type] = et.Count
	}
	if typeMap["rate_limit"] != 5 {
		t.Errorf("expected 5 rate_limit errors, got %d", typeMap["rate_limit"])
	}
	if typeMap["server_error"] != 2 {
		t.Errorf("expected 2 server_error errors, got %d", typeMap["server_error"])
	}
}

func TestHandleUnknownEndpoint(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestLandingPage(t *testing.T) {
	reg := metrics.NewRegistry("test")
	h := Handler(reg, Config{})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "LLMTrace Dashboard") {
		t.Error("expected LLMTrace Dashboard in body")
	}
	if !strings.Contains(body, "/api/overview") {
		t.Error("expected API endpoint links in body")
	}
}

func TestLandingPageSubpath(t *testing.T) {
	reg := metrics.NewRegistry("test")
	h := Handler(reg, Config{})

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSSEEndpoint(t *testing.T) {
	reg := setupRegistry()
	sse := newSSEHandler(reg, 100*time.Millisecond)

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(req.Context(), 250*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	sse.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: overview") {
		t.Error("expected overview event in SSE stream")
	}
	if !strings.Contains(body, "total_requests") {
		t.Error("expected total_requests in SSE data")
	}
}

func TestEmptyRegistry(t *testing.T) {
	reg := metrics.NewRegistry("empty")
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/overview", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp OverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalRequests != 0 {
		t.Errorf("expected 0 requests, got %d", resp.TotalRequests)
	}

	req = httptest.NewRequest("GET", "/api/providers", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var provResp ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&provResp); err != nil {
		t.Fatal(err)
	}
	if len(provResp.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(provResp.Providers))
	}
}

func TestCORSHeader(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/overview", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header *")
	}
}

func TestJSONContentType(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	endpoints := []string{"/api/overview", "/api/providers", "/api/models", "/api/latency", "/api/costs", "/api/errors"}
	for _, ep := range endpoints {
		req := httptest.NewRequest("GET", ep, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("%s: expected application/json, got %s", ep, ct)
		}
	}
}

// mockTraceStore is a simple in-memory trace store for testing.
type mockTraceStore struct {
	records []TraceRecord
}

func (m *mockTraceStore) Query(q TraceQuery) []TraceRecord {
	var results []TraceRecord
	for _, r := range m.records {
		if q.Provider != "" && r.Provider != q.Provider {
			continue
		}
		if q.Model != "" && r.Model != q.Model {
			continue
		}
		if q.Status != "" && r.Status != q.Status {
			continue
		}
		results = append(results, r)
	}
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[:q.Limit]
	}
	return results
}

func (m *mockTraceStore) TraceSummary() TraceSummaryResult {
	result := TraceSummaryResult{
		Providers: make(map[string]int),
		Models:    make(map[string]int),
	}
	for _, r := range m.records {
		result.TotalTraces++
		result.TotalTokens += r.TotalTokens
		result.TotalCostUSD += r.CostUSD
		if r.Status == "error" {
			result.TotalErrors++
		}
		result.Providers[r.Provider]++
		result.Models[r.Model]++
	}
	return result
}

func (m *mockTraceStore) Len() int { return len(m.records) }

func TestHandleTraces(t *testing.T) {
	reg := setupRegistry()
	store := &mockTraceStore{
		records: []TraceRecord{
			{ID: "trace-1", Provider: "openai", Model: "gpt-4o", Status: "success", InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			{ID: "trace-2", Provider: "anthropic", Model: "claude-3-opus", Status: "error", Error: "rate limit"},
			{ID: "trace-3", Provider: "openai", Model: "gpt-3.5-turbo", Status: "success", InputTokens: 200, OutputTokens: 100, TotalTokens: 300},
		},
	}
	handler := newAPIHandler(reg, store)

	t.Run("all traces", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traces", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp TracesResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Traces) != 3 {
			t.Errorf("expected 3 traces, got %d", len(resp.Traces))
		}
		if resp.Total != 3 {
			t.Errorf("expected total 3, got %d", resp.Total)
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traces?provider=openai", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var resp TracesResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Traces) != 2 {
			t.Errorf("expected 2 openai traces, got %d", len(resp.Traces))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traces?status=error", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var resp TracesResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Traces) != 1 {
			t.Errorf("expected 1 error trace, got %d", len(resp.Traces))
		}
		if resp.Traces[0].ID != "trace-2" {
			t.Errorf("expected trace-2, got %s", resp.Traces[0].ID)
		}
	})

	t.Run("limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traces?limit=1", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		var resp TracesResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Traces) != 1 {
			t.Errorf("expected 1 trace with limit, got %d", len(resp.Traces))
		}
	})
}

func TestHandleTraces_NilStore(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/traces", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp TracesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Traces) != 0 {
		t.Errorf("expected 0 traces with nil store, got %d", len(resp.Traces))
	}
}

func TestHandleTraceSummary(t *testing.T) {
	reg := setupRegistry()
	store := &mockTraceStore{
		records: []TraceRecord{
			{Provider: "openai", Model: "gpt-4o", Status: "success", TotalTokens: 150, CostUSD: 0.01},
			{Provider: "openai", Model: "gpt-4o", Status: "error", TotalTokens: 0, CostUSD: 0},
			{Provider: "anthropic", Model: "claude-3-opus", Status: "success", TotalTokens: 300, CostUSD: 0.02},
		},
	}
	handler := newAPIHandler(reg, store)

	req := httptest.NewRequest("GET", "/api/traces/summary", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp TraceSummaryResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalTraces != 3 {
		t.Errorf("expected 3 traces, got %d", resp.TotalTraces)
	}
	if resp.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", resp.TotalErrors)
	}
	if resp.TotalTokens != 450 {
		t.Errorf("expected 450 tokens, got %d", resp.TotalTokens)
	}
	if resp.Providers["openai"] != 2 {
		t.Errorf("expected 2 openai traces, got %d", resp.Providers["openai"])
	}
}

func TestHandleTraceSummary_NilStore(t *testing.T) {
	reg := setupRegistry()
	handler := newAPIHandler(reg, nil)

	req := httptest.NewRequest("GET", "/api/traces/summary", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp TraceSummaryResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TotalTraces != 0 {
		t.Errorf("expected 0 traces, got %d", resp.TotalTraces)
	}
}


