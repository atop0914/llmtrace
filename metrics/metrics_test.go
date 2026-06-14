package metrics

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/atop0914/llmtrace"
)

func TestCounter(t *testing.T) {
	c := &Counter{}
	if c.Value() != 0 {
		t.Errorf("initial counter = %v, want 0", c.Value())
	}
	c.Inc()
	if c.Value() != 1 {
		t.Errorf("after Inc = %v, want 1", c.Value())
	}
	c.Inc()
	if c.Value() != 2 {
		t.Errorf("after second Inc = %v, want 2", c.Value())
	}
}

func TestCounterAdd(t *testing.T) {
	c := &Counter{}
	c.Add(42.5)
	if got := c.Value(); math.Abs(got-42.5) > 1e-9 {
		t.Errorf("after Add(42.5) = %v, want 42.5", got)
	}
}

func TestCounterConcurrent(t *testing.T) {
	c := &Counter{}
	var wg sync.WaitGroup
	n := 1000
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Inc()
		}()
	}
	wg.Wait()
	if c.Value() != float64(n) {
		t.Errorf("concurrent Inc x%d = %v, want %d", n, c.Value(), n)
	}
}

func TestGauge(t *testing.T) {
	g := &Gauge{}
	g.Set(42.0)
	if got := g.Value(); got != 42.0 {
		t.Errorf("Set(42) -> %v, want 42", got)
	}
	g.Inc()
	if got := g.Value(); got != 43.0 {
		t.Errorf("Inc -> %v, want 43", got)
	}
	g.Dec()
	if got := g.Value(); got != 42.0 {
		t.Errorf("Dec -> %v, want 42", got)
	}
	g.Add(-10)
	if got := g.Value(); got != 32.0 {
		t.Errorf("Add(-10) -> %v, want 32", got)
	}
}

func TestGaugeConcurrent(t *testing.T) {
	g := &Gauge{}
	g.Set(0)
	var wg sync.WaitGroup
	n := 1000
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			g.Inc()
		}()
		go func() {
			defer wg.Done()
			g.Dec()
		}()
	}
	wg.Wait()
	if got := g.Value(); got != 0 {
		t.Errorf("concurrent Inc+Dec x%d = %v, want 0", n, got)
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram([]float64{0.1, 0.5, 1.0})

	// Observe some values
	h.Observe(0.05) // <= 0.1
	h.Observe(0.3)  // <= 0.5
	h.Observe(0.8)  // <= 1.0
	h.Observe(2.0)  // > all buckets (only in +Inf)
	h.Observe(0.1)  // <= 0.1

	buckets, count, sum := h.Snapshot()

	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
	expectedSum := 0.05 + 0.3 + 0.8 + 2.0 + 0.1
	if math.Abs(sum-expectedSum) > 1e-9 {
		t.Errorf("sum = %v, want %v", sum, expectedSum)
	}

	// Check cumulative bucket counts
	// Buckets: {0.1, 0.5, 1.0, +Inf}
	expectedCounts := []uint64{2, 3, 4, 5}
	if len(buckets) != 4 {
		t.Fatalf("got %d buckets, want 4", len(buckets))
	}
	for i, expected := range expectedCounts {
		if buckets[i].Count != expected {
			t.Errorf("bucket[%d] count = %d, want %d", i, buckets[i].Count, expected)
		}
	}
}

func TestHistogramDefaultBuckets(t *testing.T) {
	h := NewHistogram(nil)
	if len(h.buckets) != len(DefaultBuckets) {
		t.Errorf("default buckets len = %d, want %d", len(h.buckets), len(DefaultBuckets))
	}
}

func TestHistogramConcurrent(t *testing.T) {
	h := NewHistogram([]float64{100})
	var wg sync.WaitGroup
	n := 1000
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			h.Observe(50)
		}()
	}
	wg.Wait()
	_, count, _ := h.Snapshot()
	if count != uint64(n) {
		t.Errorf("count = %d, want %d", count, n)
	}
}

func TestRegistryCounter(t *testing.T) {
	reg := NewRegistry("test")
	cv := reg.RegisterCounter("requests_total", "Total requests", []string{"method"})

	cv.With("GET").Inc()
	cv.With("GET").Inc()
	cv.With("POST").Inc()

	out := reg.WritePrometheus()
	if !strings.Contains(out, "# HELP test_requests_total Total requests") {
		t.Error("missing HELP line")
	}
	if !strings.Contains(out, "# TYPE test_requests_total counter") {
		t.Error("missing TYPE line")
	}
	if !strings.Contains(out, `test_requests_total{method="GET"} 2`) {
		t.Errorf("missing GET counter in output:\n%s", out)
	}
	if !strings.Contains(out, `test_requests_total{method="POST"} 1`) {
		t.Errorf("missing POST counter in output:\n%s", out)
	}
}

func TestRegistryGauge(t *testing.T) {
	reg := NewRegistry("")
	gv := reg.RegisterGauge("active", "Active connections", []string{"pool"})

	gv.With("default").Set(5)
	gv.With("default").Inc()

	out := reg.WritePrometheus()
	if !strings.Contains(out, `active{pool="default"} 6`) {
		t.Errorf("expected gauge value 6 in output:\n%s", out)
	}
}

func TestRegistryHistogram(t *testing.T) {
	reg := NewRegistry("app")
	hv := reg.RegisterHistogram("request_duration", "Request duration", []string{"path"}, []float64{0.1, 1})

	hv.With("/api").Observe(0.05)
	hv.With("/api").Observe(0.5)
	hv.With("/api").Observe(2.0)

	out := reg.WritePrometheus()
	if !strings.Contains(out, "# TYPE app_request_duration histogram") {
		t.Error("missing histogram TYPE")
	}
	if !strings.Contains(out, `app_request_duration_bucket{path="/api",le="0.1"} 1`) {
		t.Errorf("missing bucket le=0.1:\n%s", out)
	}
	if !strings.Contains(out, `app_request_duration_bucket{path="/api",le="1"} 2`) {
		t.Errorf("missing bucket le=1:\n%s", out)
	}
	if !strings.Contains(out, `app_request_duration_count{path="/api"} 3`) {
		t.Errorf("missing count:\n%s", out)
	}
}

func TestRegistryNoNamespace(t *testing.T) {
	reg := NewRegistry("")
	cv := reg.RegisterCounter("total", "A counter", nil)
	cv.With().Inc()
	out := reg.WritePrometheus()
	if !strings.Contains(out, "total ") {
		t.Errorf("expected 'total ' in output:\n%s", out)
	}
}

func TestRegistrySnapshot(t *testing.T) {
	reg := NewRegistry("ns")
	reg.RegisterCounter("c1", "counter 1", nil)
	reg.RegisterGauge("g1", "gauge 1", nil)
	reg.RegisterHistogram("h1", "histo 1", nil, nil)

	infos := reg.Snapshot()
	if len(infos) != 3 {
		t.Fatalf("got %d infos, want 3", len(infos))
	}
	// Should be sorted by name
	if infos[0].Name != "ns_c1" {
		t.Errorf("first metric = %s, want ns_c1", infos[0].Name)
	}
}

func TestRegistryReset(t *testing.T) {
	reg := NewRegistry("")
	cv := reg.RegisterCounter("c", "a counter", []string{"k"})
	cv.With("a").Inc()
	cv.With("a").Inc()

	reg.Reset()

	out := reg.WritePrometheus()
	// After reset, samples map is empty, so no samples should appear
	if strings.Contains(out, `c{k="a"} 2`) {
		t.Errorf("counter should be reset:\n%s", out)
	}
}

func TestHandler(t *testing.T) {
	reg := NewRegistry("llm")
	cv := reg.RegisterCounter("requests", "Requests", []string{"method"})
	cv.With("GET").Inc()

	handler := Handler(reg)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("content-type = %s, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "llm_requests") {
		t.Errorf("missing metric in response:\n%s", body)
	}
}

func TestCounterVecMultiLabels(t *testing.T) {
	reg := NewRegistry("")
	cv := reg.RegisterCounter("req", "requests", []string{"method", "status"})

	cv.With("GET", "200").Inc()
	cv.With("GET", "200").Inc()
	cv.With("POST", "201").Inc()
	cv.With("GET", "500").Inc()

	out := reg.WritePrometheus()
	if !strings.Contains(out, `req{method="GET",status="200"} 2`) {
		t.Errorf("missing GET/200:\n%s", out)
	}
	if !strings.Contains(out, `req{method="POST",status="201"} 1`) {
		t.Errorf("missing POST/201:\n%s", out)
	}
	if !strings.Contains(out, `req{method="GET",status="500"} 1`) {
		t.Errorf("missing GET/500:\n%s", out)
	}
}

// --- LLM Collector Tests ---

func TestLLMCollectorMiddleware(t *testing.T) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	// Mock complete function
	mockComplete := func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		return &llmtrace.Response{
			Model:        req.Model,
			Content:      "Hello!",
			FinishReason: "stop",
			Usage: llmtrace.Usage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
			Provider: "openai",
		}, nil
	}

	// Apply middleware
	mw := collector.Middleware()
	wrapped := mw(mockComplete)

	// Set provider in context
	ctx := ContextWithProvider(context.Background(), "openai")

	resp, err := wrapped(ctx, &llmtrace.Request{Model: "gpt-4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("content = %q, want %q", resp.Content, "Hello!")
	}

	// Check metrics
	out := reg.WritePrometheus()
	if !strings.Contains(out, `llmtrace_requests_total{provider="openai",model="gpt-4"} 1`) {
		t.Errorf("missing request counter:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_tokens_total{provider="openai",model="gpt-4"} 30`) {
		t.Errorf("missing tokens counter:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_input_tokens_total{provider="openai",model="gpt-4"} 10`) {
		t.Errorf("missing input tokens:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_output_tokens_total{provider="openai",model="gpt-4"} 20`) {
		t.Errorf("missing output tokens:\n%s", out)
	}
}

func TestLLMCollectorMiddlewareError(t *testing.T) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	mockComplete := func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
		return nil, llmtrace.ErrRateLimit
	}

	mw := collector.Middleware()
	wrapped := mw(mockComplete)

	ctx := ContextWithProvider(context.Background(), "anthropic")
	_, err := wrapped(ctx, &llmtrace.Request{Model: "claude-3"})
	if err == nil {
		t.Fatal("expected error")
	}

	out := reg.WritePrometheus()
	if !strings.Contains(out, `llmtrace_errors_total{provider="anthropic",error_type="rate_limit"} 1`) {
		t.Errorf("missing error counter:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_requests_total{provider="anthropic",model="claude-3"} 1`) {
		t.Errorf("request should be counted even on error:\n%s", out)
	}
}

func TestLLMCollectorStreamMiddleware(t *testing.T) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	mockStream := func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
		ch := make(chan llmtrace.StreamChunk, 3)
		ch <- llmtrace.StreamChunk{Content: "Hel"}
		ch <- llmtrace.StreamChunk{Content: "lo"}
		ch <- llmtrace.StreamChunk{
			Content: "",
			Usage: &llmtrace.Usage{
				InputTokens:  5,
				OutputTokens: 10,
				TotalTokens:  15,
			},
		}
		close(ch)
		return ch, nil
	}

	mw := collector.StreamMiddleware()
	wrapped := mw(mockStream)

	ctx := ContextWithProvider(context.Background(), "gemini")
	ch, err := wrapped(ctx, &llmtrace.Request{Model: "gemini-pro"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain the channel
	for range ch {
	}

	out := reg.WritePrometheus()
	if !strings.Contains(out, `llmtrace_stream_chunks_total{provider="gemini",model="gemini-pro"} 3`) {
		t.Errorf("missing stream chunks counter:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_tokens_total{provider="gemini",model="gemini-pro"} 15`) {
		t.Errorf("missing tokens counter:\n%s", out)
	}
	if !strings.Contains(out, `llmtrace_requests_total{provider="gemini",model="gemini-pro"} 1`) {
		t.Errorf("missing request counter:\n%s", out)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"rate limit", llmtrace.ErrRateLimit, "rate_limit"},
		{"auth", llmtrace.ErrAuth, "auth"},
		{"invalid request", llmtrace.ErrInvalidRequest, "invalid_request"},
		{"server error", llmtrace.ErrServerError, "server_error"},
		{"unknown", context.Canceled, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.err)
			if got != tt.want {
				t.Errorf("classifyError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestContextWithProvider(t *testing.T) {
	ctx := context.Background()
	if got := getProviderFromContext(ctx); got != "unknown" {
		t.Errorf("empty context = %q, want unknown", got)
	}

	ctx = ContextWithProvider(ctx, "openai")
	if got := getProviderFromContext(ctx); got != "openai" {
		t.Errorf("with provider = %q, want openai", got)
	}
}

func TestRecordCost(t *testing.T) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	collector.RecordCost("openai", "gpt-4", 0.05)
	collector.RecordCost("openai", "gpt-4", 0.03)

	out := reg.WritePrometheus()
	if !strings.Contains(out, `llmtrace_cost_usd_total{provider="openai",model="gpt-4"} 0.08`) {
		t.Errorf("missing cost counter:\n%s", out)
	}
}

func TestDurationBuckets(t *testing.T) {
	buckets := DurationBuckets()
	if len(buckets) == 0 {
		t.Error("empty duration buckets")
	}
	// Check sorted
	for i := 1; i < len(buckets); i++ {
		if buckets[i] <= buckets[i-1] {
			t.Errorf("buckets not sorted: %v[%d]=%f <= %v[%d]=%f", buckets, i, buckets[i], buckets, i-1, buckets[i-1])
		}
	}
}

func TestTokenBuckets(t *testing.T) {
	buckets := TokenBuckets()
	if len(buckets) == 0 {
		t.Error("empty token buckets")
	}
}

func TestActiveRequestsGauge(t *testing.T) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	// Simulate middleware tracking active requests
	provider := "openai"

	// Before request
	out := reg.WritePrometheus()
	// Gauge is 0, so with With() it's registered but value is 0
	g := collector.ActiveRequests.With(provider)
	if g.Value() != 0 {
		t.Errorf("initial active = %v, want 0", g.Value())
	}

	g.Inc()
	if g.Value() != 1 {
		t.Errorf("after Inc = %v, want 1", g.Value())
	}
	g.Dec()
	if g.Value() != 0 {
		t.Errorf("after Dec = %v, want 0", g.Value())
	}

	_ = out // suppress unused warning
}

func TestPrometheusFormatValidity(t *testing.T) {
	reg := NewRegistry("app")
	cv := reg.RegisterCounter("http_requests_total", "Total HTTP requests", []string{"method", "handler"})
	gv := reg.RegisterGauge("in_flight", "In-flight requests", nil)
	hv := reg.RegisterHistogram("request_duration_seconds", "Request duration", []string{"method"}, []float64{0.01, 0.1, 1})

	cv.With("GET", "/api").Inc()
	gv.With().Set(3)
	hv.With("POST").Observe(0.5)

	out := reg.WritePrometheus()

	// Check basic format requirements
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Non-comment lines should have space between name and value
		if !strings.Contains(line, " ") && !strings.Contains(line, "{") {
			t.Errorf("invalid metric line (no space): %q", line)
		}
	}

	// Check that HELP and TYPE come before samples
	helpIdx := strings.Index(out, "# HELP app_http_requests_total")
	typeIdx := strings.Index(out, "# TYPE app_http_requests_total")
	sampleIdx := strings.Index(out, "app_http_requests_total{")
	if helpIdx > typeIdx || typeIdx > sampleIdx {
		t.Error("HELP/TYPE/sample ordering incorrect")
	}
}

func BenchmarkCounterInc(b *testing.B) {
	c := &Counter{}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkGaugeInc(b *testing.B) {
	g := &Gauge{}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Inc()
		}
	})
}

func BenchmarkHistogramObserve(b *testing.B) {
	h := NewHistogram(DurationBuckets())
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.Observe(0.1)
		}
	})
}

func BenchmarkWritePrometheus(b *testing.B) {
	reg := NewRegistry("llmtrace")
	collector := NewLLMCollector(reg)

	// Pre-populate with some data
	for i := 0; i < 10; i++ {
		model := "model-" + string(rune('A'+i))
		collector.RequestTotal.With("openai", model).Inc()
		collector.TokensTotal.With("openai", model).Add(100)
		collector.RequestDuration.With("openai", model).Observe(0.1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.WritePrometheus()
	}
}
