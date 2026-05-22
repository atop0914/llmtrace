# LLMTrace

**OpenTelemetry-native LLM Observability SDK for Go**

LLMTrace wraps LLM client calls with OpenTelemetry spans, capturing token usage, latency, cost, and request/response metadata — following the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **OpenTelemetry native** — standard `gen_ai.*` span attributes, OTLP export
- **Multi-provider** — OpenAI, Anthropic, Gemini (extensible)
- **Cost tracking** — automatic USD cost calculation per request
- **Streaming support** — trace SSE streaming responses
- **Zero-config defaults** — works out of the box with sensible defaults

## Quick Start

```go
import (
    "github.com/atop0914/llmtrace"
    "github.com/atop0914/llmtrace/provider/openai"
)

tracer := llmtrace.NewTracer("my-service",
    llmtrace.WithProvider("openai"),
    llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
)

resp, err := tracer.Complete(ctx, &llmtrace.Request{
    Model:    "gpt-4o",
    Messages: []llmtrace.Message{{Role: "user, Content: "Hello!"}},
}, openai.Complete)
```

## Providers

| Provider | Package | Status |
|----------|---------|--------|
| OpenAI | `provider/openai` | ✅ |
| Anthropic | `provider/anthropic` | ✅ |
| Gemini | `provider/gemini` | ✅ |

## License

MIT
