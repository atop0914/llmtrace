# LLMTrace

[![Go Reference](https://pkg.go.dev/badge/github.com/atop0914/llmtrace.svg)](https://pkg.go.dev/github.com/atop0914/llmtrace)
[![CI](https://github.com/atop0914/llmtrace/actions/workflows/ci.yml/badge.svg)](https://github.com/atop0914/llmtrace/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**OpenTelemetry-native LLM Observability SDK for Go**

LLMTrace wraps LLM client calls with OpenTelemetry spans, capturing token usage, latency, cost, and request/response metadata — following the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **OpenTelemetry native** — standard `gen_ai.*` span attributes, OTLP export
- **Multi-provider** — OpenAI, Anthropic, Gemini (extensible)
- **Cost tracking** — automatic USD cost calculation per request
- **Streaming support** — trace SSE streaming responses
- **Retry with backoff** — configurable exponential backoff for transient errors
- **Rate limiting** — token bucket rate limiter for API call throttling
- **Middleware pattern** — add logging, hooks, and custom interceptors
- **Prometheus metrics** — built-in metrics collector with `/metrics` endpoint
- **Unified errors** — consistent error types across all providers
- **Zero external dependencies** — only depends on OpenTelemetry

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

### Creating a Provider

```go
// OpenAI
provider := openai.New(
    openai.WithAPIKey("sk-..."),
    openai.WithBaseURL("https://api.openai.com/v1"), // optional, for proxies
)

// Anthropic
provider := anthropic.New(
    anthropic.WithAPIKey("sk-ant-..."),
)

// Gemini
provider := gemini.New(
    gemini.WithAPIKey("..."),
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

## Retry with Backoff

Automatically retry transient errors (rate limits, server errors) with exponential backoff:

```go
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallRetry(llmtrace.RetryConfig{
        MaxRetries:      3,
        InitialInterval: 500 * time.Millisecond,
        MaxInterval:     30 * time.Second,
        Multiplier:      2.0,
        Jitter:          0.2,
    }),
)
```

Or use the default config:

```go
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallRetry(llmtrace.DefaultRetryConfig()),
)
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
┌─────────────────────────────────────────────────────────┐
│                      Your Application                    │
├─────────────────────────────────────────────────────────┤
│                    llmtrace.Tracer                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │ Complete  │  │  Stream  │  │   Chat   │              │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘              │
│       │              │              │                    │
│  ┌────▼──────────────▼──────────────▼────┐              │
│  │         Middleware Chain                │              │
│  │  ┌──────┐  ┌──────┐  ┌──────┐        │              │
│  │  │Rate  │  │Retry │  │Hooks │        │              │
│  │  │Limit │  │      │  │      │        │              │
│  │  └──────┘  └──────┘  └──────┘        │              │
│  └────────────────┬──────────────────────┘              │
│                   │                                      │
│  ┌────────────────▼──────────────────────┐              │
│  │          Provider Interface            │              │
│  │  ┌────────┐ ┌──────────┐ ┌────────┐  │              │
│  │  │ OpenAI │ │Anthropic │ │ Gemini │  │              │
│  │  └────────┘ └──────────┘ └────────┘  │              │
│  └───────────────────────────────────────┘              │
│                                                          │
│  ┌───────────────────────────────────────┐              │
│  │     OpenTelemetry Spans (gen_ai.*)    │              │
│  │  • gen_ai.system  • gen_ai.usage.*    │              │
│  │  • gen_ai.request • gen_ai.response   │              │
│  └───────────────────────────────────────┘              │
│                                                          │
│  ┌───────────────────────────────────────┐              │
│  │     Prometheus Metrics (/metrics)     │              │
│  │  • requests_total  • tokens_total     │              │
│  │  • duration        • cost_usd_total   │              │
│  └───────────────────────────────────────┘              │
└─────────────────────────────────────────────────────────┘
```

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

## Examples

See [examples/basic/](examples/basic/) for a complete usage demo.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'feat: add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development

```bash
# Run tests
go test -short -v -race ./...

# Run benchmarks
go test -bench=. -benchmem ./...

# Run linter
golangci-lint run

# Build
go build ./...
```

## License

MIT — see [LICENSE](LICENSE) for details.
