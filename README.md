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
- **Trace export** — export traces to JSON/CSV files with batch buffering and auto-rotation
- **Prometheus metrics** — built-in metrics collector with `/metrics` endpoint
- **Real-time dashboard** — web UI with Chart.js, SSE updates, 6 monitoring pages
- **Unified errors** — consistent error types across all providers
- **Config hot-reload** — watch config file changes and apply without restart

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
```

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
+-- metrics/                # Prometheus-compatible metrics
+-- dashboard/              # Web dashboard (API + static UI)
+-- eval/                   # Evaluation framework
+-- traceexport/            # Trace file export (JSON/CSV/rotate)
+-- webhook/                # Webhook alert notifications
+-- configwatch/            # Config file hot-reload
+-- tokenreport/            # Token usage aggregation
+-- examples/               # Example programs
|   +-- basic/
|   +-- dashboard/
|   +-- middleware/
|   +-- streaming/
+-- .github/workflows/      # CI/CD
```

## License

MIT — see [LICENSE](LICENSE) for details.