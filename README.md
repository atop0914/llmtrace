# LLMTrace

**OpenTelemetry-native LLM Observability SDK for Go**

LLMTrace wraps LLM client calls with OpenTelemetry spans, capturing token usage, latency, cost, and request/response metadata — following the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **OpenTelemetry native** — standard `gen_ai.*` span attributes, OTLP export
- **Multi-provider** — OpenAI, Anthropic, Gemini (extensible)
- **Cost tracking** — automatic USD cost calculation per request
- **Streaming support** — trace SSE streaming responses
- **Retry with backoff** — configurable exponential backoff for transient errors
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

## License

MIT
