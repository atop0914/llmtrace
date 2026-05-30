# LLMTrace

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
- **Zero-config defaults** — works out of the box with sensible defaults

## Quick Start

```go
import (
    "github.com/atop0914/llmtrace"
    "github.com/atop0914/llmtrace/provider/openai"
)

provider := openai.New(openai.WithAPIKey("sk-..."))
tracer := llmtrace.NewTracer("my-service",
    llmtrace.WithProvider("openai"),
    llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
)

resp, err := tracer.Chat(ctx, &llmtrace.Request{
    Model:    "gpt-4o",
    Messages: []llmtrace.Message{{Role: "user", Content: "Hello!"}},
}, provider)
```

## Providers

| Provider | Package | Status |
|----------|---------|--------|
| OpenAI | `provider/openai` | ✅ |
| Anthropic | `provider/anthropic` | ✅ |
| Gemini | `provider/gemini` | ✅ |

## Retry with Backoff

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

## Middleware

```go
// Add a hook that runs after every request
resp, err := tracer.Chat(ctx, req, provider,
    llmtrace.WithCallMiddleware(
        llmtrace.WithCompleteHook(func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response, err error) {
            log.Printf("model=%s tokens=%d", resp.Model, resp.Usage.TotalTokens)
        }),
    ),
)
```

## Streaming

```go
ch, err := tracer.ChatStream(ctx, &llmtrace.Request{
    Model:    "gpt-4o",
    Messages: []llmtrace.Message{{Role: "user", Content: "Write a poem."}},
}, provider)

for chunk := range ch {
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

// Non-blocking check
if lim.Allow() {
    // proceed immediately
}

// Blocking wait with context
if err := lim.Wait(ctx); err != nil {
    // context canceled or rate limit exceeded
}
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

## License

MIT
