# LLMTrace

[![Go Reference](https://pkg.go.dev/badge/github.com/atop0914/llmtrace.svg)](https://pkg.go.dev/github.com/atop0914/llmtrace)
[![CI](https://github.com/atop0914/llmtrace/actions/workflows/ci.yml/badge.svg)](https://github.com/atop0914/llmtrace/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**OpenTelemetry-native LLM Observability SDK for Go**

LLMTrace wraps LLM client calls with OpenTelemetry spans, capturing token usage, latency, cost, and request/response metadata — following the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **OpenTelemetry native** — standard `gen_ai.*` span attributes, OTLP export
- **Multi-provider** — OpenAI, Anthropic, Gemini, Ollama, Azure, OpenAI-compatible
- **Cost tracking** — automatic USD cost calculation per request
- **Streaming support** — trace SSE streaming responses
- **Retry with backoff** — configurable exponential backoff for transient errors
- **Rate limiting** — token bucket rate limiter for API call throttling
- **Circuit breaker** — automatic failure detection with half-open recovery
- **Response caching** — LRU cache with TTL for duplicate requests
- **Provider fallback** — automatic failover between providers on error
- **Cost budgeting** — daily/weekly/monthly token budget alerts
- **Middleware pattern** — add logging, hooks, and custom interceptors
- **Structured logging** — `log/slog` integration with content sanitization
- **Evaluation framework** — composable response quality checks (length, JSON, regex, custom)
- **Guardrails** — real-time input/output validation with 14 built-in rules (block/warn severity, fail-open mode)
- **Token counting** — estimate tokens, validate context windows, manage costs before API calls
- **Prompt templates** — versioned templates with variable substitution and A/B testing
- **Session tracking** — multi-turn conversation history with automatic token counting
- **Token middleware** — observability middleware for automatic token usage and cost tracking
- **Trace export** — export traces to JSON/CSV files with batch buffering and auto-rotation
- **Prometheus metrics** — built-in metrics collector with `/metrics` endpoint
- **Real-time dashboard** — web UI with Chart.js, SSE updates, 6 monitoring pages
- **Unified errors** — consistent error types across all providers
- **Config hot-reload** — watch config file changes and apply without restart
- **Provider load balancing** — distribute requests across multiple instances with round-robin, least-latency, random, or weighted strategies
- **Streaming metrics** — track TTFT, inter-chunk latency percentiles, and tokens-per-second for streaming responses
- **Text embeddings** — generate vector embeddings via OpenAI API with batch chunking, similarity search, and an in-memory vector index for RAG pipelines
- **Content moderation** — blocklist, regex, and PII detection with input/output middleware
- **HTTP framework adapters** — `net/http` middleware with OTel spans, request IDs, and response metadata headers
- **Local proxy server** — OpenAI-compatible proxy with multi-provider routing and full observability
- **Health checks** — Kubernetes-style liveness and readiness probes with pluggable checks
- **Correlation IDs** — automatic request ID propagation across HTTP boundaries

## Installation

```bash
go get github.com/atop0914/llmtrace@latest
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/atop0914/llmtrace"
    "github.com/atop0914/llmtrace/provider/openai"
)

func main() {
    // Create a provider
    provider := openai.New(openai.WithAPIKey("sk-..."))

    // Create a tracer with cost tracking
    tracer := llmtrace.NewTracer("my-service",
        llmtrace.WithProvider("openai"),
        llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
    )

    // Make a completion call
    resp, err := tracer.Chat(context.Background(), &llmtrace.Request{
        Model:    "gpt-4o",
        Messages: []llmtrace.Message{{Role: "user", Content: "Hello!"}},
    }, provider)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Response: %s\n", resp.Content)
    fmt.Printf("Tokens: %d (in: %d, out: %d)\n",
        resp.Usage.TotalTokens, resp.Usage.InputTokens, resp.Usage.OutputTokens)
}
```

## Providers

| Provider | Package | Status |
|----------|---------|--------|
| OpenAI | `provider/openai` | ✅ |
| Anthropic | `provider/anthropic` | ✅ |
| Gemini | `provider/gemini` | ✅ |
| Ollama | `provider/ollama` | ✅ |
| Azure OpenAI | `provider/azure` | ✅ |
| OpenAI-compatible | `provider/compat` | ✅ |

### Creating a Provider

```go
// OpenAI
provider := openai.New(
    openai.WithAPIKey("sk-..."),
    openai.WithBaseURL("https://api.openai.com/v1"), // optional, for proxies
    openai.WithDefaultModel("gpt-4o"),
)

// Anthropic
provider := anthropic.New(
    anthropic.WithAPIKey("sk-ant-..."),
)

// Gemini
provider := gemini.New(
    gemini.WithAPIKey("..."),
)

// Ollama (local LLMs)
provider := ollama.New(
    ollama.WithBaseURL("http://localhost:11434"),
    ollama.WithDefaultModel("llama3"),
)

// Azure OpenAI
provider := azure.New(
    azure.WithEndpoint("https://myresource.openai.azure.com"),
    azure.WithAPIKey("..."),
    azure.WithDeployment("gpt-4o"),
)

// OpenAI-compatible (any provider with OpenAI API format)
provider := compat.New(
    compat.WithBaseURL("https://api.example.com/v1"),
    compat.WithAPIKey("..."),
    compat.WithDefaultModel("my-model"),
)
```

## Streaming

```go
ch, err := tracer.ChatStream(ctx, &llmtrace.Request{
    Model:    "gpt-4o",
    Messages: []llmtrace.Message{{Role: "user", Content: "Write a poem."}},
}, provider)

for chunk := range ch {
    if chunk.Error != nil {
        log.Printf("stream error: %v", chunk.Error)
        break
    }
    fmt.Print(chunk.Content)
}
```

### Streaming Metrics

Monitor streaming performance with real-time TTFT, inter-chunk latency, and throughput tracking:

```go
import "github.com/atop0914/llmtrace/streammetric"

// Method 1: Direct collector usage
collector := streammetric.NewCollector()
ch, _ := tracer.ChatStream(ctx, req, provider)
wrappedCh := collector.Wrap(ch)

for chunk := range wrappedCh {
    fmt.Print(chunk.Content)
}

m := collector.Metrics()
fmt.Printf("TTFT: %v | TPS: %.1f | Chunks: %d\n", m.TTFT, m.TokensPerSecond, m.ChunkCount)
fmt.Printf("ICL P50: %v | P99: %v\n", m.P50InterChunkLatency, m.P99InterChunkLatency)

// Method 2: Automatic via StreamMiddleware
metricsMW := streammetric.WithStreamMetrics(func(req *llmtrace.Request, m streammetric.Metrics) {
    slog.Info("stream complete",
        "model", req.Model,
        "ttft", m.TTFT,
        "tps", m.TokensPerSecond,
        "p99_icl", m.P99InterChunkLatency,
    )
})
streamFn := llmtrace.ChainStream(metricsMW)(provider.Stream)
```

## Rate Limiting

Control API call rates with the token bucket rate limiter:

```go
// Create a limiter: 10 requests/second, burst of 20
lim := llmtrace.NewLimiter(10, 20)

// Use as middleware
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithRateLimit(lim)),
)

// Or use the ChatOption shorthand
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallRateLimit(llmtrace.RateLimitConfig{
        Rate:  10,  // 10 requests per second
        Burst: 20,  // burst up to 20
    }),
)
```

Non-blocking checks:

```go
if lim.Allow() {
    // proceed immediately
}

// Blocking wait with context
if err := lim.Wait(ctx); err != nil {
    // context canceled or rate limit exceeded
}
```

## Circuit Breaker

Protect against cascading failures with automatic circuit breaking:

```go
cb := llmtrace.NewCircuitBreaker(llmtrace.CircuitBreakerConfig{
    MaxFailures:      5,                // failures before opening
    Timeout:          30 * time.Second, // open state duration
    MaxRequests:      3,                // half-open test requests
    FailureThreshold: 50,               // failure rate % to trip
})

resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithCircuitBreaker(cb)),
)
```

## Response Caching

Cache identical requests to reduce API costs:

```go
cache := llmtrace.NewResponseCache(llmtrace.CacheConfig{
    MaxSize: 1000,           // max cached responses
    TTL:     10 * time.Minute, // cache expiry
})

resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithCache(cache)),
)
```

## Provider Fallback

Automatically failover to backup providers:

```go
router := llmtrace.NewFallbackRouter(llmtrace.FallbackConfig{
    Providers: []llmtrace.ProviderEntry{{
        Name:     "openai",
        Provider: openaiProvider,
        Priority: 1,
    }, {
        Name:     "anthropic",
        Provider: anthropicProvider,
        Priority: 2,
    }},
    MaxRetries: 2,
    Cooldown:   60 * time.Second,
})

resp, err := router.Complete(ctx, req)
```

## Cost Budgeting

Set daily/weekly/monthly token budgets with alerts:

```go
budget := llmtrace.NewBudget(llmtrace.BudgetConfig{
    DailyLimit:   100000,  // 100K tokens/day
    WeeklyLimit:  500000,  // 500K tokens/week
    MonthlyLimit: 2000000, // 2M tokens/month
    OnExceed: func(info llmtrace.BudgetExceedInfo) {
        log.Printf("Budget exceeded: %s limit reached (%d/%d)",
            info.Period, info.Used, info.Limit)
    },
})

resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithBudget(budget)),
)
```

## Load Balancing

Distribute requests across multiple provider instances:

```go
import "github.com/atop0914/llmtrace/loadbalancer"

// Create multiple provider instances (e.g., different API keys)
openai1 := openai.New(openai.WithAPIKey("sk-key-1"))
openai2 := openai.New(openai.WithAPIKey("sk-key-2"))

// Create a load balancer
lb := loadbalancer.New(
    loadbalancer.WithStrategy(loadbalancer.RoundRobin),
    loadbalancer.WithEndpoints(
        loadbalancer.NewEndpoint("key-1", openai1),
        loadbalancer.NewEndpoint("key-2", openai2),
    ),
)

// Use as a provider — seamless integration
resp, err := tracer.Chat(ctx, req, lb)
```

**Strategies:**
| Strategy | Description |
|----------|-------------|
| `RoundRobin` | Sequential distribution across endpoints |
| `LeastLatency` | Route to the fastest endpoint (EMA-based) |
| `Random` | Random healthy endpoint selection |
| `Weighted` | Proportional distribution by weight |

**Features:**
- Automatic health tracking — endpoints marked unhealthy after 3 consecutive failures
- Failover — automatically tries another endpoint on error
- Health probes — periodic checks to recover unhealthy endpoints
- Per-endpoint stats — latency, error rate, call count


## Middleware

Add custom behavior to the request pipeline:

```go
// Logging hook
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(
        llmtrace.WithCompleteHook(func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response, err error) {
            log.Printf("model=%s tokens=%d latency=%v", resp.Model, resp.Usage.TotalTokens, resp.Latency)
        }),
    ),
)

// Timing middleware
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(
        llmtrace.WithTiming(func(req *llmtrace.Request, durationMS float64) {
            metrics.Observe("llm_latency_ms", durationMS)
        }),
    ),
)

// Chain multiple middlewares
chain := llmtrace.Chain(
    llmtrace.WithRateLimit(lim),
    llmtrace.WithCompleteHook(loggingHook),
)
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(chain),
)
```
## Prometheus Metrics

Expose LLM metrics for Prometheus scraping:

```go
import "github.com/atop0914/llmtrace/metrics"

// Create a registry and collector
reg := metrics.NewRegistry("llmtrace")
collector := metrics.NewLLMCollector(reg)

// Use as middleware
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(collector.Middleware()),
)

// Serve metrics endpoint
http.Handle("/metrics", metrics.Handler(reg))
log.Fatal(http.ListenAndServe(":2112", nil))
```

### Exposed Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `llmtrace_requests_total` | counter | provider, model | Total LLM requests |
| `llmtrace_request_duration_seconds` | histogram | provider, model | Request latency |
| `llmtrace_tokens_total` | counter | provider, model | Total tokens processed |
| `llmtrace_input_tokens_total` | counter | provider, model | Input tokens sent |
| `llmtrace_output_tokens_total` | counter | provider, model | Output tokens received |
| `llmtrace_cost_usd_total` | counter | provider, model | Cumulative cost in USD |
| `llmtrace_active_requests` | gauge | provider | In-flight requests |
| `llmtrace_errors_total` | counter | provider, error_type | Failed requests |
| `llmtrace_stream_chunks_total` | counter | provider, model | Stream chunks received |

## Dashboard

LLMTrace includes a built-in real-time dashboard with 6 monitoring pages:

| Page | Description |
|------|-------------|
| **Overview** | Total requests, tokens, cost, errors, active connections |
| **Providers** | Per-provider breakdown with health, latency percentiles, cost efficiency |
| **Models** | Per-model analysis with health scores, rankings, side-by-side comparison |
| **Latency** | Latency distribution histogram by provider/model |
| **Costs** | Cost trends, provider breakdown, daily averages |
| **Errors** | Error rate trends, recent error traces, error classification |

### Quick Start

```go
import (
    "github.com/atop0914/llmtrace/dashboard"
    "github.com/atop0914/llmtrace/metrics"
)

// Create metrics registry
reg := metrics.NewRegistry("llmtrace")
collector := metrics.NewLLMCollector(reg)

// Use collector as middleware (see Prometheus Metrics section)

// Create and serve dashboard
dash := dashboard.Handler(reg, dashboard.Config{
    SSEInterval: 2 * time.Second,    // SSE push interval (default: 2s)
    TraceStore:  traceStore,         // optional: enables trace explorer
})
log.Fatal(http.ListenAndServe(":8080", dash))
```

### Dashboard Pages

**Overview** — High-level metrics at a glance:
- Total requests, tokens (input/output), cost
- Active requests, error count, average latency
- Provider and model counts

**Providers** — Provider health analysis:
- Error rate, health score (0-100)
- Latency percentiles (P50/P95/P99)
- Cost per 1K tokens, throughput (tokens/sec)
- Per-model breakdown within each provider

**Models** — Model performance analysis:
- Health scores with status badges (healthy/degraded/unhealthy)
- Rankings by cost efficiency, latency, throughput, reliability
- Side-by-side model comparison (query: `?models=openai:gpt-4o,anthropic:claude-3`)

**Costs** — Cost monitoring:
- Daily cost trend chart
- Provider cost breakdown with percentages
- Average daily cost stats

**Errors** — Error monitoring:
- Error rate trend over time
- Recent error traces with error type classification
- Error distribution by type and provider

### SSE Real-Time Updates

The dashboard uses Server-Sent Events for live updates:

```javascript
// Frontend (built-in, no setup needed)
const events = new EventSource('/api/events');
events.addEventListener('overview', (e) => {
    const data = JSON.parse(e.data);
    // Update overview metrics
});
```

SSE events are pushed at the configured `SSEInterval` (default: 2 seconds).
Event types: `overview`, `providers`, `models`, `latency`, `costs`, `errors`.

## Trace Export

Export traces to files for compliance, offline analysis, and integration with external tools:

```go
import "github.com/atop0914/llmtrace/traceexport"

// Export to JSON file
exp, err := traceexport.NewJSONExporter("traces.json", traceexport.WithIndent())
if err != nil {
    log.Fatal(err)
}
defer exp.Close()

traces := store.Query(llmtrace.TraceQuery{
    Since:    time.Now().Add(-24 * time.Hour),
    Provider: "openai",
})
exp.Export(ctx, traces)

// Export to CSV with header
csv, _ := traceexport.NewCSVExporter("traces.csv", traceexport.WithCSVHeader())
defer csv.Close()
csv.Export(ctx, traces)

// Batch export with periodic flush
batch := traceexport.NewBatchExporter(traceexport.BatchConfig{
    Exporter:     exp,
    Interval:     5 * time.Minute,
    MaxBatchSize: 1000,
})
batch.Start(ctx)
defer batch.Stop()

// Add traces as they come in
batch.Add(trace1, trace2, ...)
```

### Auto-rotating File Export

Automatically rotate output files by size or age:

```go
exp, err := traceexport.NewRotateExporter(traceexport.RotateConfig{
    Dir:      "/var/log/llmtrace",
    Prefix:   "traces",
    Format:   "json",
    MaxSize:  100 * 1024 * 1024, // rotate at 100MB
    MaxAge:   24 * time.Hour,    // rotate daily
    MaxFiles: 10,                // keep last 10 files
})
```

## Error Handling

LLMTrace provides unified error types across all providers:

```go
resp, err := tracer.Chat(ctx, req, provider)
if err != nil {
    // Check specific error types
    switch {
    case llmtrace.IsRateLimit(err):
        log.Println("rate limited, try again later")
    case llmtrace.IsAuthError(err):
        log.Println("check your API key")
    case llmtrace.IsServerError(err):
        log.Println("provider error, will retry")
    case llmtrace.IsInvalidRequest(err):
        log.Println("bad request parameters")
    default:
        log.Printf("unknown error: %v", err)
    }

    // Access structured error details
    var pe *llmtrace.ProviderError
    if errors.As(err, &pe) {
        log.Printf("provider=%s status=%d code=%s type=%s",
            pe.Provider, pe.StatusCode, pe.Code, pe.Type)
    }
}
```

### Transient Error Detection

```go
if llmtrace.IsTransient(err) {
    // Error is likely temporary (rate limit, server error, timeout)
    // Retry logic may succeed
}
```

## Configuration

### Tracer Options

```go
tracer := llmtrace.NewTracer("my-service",
    llmtrace.WithProvider("openai"),              // set provider name
    llmtrace.WithCostCalculator(costCalc),         // enable cost tracking
)
```

### Provider Options

```go
provider := openai.New(
    openai.WithAPIKey("sk-..."),                   // API key
    openai.WithBaseURL("https://proxy.example.com"), // custom endpoint
    openai.WithDefaultModel("gpt-4o"),             // default model
    openai.WithMaxRetries(3),                      // provider-level retries
)
```

### Cost Calculator

```go
calc := llmtrace.NewCostCalculator()

// Add custom model pricing
calc.SetPrice("my-model", llmtrace.CostEntry{
    InputCostPer1K:  0.001,
    OutputCostPer1K: 0.002,
})
```
## Architecture

```
+---------------------------------------------------------+
|                      Your Application                    |
+---------------------------------------------------------+
|                    llmtrace.Tracer                       |
|  +----------+  +----------+  +----------+               |
|  | Complete  |  |  Stream  |  |   Chat   |               |
|  +----+-----+  +----+-----+  +----+-----+               |
|       |              |              |                    |
|  +----v--------------v--------------v----+               |
|  |         Middleware Chain                |               |
|  |  +------+ +------+ +------+ +------+ |               |
|  |  |Rate  | |Retry | |Cache | |Break | |               |
|  |  |Limit | |      | |      | |er    | |               |
|  |  +------+ +------+ +------+ +------+ |               |
|  |  +------+ +------+ +------+ +------+ |               |
|  |  |Budget| |Slog  | |Eval  | |Hooks | |               |
|  |  +------+ +------+ +------+ +------+ |               |
|  +----------------+---------------------+               |
|                   |                                      |
|  +----------------v---------------------+               |
|  |          Provider Interface            |               |
|  |  +--------+ +----------+ +--------+  |               |
|  |  | OpenAI | |Anthropic | | Gemini |  |               |
|  |  +--------+ +----------+ +--------+  |               |
|  |  +--------+ +----------+ +--------+  |               |
|  |  | Ollama | |  Azure   | | Compat |  |               |
|  |  +--------+ +----------+ +--------+  |               |
|  +--------------------------------------+               |
|                                                          |
|  +--------------------------------------+               |
|  |     OpenTelemetry Spans (gen_ai.*)    |               |
|  |  * gen_ai.system  * gen_ai.usage.*    |               |
|  |  * gen_ai.request * gen_ai.response   |               |
|  +--------------------------------------+               |
|                                                          |
|  +--------------------------------------+               |
|  |     Prometheus Metrics (/metrics)     |               |
|  |  * requests_total  * tokens_total     |               |
|  |  * duration        * cost_usd_total   |               |
|  +--------------------------------------+               |
|                                                          |
|  +--------------------------------------+               |
|  |     Dashboard (HTTP :8080)            |               |
|  |  * Overview  * Providers * Models     |               |
|  |  * Latency   * Costs     * Errors     |               |
|  |  * SSE real-time updates              |               |
|  +--------------------------------------+               |
+---------------------------------------------------------+
```

## Examples

See the [examples/](examples/) directory:

| Example | Description |
|---------|-------------|
| [basic](examples/basic/) | Basic usage with OpenAI provider |
| [dashboard](examples/dashboard/) | Web dashboard with real-time metrics |
| [middleware](examples/middleware/) | Middleware chain with retry, rate limit, logging |
| [streaming](examples/streaming/) | Streaming responses with error handling |
| [streammetric](examples/streammetric/) | Streaming performance metrics (TTFT, ICL, TPS) |
| [batch](examples/batch/) | Async batch request execution with concurrency control |
| [embedding](examples/embedding/) | Text embeddings and vector similarity search |
| [propagation](examples/propagation/) | W3C Trace Context distributed tracing propagation |
| [judge](examples/judge/) | LLM-as-judge evaluation framework |
| [semcache](examples/semcache/) | Semantic caching with embedding similarity |
| [tokencount](examples/tokencount/) | Token estimation, context window management, cost comparison |
| [prompt](examples/prompt/) | Versioned prompt templates with A/B testing |
| [session](examples/session/) | Multi-turn conversation session tracking |
| [proxy](examples/proxy/) | OpenAI-compatible local proxy server with multi-provider routing |
| [healthcheck](examples/healthcheck/) | Kubernetes-style liveness and readiness probes |
| [correlation](examples/correlation/) | Request/response correlation ID propagation |
| [adapters](examples/adapters/) | HTTP framework adapters with OTel tracing and response headers |
| [moderation](examples/moderation/) | Content moderation with blocklists, PII detection, and regex rules |

Run an example:

```bash
# Basic usage
go run ./examples/basic

# Dashboard (open http://localhost:8080)
go run ./examples/dashboard

# Middleware demo
go run ./examples/middleware

# Streaming demo
go run ./examples/streaming

# Streaming metrics
go run ./examples/streammetric

# Batch requests
go run ./examples/batch

# Text embeddings
go run ./examples/embedding

# Distributed tracing propagation
go run ./examples/propagation

# LLM-as-judge evaluation
go run ./examples/judge

# Semantic caching
go run ./examples/semcache

# OpenAI-compatible proxy
go run ./examples/proxy

# Health check & readiness probes
go run ./examples/healthcheck

# Correlation ID middleware
go run ./examples/correlation

# HTTP framework adapters
go run ./examples/adapters

# Content moderation
go run ./examples/moderation
```

## Batch Requests

Execute multiple LLM requests concurrently with configurable parallelism:

```go
import "github.com/atop0914/llmtrace/batch"

// Create a batch executor
executor := batch.New(provider,
    batch.WithMaxConcurrency(5),
    batch.WithContinueOnError(true),  // don't stop on individual failures
    batch.WithTimeout(60 * time.Second),
    batch.WithOnProgress(func(info batch.ProgressInfo) {
        log.Printf("Progress: %d/%d completed", info.Completed, info.Total)
    }),
)

// Add requests
executor.Add(batch.Item{ID: "q1", Request: &llmtrace.Request{Model: "gpt-4o", Messages: []llmtrace.Message{{Role: "user", Content: "What is Go?"}}}})
executor.Add(batch.Item{ID: "q2", Request: &llmtrace.Request{Model: "gpt-4o", Messages: []llmtrace.Message{{Role: "user", Content: "What is Rust?"}}}})

// Execute all requests concurrently
results, err := executor.Execute(ctx)

// Check aggregate metrics
fmt.Printf("Total tokens: %d, Avg latency: %v\n",
    results.Metrics.TotalTokens, results.Metrics.AvgLatency)

for _, r := range results.Items {
    if r.Error != nil {
        log.Printf("  %s: ERROR %v", r.ID, r.Error)
    } else {
        log.Printf("  %s: %s (tokens: %d)", r.ID, r.Response.Content[:50], r.Response.Usage.TotalTokens)
    }
}
```

## Distributed Tracing Propagation

Propagate trace context across service boundaries using W3C Trace Context:

```go
import "github.com/atop0914/llmtrace/propagation"

prop := propagation.New()

// Server side: extract incoming trace context from HTTP headers
downstreamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    spanCtx, ok := propagation.SpanContextFromContext(r.Context())
    if ok {
        log.Printf("Received trace: %s", spanCtx.TraceID())
    }
})
http.Handle("/api", propagation.Middleware(prop)(downstreamHandler))

// Client side: inject trace context into outgoing requests
client := &http.Client{
    Transport: propagation.ClientMiddleware(prop)(http.DefaultTransport),
}

// Set trace context in Go context
spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID:    traceID,
    SpanID:     spanID,
    TraceFlags: trace.FlagsSampled,
})
ctx := propagation.ContextWithSpanContext(context.Background(), spanCtx)

req, _ := http.NewRequestWithContext(ctx, "GET", "http://downstream/api", nil)
resp, _ := client.Do(req)
```

**Supported carriers:**
- `HTTPCarrier` — HTTP headers (via `InjectIntoHTTP` / `ExtractFromHTTP`)
- `MapCarrier` — generic key-value map (for gRPC metadata, custom transports)

## Semantic Caching

Cache LLM responses based on semantic similarity rather than exact matches:

```go
import "github.com/atop0914/llmtrace/semcache"

// Create a semantic cache with an embedding provider
cache := semcache.New(
    semcache.WithEmbeddingProvider(embeddingProvider),
    semcache.WithSimilarityThreshold(0.92),  // cosine similarity threshold
    semcache.WithMaxEntries(1000),
    semcache.WithTTL(30 * time.Minute),
)

// Use as middleware — automatically caches and retrieves semantically similar responses
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(cache.Middleware()),
)
```

**How it works:**
1. Each request is converted to an embedding vector
2. On cache hit (cosine similarity >= threshold), returns cached response without API call
3. On cache miss, calls the LLM and stores the response with its embedding
4. Supports TTL-based expiration and LRU eviction

## Text Embeddings

Generate vector embeddings for text with similarity search:

```go
import "github.com/atop0914/llmtrace/embedding"

// Create an embedding provider
provider := embedding.NewOpenAI(
    embedding.WithAPIKey("sk-..."),
    embedding.WithModel("text-embedding-3-small"),
)

// Generate embedding
result, _ := provider.Embed(ctx, "The quick brown fox")
fmt.Printf("Dimensions: %d, Tokens: %d\n", len(result.Vector), result.Usage.TotalTokens)

// Build a vector index for similarity search
index := embedding.NewIndex(embedding.WithMetric(embedding.Cosine))
index.Add("doc1", result.Vector, map[string]string{"source": "example"})
index.Add("doc2", result2.Vector, map[string]string{"source": "docs"})

// Search for similar vectors
results, _ := index.Search(queryVector, 5)  // top 5
for _, r := range results {
    fmt.Printf("  %s: similarity=%.4f\n", r.ID, r.Score)
}

// Batch embedding with automatic chunking
texts := []string{"text1", "text2", /* ... hundreds of texts ... */}
batchResults, _ := provider.EmbedBatch(ctx, texts)
```

**Features:**
- Supports `text-embedding-3-small`, `text-embedding-3-large`, `text-embedding-ada-002`
- Configurable dimensionality reduction for `text-embedding-3-*` models
- Automatic batch chunking for large inputs (configurable `MaxBatchSize`)
- Thread-safe in-memory vector index with cosine, Euclidean, and dot product metrics

## Structured Logging

Add structured logging to LLM calls using Go's `log/slog`:

```go
import "log/slog"

// Configure slog middleware
cfg := llmtrace.SlogConfig{
    Logger:         slog.Default(),  // or custom logger
    Level:          slog.LevelInfo,
    ErrorLevel:     slog.LevelError,
    LogRequest:     true,
    LogResponse:    true,
    LogErrors:      true,
    SanitizeContent: true,
}

// Use with completion calls
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithSlog(cfg)),
)

// Use with streaming calls
ch, err := tracer.ChatStream(ctx, req, provider,
    llmtrace.WithCallMiddleware(llmtrace.WithStreamSlog(cfg)),
)
```

### Log Output Examples

Request start:
```json
{
  "level": "INFO",
  "msg": "llm request started",
  "model": "gpt-4o",
  "message_count": 3,
  "max_tokens": 1000,
  "temperature": 0.7
}
```

Request completion:
```json
{
  "level": "INFO",
  "msg": "llm request completed",
  "model": "gpt-4o",
  "provider": "openai",
  "latency": 1234567890,
  "input_tokens": 150,
  "output_tokens": 50,
  "total_tokens": 200,
  "finish_reason": "stop",
  "response_id": "resp-abc123"
}
```

Error with provider details:
```json
{
  "level": "ERROR",
  "msg": "llm request failed",
  "model": "gpt-4o",
  "latency": 500000000,
  "error": "openai: rate limit exceeded",
  "provider": "openai",
  "status_code": 429,
  "error_code": "rate_limit_exceeded",
  "error_type": "rate_limit"
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Logger` | `*slog.Logger` | `slog.Default()` | Custom logger instance |
| `Level` | `slog.Level` | `slog.LevelInfo` | Log level for success messages |
| `ErrorLevel` | `slog.Level` | `slog.LevelError` | Log level for error messages |
| `LogRequest` | `bool` | `true` | Log request start with model and message count |
| `LogResponse` | `bool` | `true` | Log completion with tokens and latency |
| `LogErrors` | `bool` | `true` | Log errors with provider details |
| `SanitizeContent` | `bool` | `true` | Only log message count, not content |

## Evaluation

Automatically evaluate LLM response quality with composable evaluators:

```go
import "github.com/atop0914/llmtrace/eval"

// Create an evaluation suite
suite := eval.NewSuite("quality-checks",
    eval.MinLength(10),           // response must be at least 10 chars
    eval.MaxLength(4000),         // response must be at most 4000 chars
    eval.NonEmpty(),              // response must not be empty
    eval.ValidJSON(),             // response must be valid JSON
    eval.FinishReason("stop"),    // must finish normally
    eval.Contains("answer"),      // must contain keyword
    eval.RegexMatch(`\d{3}-\d{4}`), // must match pattern
    eval.MaxLatency(5*time.Second), // must complete in 5s
    eval.TokenLimit(1000),        // must use <= 1000 tokens
    eval.Custom("no_apology", func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) (bool, string) {
        ok := !strings.Contains(resp.Content, "I'm sorry")
        return ok, "response does not contain apology"
    }),
)

// Run as middleware (non-blocking, for monitoring)
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(suite.Middleware()),
)

// Or validate (returns error if any evaluator fails)
result, err := suite.Validate(ctx, req, resp)
if err != nil {
    log.Printf("eval failed: %v", err)
}
```

### Built-in Evaluators

| Evaluator | Description |
|-----------|-------------|
| `MinLength(n)` | Content has at least n characters |
| `MaxLength(n)` | Content has at most n characters |
| `NonEmpty()` | Content is not empty/whitespace |
| `Contains(s)` | Content contains substring (case-sensitive) |
| `ContainsAny(s...)` | Content contains at least one substring |
| `NotContains(s)` | Content does not contain substring |
| `ValidJSON()` | Content is valid JSON |
| `FinishReason(reasons...)` | Finish reason matches expected values |
| `RegexMatch(pattern)` | Content matches regex pattern |
| `TokenLimit(n)` | Total tokens <= n |
| `MaxLatency(d)` | Latency <= duration |
| `ResponseID()` | Response ID is non-empty |
| `Custom(name, fn)` | User-defined evaluator |
## Guardrails

Enforce input/output policies in real-time as middleware. Unlike Eval (post-hoc quality checks) and Sanitizer (PII redaction), Guardrails validate **before** the LLM call and **after** the response:

```go
import "github.com/atop0914/llmtrace/guardrails"

gate := guardrails.NewGate(
    guardrails.WithInputRules(
        guardrails.MaxPromptLength(4096),
        guardrails.BlockedTerms([]string{"jailbreak", "ignore instructions"}),
        guardrails.RequiredRoles(llmtrace.RoleSystem, llmtrace.RoleUser),
    ),
    guardrails.WithOutputRules(
        guardrails.MinResponseLength(10),
        guardrails.RequiredFinishReason("stop"),
        guardrails.MaxTokenUsage(10000),
    ),
)

// Optional: log violations
gate.OnViolation(func(v guardrails.Violation) {
    log.Printf("guardrail violation: %s (%s) - %s", v.RuleName, v.Severity, v.Message)
})

// Use as middleware
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(gate.Middleware()),
)
```

### Built-in Rules

| Rule | Side | Severity | Description |
|------|------|----------|-------------|
| `MaxPromptLength(n)` | Input | Block | Total prompt chars <= n |
| `MinPromptLength(n)` | Input | Warn | Total prompt chars >= n |
| `MaxMessages(n)` | Input | Block | Message count <= n |
| `BlockedTerms(terms)` | Input | Block | Prompt must not contain terms |
| `WarnedTerms(terms)` | Input | Warn | Warn on terms in prompt |
| `BlockedPattern(name, re)` | Input | Block | Prompt must not match regex |
| `WarnedPattern(name, re)` | Input | Warn | Warn on regex match |
| `RequiredRoles(roles...)` | Input | Block | Conversation must have roles |
| `MinResponseLength(n)` | Output | Warn | Response chars >= n |
| `MaxResponseLength(n)` | Output | Block | Response chars <= n |
| `RequiredFinishReason(r...)` | Output | Warn | Finish reason in allowed set |
| `BlockedOutputTerms(terms)` | Output | Block | Response must not contain terms |
| `MaxTokenUsage(n)` | Output | Block | Total tokens <= n |
| `OutputMustMatch(name, re)` | Output | Block | Response must match pattern |
| `OutputMustNotMatch(name, re)` | Output | Block | Response must not match pattern |

### Configuration Options

- **`WithFailOpen(true)`** — Allow calls to proceed even when block-level rules trigger (default: fail-closed)
- **`gate.Stats()`** — Get violation counts by rule name
- **`gate.StreamMiddleware()`** — Enforce guardrails on streaming calls

## Token Counting & Context Window Management

Estimate tokens, validate context windows, and manage costs before making API calls:

```go
import "github.com/atop0914/llmtrace/tokencount"

// Create a manager with default model registry
mgr := tokencount.NewManager()

// Estimate tokens for a text
tokens := tokencount.EstimateTokens("Hello, world!", 4.0)

// Validate a request against a model's context window
messages := []tokencount.Message{
    {Role: "system", Content: "You are a helpful assistant."},
    {Role: "user", Content: "Explain quantum computing."},
}
check := mgr.ValidateRequest("gpt-4o", messages, 500)
if !check.FitsContext {
    log.Printf("Request exceeds context window: %d tokens", check.InputTokens)
}

// Estimate cost before making the call
cost, _ := mgr.EstimateCost("gpt-4o", check.InputTokens, 500)
log.Printf("Estimated cost: $%.6f", cost)

// Truncate conversation to fit within limits
truncated := mgr.TruncateToFit("gpt-4o", messages, 500)

// Get model recommendations based on requirements
recommendations := mgr.RecommendModels(tokencount.Requirements{
    InputTokens:  5000,
    OutputTokens: 1000,
    MaxCostUSD:   0.01,
})
```

### Supported Models

The tokencount package includes a built-in registry with pricing and context windows for:
- OpenAI: gpt-4o, gpt-4o-mini, gpt-4-turbo, gpt-3.5-turbo
- Anthropic: claude-sonnet-4-20250514, claude-haiku-4-20250414
- Google: gemini-2.0-flash, gemini-2.0-flash-lite
- Ollama: llama3, mistral, phi3

### Token Estimation

Token estimation uses a configurable characters-per-token ratio (default: 4.0 for English text):

```go
// Custom estimation ratio for CJK text (typically ~2 chars/token)
tokens := tokencount.EstimateTokens("你好世界", 2.0)
```

## Prompt Template Management

Manage versioned prompt templates with variable substitution and A/B testing:

```go
import "github.com/atop0914/llmtrace/prompt"

// Create a registry
reg := prompt.NewRegistry()

// Register versioned templates
reg.Register(prompt.Template{
    Name:    "system",
    Version: "1.0",
    Content: "You are a helpful {{.Domain}} assistant. Be {{.Tone}}.",
    Vars: []prompt.VarDef{
        {Name: "Domain", Required: true, Default: "general"},
        {Name: "Tone", Required: false, Default: "concise"},
    },
    Tags: []string{"system", "v1"},
})

// Render a template
result, err := reg.Render("system", "1.0", map[string]string{
    "Domain": "Go programming",
    "Tone":   "friendly",
})

// Get the latest version
latest := reg.Latest("system")

// A/B testing with variant selection
ab := prompt.NewAB(reg, prompt.WithSeed(42))
variant, err := ab.Select("system", []string{"1.0", "2.0"})
```

### Template Features

- **Versioning** — Track and compare template versions
- **Variable validation** — Required/optional variables with defaults
- **Tags** — Categorize templates for filtering
- **A/B testing** — Deterministic variant selection for experiments
- **Diff** — Compare template versions side-by-side

## Multi-turn Conversation Session Tracking

Track conversation history with automatic token counting and context management:

```go
import "github.com/atop0914/llmtrace/session"

// Create a session manager
mgr := session.NewManager(
    session.WithMaxSessions(100),
    session.WithDefaultTTL(1 * time.Hour),
    session.WithManagerMaxTurns(20),
)

// Create a session with metadata
sess := mgr.Create(
    session.WithSystemPrompt("You are a helpful Go programming assistant."),
    session.WithMetadata("user_id", "user-123"),
)

// Add messages to the conversation
sess.AddUserMessage("What is Go?")
sess.AddAssistantMessage("Go is an open-source programming language...")

// Get conversation history for API calls
messages := sess.Messages()  // Returns []llmtrace.Message

// Track token usage
totalTokens := sess.TotalTokens()

// Check session health
if sess.IsExpired() {
    log.Println("Session expired, creating new one")
}

// Get session statistics
stats := sess.Stats()
log.Printf("Turns: %d, Tokens: %d", stats.TurnCount, stats.TotalTokens)
```

### Session Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `WithMaxSessions(n)` | Maximum concurrent sessions | 1000 |
| `WithDefaultTTL(d)` | Session expiration time | 24h |
| `WithManagerMaxTurns(n)` | Max turns per session | 100 |
| `WithCleanupInterval(d)` | Cleanup frequency for expired sessions | 5m |

## Token Counting Middleware

Automatically track token usage and costs as observability middleware:

```go
import "github.com/atop0914/llmtrace/tokencount"

// Create token counter with default settings
counter := tokencount.NewCounter()

// Use as middleware in the tracer
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(counter.Middleware()),
)

// Get aggregated token statistics
stats := counter.Stats()
log.Printf("Total tokens: %d, Total cost: $%.4f", stats.TotalTokens, stats.TotalCost)
log.Printf("Average tokens per request: %.1f", stats.AvgTokensPerRequest)

// Reset statistics
counter.Reset()
```

### Token Counting Features

- **Automatic tracking** — Captures input/output tokens from every LLM response
- **Cost calculation** — Real-time cost accumulation based on model pricing
- **Per-model breakdown** — Separate statistics for each model used
- **Middleware integration** — Works seamlessly with the existing middleware chain
- **Thread-safe** — Safe for concurrent use in production environments

## HTTP Framework Adapters

Standard `net/http` middleware for integrating LLMTrace into any Go web application. Works with Gin, Echo, Chi, and stdlib ServeMux.

```go
import "github.com/atop0914/llmtrace/adapters"

// Create middleware with defaults (OTel spans, request IDs, response headers)
mw := adapters.Middleware(adapters.DefaultConfig())

mux := http.NewServeMux()
mux.HandleFunc("POST /v1/chat", func(w http.ResponseWriter, r *http.Request) {
    // Access request metadata
    data := adapters.RequestDataFromContext(r.Context())
    log.Printf("[%s] request received", data.RequestID)

    // Set LLM-specific metadata (appears in response headers)
    adapters.SetProvider(r.Context(), "openai")
    adapters.SetModel(r.Context(), "gpt-4o")
    adapters.SetTokensUsed(r.Context(), 150)

    w.Write([]byte(`{"response":"hello"}`))
})

http.ListenAndServe(":8080", mw(mux))
```

**Response headers added automatically:**

| Header | Description |
|--------|-------------|
| `X-Request-ID` | Unique correlation ID for request tracing |
| `X-Response-Time-Ms` | Request processing duration |
| `X-LLM-Provider` | LLM provider name (set via `SetProvider`) |
| `X-Tokens-Used` | Total tokens consumed |

**Configuration options:**

```go
adapters.Config{
    TracerName:         "my-service",   // OTel instrumentation name
    GenerateRequestID:  true,           // Auto-generate X-Request-ID
    AddResponseHeaders: true,           // Add timing/token headers
    RecoverFromPanic:   true,           // Catch panics → 500 JSON error
    SpanNameFunc: func(r *http.Request) string { // Custom span naming
        return r.Method + " " + r.URL.Path
    },
}
```

## Content Moderation

Real-time content filtering for LLM inputs and outputs with blocklists, regex patterns, and PII detection.

```go
import "github.com/atop0914/llmtrace/moderation"

engine := moderation.New(moderation.DefaultConfig())

// Block specific words/phrases
engine.AddRule(moderation.NewWordBlocklist(
    "profanity", []string{"spam", "scam"},
    moderation.ActionBlock, moderation.SeverityHigh, false,
))

// Detect and redact PII (emails, phones, SSNs, credit cards)
engine.AddRule(moderation.NewPIIDetector(
    moderation.ActionRedact, moderation.SeverityMedium,
))

// Custom regex rule
engine.AddRule(moderation.NewRegexRule(
    "url_detector", "Detects URLs",
    regexp.MustCompile(`https?://[^\s]+`),
    moderation.ActionLog, moderation.SeverityLow,
))

// Check content
result, _ := engine.CheckInput(ctx, userInput)
if !result.Allowed {
    log.Printf("blocked: %d matches", len(result.Matches))
}
```

**Built-in rule constructors:**

| Constructor | Description |
|------------|-------------|
| `NewWordBlocklist` | Word/phrase matching (case-sensitive or insensitive) |
| `NewRegexRule` | Regex pattern matching |
| `NewPIIDetector` | Emails, phone numbers, SSNs, credit card numbers |
| `NewMaxLengthRule` | Content byte length limit |

**Actions:** `ActionBlock` (reject), `ActionRedact` (replace with placeholder), `ActionLog` (record only)

**LLM middleware integration:**

```go
// Block harmful input before it reaches the LLM
tracer.Use(moderation.Middleware(engine))

// Filter/redact LLM output
tracer.Use(moderation.OutputMiddleware(engine))

// Check if an error was caused by moderation
if moderation.IsBlocked(err) {
    // Handle blocked content
}
```

## OpenAI-Compatible Local Proxy

Run a local proxy server that accepts OpenAI API format requests and routes them through LLMTrace providers with full observability.

```go
import "github.com/atop0914/llmtrace/proxy"

srv := proxy.New(proxy.Config{
    Listen: ":8080",
    Providers: map[string]proxy.ProviderEntry{
        "gpt": {
            Provider: openai.New(openai.WithAPIKey("sk-...")),
            Default:  true,
        },
        "claude": {
            Provider: anthropic.New(anthropic.WithAPIKey("sk-ant-...")),
        },
    },
    Tracer: llmtrace.NewTracer("llmtrace-proxy"),
    APIKey: "my-proxy-key", // Clients authenticate with this
})
srv.ListenAndServe()
```

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | Chat completions (streaming & non-streaming) |
| `GET` | `/v1/models` | List available models across all providers |
| `GET` | `/health` | Health check (no auth required) |

**Usage with any OpenAI-compatible client:**

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer my-proxy-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello!"}]}'
```

## Health Check & Readiness Probes

Kubernetes-style liveness and readiness endpoints for production deployments.

```go
import "github.com/atop0914/llmtrace/healthcheck"

hc := healthcheck.New(healthcheck.Config{
    Timeout:         3 * time.Second,
    AggregateStatus: true,
})

// Register readiness checks
hc.AddReadinessCheck("database", func(ctx context.Context) error {
    return db.PingContext(ctx)
})
hc.AddReadinessCheck("redis", func(ctx context.Context) error {
    return redisClient.Ping(ctx).Err()
})

mux := http.NewServeMux()
mux.HandleFunc("/healthz", hc.LiveHandler)   // Liveness: always 200
mux.HandleFunc("/readyz", hc.ReadyHandler)   // Readiness: 200 only when all checks pass
```

**Features:**
- Per-check timeout with context cancellation
- Aggregate status (all checks must pass for 200)
- Uptime tracking (`hc.Uptime()`)
- Pluggable check functions for any dependency

## Request/Response Correlation IDs

Propagate correlation IDs across HTTP requests for distributed tracing.

```go
import "github.com/atop0914/llmtrace/correlation"

corr := correlation.New(correlation.DefaultConfig())

handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    id := correlation.IDFromContext(r.Context())
    log.Printf("[%s] processing request", id)
    // ID is automatically added to response headers
})

http.ListenAndServe(":8080", corr.Middleware(handler))
```

**Features:**
- Auto-generates `X-Request-ID` if not present in request
- Extracts existing ID from incoming headers
- Adds ID to response headers for client-side correlation
- Context-based access via `IDFromContext()`
- Configurable header name and ID generator

## Webhook Alerts

Send alert notifications via webhooks when thresholds are exceeded:

```go
import "github.com/atop0914/llmtrace/webhook"

alerter := webhook.NewAlerter(webhook.Config{
    URL: "https://hooks.slack.com/services/...",
    Thresholds: webhook.Thresholds{
        ErrorRate:    0.05,  // 5% error rate
        LatencyP95MS: 5000,  // 5s P95 latency
        CostPerHour:  10.0,  // $10/hour
    },
})
```

## Config Hot-Reload

Watch configuration file changes without restarting:

```go
import "github.com/atop0914/llmtrace/configwatch"

watcher, err := configwatch.Watch("config.yaml", func(cfg configwatch.Config) {
    // Config updated — apply changes
    log.Printf("Config reloaded: providers=%d", len(cfg.Providers))
})
defer watcher.Stop()
```

## Dashboard API Reference

All endpoints return JSON. Base path: `/api`

### Overview

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/overview` | GET | Summary metrics |
| `/api/events` | GET | SSE real-time stream |

### Provider Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/providers` | GET | Per-provider metrics breakdown |
| `/api/providers/health` | GET | Provider health with latency percentiles and cost efficiency |

**`/api/providers/health` Response:**
```json
{
  "providers": [{
    "name": "openai",
    "error_rate": 0.02,
    "health_score": 92.5,
    "cost_per_1k_tokens": 0.003,
    "tokens_per_second": 150.5,
    "latency_p50_ms": 450.0,
    "latency_p95_ms": 1200.0,
    "latency_p99_ms": 2500.0,
    "total_requests": 1000,
    "total_tokens": 50000,
    "total_cost_usd": 0.15,
    "models": [{"model": "gpt-4o", "requests": 800, "error_rate": 0.01}],
    "status": "healthy"
  }]
}
```

### Model Endpoints

| Endpoint | Method | Query Params | Description |
|----------|--------|--------------|-------------|
| `/api/models` | GET | — | Per-model metrics breakdown |
| `/api/models/health` | GET | — | Detailed model health with latency percentiles |
| `/api/models/compare` | GET | `models=prov:model,prov:model` | Side-by-side model comparison |
| `/api/models/rankings` | GET | — | Model rankings by cost, latency, throughput, reliability |

**`/api/models/compare` Example:**
```
GET /api/models/compare?models=openai:gpt-4o,anthropic:claude-3
```

**`/api/models/rankings` Response:**
```json
{
  "by_cost_efficiency": [{"rank": 1, "provider": "openai", "model": "gpt-4o-mini", "value": 0.00015}],
  "by_latency": [{"rank": 1, "provider": "ollama", "model": "llama3", "value": 120.5}],
  "by_throughput": [{"rank": 1, "provider": "openai", "model": "gpt-4o", "value": 200.0}],
  "by_reliability": [{"rank": 1, "provider": "anthropic", "model": "claude-3", "value": 0.001}]
}
```

### Cost Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/costs` | GET | Cost analysis by model |
| `/api/costs/trend` | GET | Daily cost trend from traces |
| `/api/costs/breakdown` | GET | Cost breakdown by provider with percentages |

**`/api/costs/trend` Response:**
```json
{
  "daily": [{"date": "2026-06-20", "cost_usd": 0.05, "requests": 100, "tokens": 5000}],
  "total_usd": 0.35,
  "days": 7,
  "avg_per_day": 0.05
}
```

### Error Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/errors` | GET | Error statistics by type and provider |
| `/api/errors/trend` | GET | Daily error rate trend from traces |
| `/api/errors/recent` | GET | Recent error traces with classification |

**`/api/errors/recent` Response:**
```json
{
  "errors": [{
    "id": "trace-abc123",
    "timestamp": "2026-06-20T10:30:00Z",
    "provider": "openai",
    "model": "gpt-4o",
    "error_type": "rate_limit",
    "error_msg": "openai: rate limit exceeded (429)",
    "latency_ms": 500.0
  }],
  "total": 1
}
```

Error types: `rate_limit`, `timeout`, `context_length`, `auth_error`, `server_error`, `api_error`, `unknown`

### Trace Endpoints

| Endpoint | Method | Query Params | Description |
|----------|--------|--------------|-------------|
| `/api/traces` | GET | `provider`, `model`, `status`, `limit`, `since`, `sort` | Trace explorer with filtering |
| `/api/traces/summary` | GET | — | Trace summary statistics |

### Latency Endpoint

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/latency` | GET | Latency histogram distribution by provider/model |

### Health Score Calculation

Health scores (0-100) are computed as a weighted composite:
- Base score: 100
- Error rate penalty: up to -40 (error_rate * 40)
- Latency penalty: -30 if P95 > 5s (scaled by severity)
- Status: `healthy` (>=70), `degraded` (40-69), `unhealthy` (<40)

## Benchmarks

Run benchmarks with:

```bash
go test -bench=. -benchmem ./...
```

Key results (Xeon Gold 6148, 2.40 GHz):

| Benchmark | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| Tracer.Complete | ~9,000 | ~6,600 | 19 |
| Tracer.Complete + Cost | ~9,400 | ~7,700 | 20 |
| Tracer.Stream | ~16,000 | ~7,300 | 24 |
| CostCalculator.Calculate | ~37 | 0 | 0 |
| RetryConfig.CalculateDelay | ~40 | 0 | 0 |
| WithRetry (immediate success) | ~11 | 0 | 0 |
| Limiter.Allow | ~102 | 0 | 0 |
| Limiter.Wait | ~900 | 0 | 0 |
| Middleware Chain (1/3/5) | ~10/21/26 | 0 | 0 |
| Chat (no middleware) | ~7,100 | — | — |
| Chat + retry | ~10,400 | — | — |
| ClassifyHTTPStatus | ~3 | 0 | 0 |
| Eval.Suite.Run (8 evaluators) | ~7,700 | ~2,500 | 44 |
| Eval.MinLength | ~450 | 40 | 2 |
| Eval.ValidJSON | ~1,170 | 320 | 6 |

## API Reference

Full API documentation is available on [pkg.go.dev](https://pkg.go.dev/github.com/atop0914/llmtrace).

### Core Types

- `Tracer` — main entry point for tracing LLM calls
- `Request` / `Response` — LLM request/response types
- `Message` — conversation message with role and content
- `Usage` — token usage tracking
- `StreamChunk` — partial response in a stream

### Key Functions

- `NewTracer(serviceName, ...Option)` — create a new tracer
- `tracer.Complete(ctx, req, fn)` — trace a non-streaming call
- `tracer.Stream(ctx, req, fn)` — trace a streaming call
- `tracer.Chat(ctx, req, provider, ...ChatOption)` — convenience method with retry/middleware
- `tracer.ChatStream(ctx, req, provider, ...ChatOption)` — streaming convenience method

## Contributing

Contributions are welcome! Here's how to get started.

### Getting Started

1. Fork the repository
2. Clone your fork: `git clone git@github.com:YOUR_USERNAME/llmtrace.git`
3. Create a feature branch: `git checkout -b feature/amazing-feature`
4. Make your changes
5. Run tests: `go test -short -v -race ./...`
6. Commit: `git commit -m 'feat: add amazing feature'`
7. Push: `git push origin feature/amazing-feature`
8. Open a Pull Request

### Development

```bash
# Run all tests (skip Docker-dependent tests)
go test -short -v -race ./...

# Run specific package tests
go test -short -v ./dashboard/...

# Run benchmarks
go test -bench=. -benchmem ./...

# Run linter
golangci-lint run

# Build
go build ./...

# Run dashboard example
go run ./examples/dashboard
```

### Code Style

- Follow standard Go conventions and `gofmt`
- Use `golangci-lint` for static analysis
- All exported functions must have doc comments
- Error messages should be lowercase and not end with punctuation
- Use `context.Context` as the first parameter for cancellable operations

### Commit Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation
- `test:` adding tests
- `refactor:` code restructuring
- `chore:` maintenance tasks
- `perf:` performance improvement

### Adding a New Provider

1. Create a new package under `provider/yourprovider/`
2. Implement the `Provider` interface (Complete, Stream, Name, DefaultModel, SupportsStreaming)
3. Add tests with mock HTTP server
4. Add pricing data to the cost calculator
5. Update the providers table in this README

### Adding a New Middleware

1. Implement the middleware function signature
2. Add a `WithXxx` option function
3. Add tests covering both Complete and Stream paths
4. Document usage in this README

### Testing Guidelines

- Use `-short` flag to skip Docker-dependent tests locally
- All container-dependent tests must check `testing.Short()` at the start
- Use `httptest.NewServer` for HTTP-based provider tests
- Aim for >80% code coverage on new code
- Run `go vet ./...` before committing

### Project Structure

```
llmtrace/
+-- *.go                    # Core SDK (tracer, middleware, errors)
+-- provider/               # LLM provider implementations
|   +-- openai/
|   +-- anthropic/
|   +-- gemini/
|   +-- ollama/
|   +-- azure/
|   +-- compat/
+-- adapters/               # HTTP framework adapters (net/http, gin, echo, chi)
+-- correlation/            # Request/response correlation ID middleware
+-- healthcheck/            # Liveness & readiness probes (K8s-style)
+-- moderation/             # Content moderation (blocklist, PII, regex)
+-- proxy/                  # OpenAI-compatible local proxy server
+-- metrics/                # Prometheus-compatible metrics
+-- dashboard/              # Web dashboard (API + static UI)
+-- eval/                   # Evaluation framework + LLM-as-judge
+-- embedding/              # Text embeddings & vector similarity search
+-- guardrails/             # Input/output validation rules
+-- tokencount/             # Token estimation & context window management
+-- prompt/                 # Versioned prompt templates
+-- session/                # Multi-turn conversation tracking
+-- batch/                  # Async batch request execution
+-- propagation/            # W3C Trace Context distributed tracing
+-- semcache/               # Semantic caching with embedding similarity
+-- streammetric/           # Streaming metrics (TTFT, ICL, TPS)
+-- loadbalancer/           # Provider load balancing (4 strategies)
+-- traceexport/            # Trace file export (JSON/CSV/JSONL/rotate)
+-- webhook/                # Webhook alert notifications
+-- configwatch/            # Config file hot-reload
+-- tokenreport/            # Token usage aggregation
+-- examples/               # Example programs
|   +-- basic/
|   +-- dashboard/
|   +-- middleware/
|   +-- streaming/
|   +-- streammetric/
|   +-- batch/
|   +-- embedding/
|   +-- propagation/
|   +-- judge/
|   +-- semcache/
|   +-- tokencount/
|   +-- prompt/
|   +-- session/
|   +-- proxy/
|   +-- healthcheck/
|   +-- correlation/
|   +-- adapters/
|   +-- moderation/
+-- .github/workflows/      # CI/CD
```

## License

MIT — see [LICENSE](LICENSE) for details.