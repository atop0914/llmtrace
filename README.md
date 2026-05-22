# GoLLM

**OpenTelemetry-native LLM Observability SDK for Go**

GoLLM wraps LLM client calls with OpenTelemetry spans, capturing token usage, latency, cost, and request/response metadata — following the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **OpenTelemetry native** — standard `gen_ai.*` span attributes, OTLP export
- **Multi-provider** — OpenAI, Anthropic, Gemini (extensible)
- **Cost tracking** — automatic USD cost calculation per request
- **Streaming support** — trace SSE streaming responses
- **Zero-config defaults** — works out of the box with sensible defaults

## Quick Start

```go
import (
    "github.com/atop0914/gollm"
    "github.com/atop0914/gollm/provider/openai"
)

tracer := gollm.NewTracer("my-service",
    gollm.WithProvider("openai"),
    gollm.WithCostCalculator(gollm.NewCostCalculator()),
)

resp, err := tracer.Complete(ctx, &gollm.Request{
    Model:    "gpt-4o",
    Messages: []gollm.Message{{Role: "user, Content: "Hello!"}},
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
