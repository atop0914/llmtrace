package llmtrace

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TracerName is the default instrumentation name.
	TracerName = "github.com/atop0914/llmtrace"

	// GenAI semantic convention attribute keys (OTel GenAI semconv).
	AttrGenAISystem        = "gen_ai.system"
	AttrGenAIOperationName = "gen_ai.operation.name"
	AttrGenAIRequestModel  = "gen_ai.request.model"
	AttrGenAIResponseModel = "gen_ai.response.model"
	AttrGenAIRequestTemp   = "gen_ai.request.temperature"
	AttrGenAIRequestTopP   = "gen_ai.request.top_p"
	AttrGenAIRequestMaxTok = "gen_ai.request.max_tokens"
	AttrGenAIResponseID    = "gen_ai.response.id"
	AttrGenAIFinishReasons = "gen_ai.response.finish_reasons"
	AttrGenAIInputTokens   = "gen_ai.usage.input_tokens"
	AttrGenAIOutputTokens  = "gen_ai.usage.output_tokens"
	AttrGenAITotalTokens   = "gen_ai.usage.total_tokens"
	AttrGenAICostUSD       = "gen_ai.usage.cost_usd"
)

// Tracer wraps LLM calls with OpenTelemetry spans.
type Tracer struct {
	tracer   trace.Tracer
	provider string
	costCalc *CostCalculator
}

// NewTracer creates a new Tracer for the given service name.
// Options can configure the provider name, cost calculator, etc.
func NewTracer(serviceName string, opts ...Option) *Tracer {
	t := &Tracer{
		tracer: otel.GetTracerProvider().Tracer(
			TracerName,
			trace.WithInstrumentationVersion(Version),
		),
		provider: "unknown",
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Complete wraps an LLM completion call with OpenTelemetry tracing.
// It creates a span, captures request/response metadata, and records
// token usage, latency, and cost.
//
// Usage:
//
//	tracer := gollm.NewTracer("my-service", gollm.WithProvider("openai"))
//	resp, err := tracer.Complete(ctx, req, openai.Complete)
func (t *Tracer) Complete(ctx context.Context, req *Request, fn CompleteFunc) (*Response, error) {
	ctx, span := t.tracer.Start(ctx, "chat "+req.Model,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Set request attributes
	t.setRequestAttrs(span, req)

	start := time.Now()
	resp, err := fn(ctx, req)
	latency := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// Set response attributes
	t.setResponseAttrs(span, resp, latency)

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// Stream wraps a streaming LLM call with OpenTelemetry tracing.
// The span is ended when the stream channel is closed.
func (t *Tracer) Stream(ctx context.Context, req *Request, fn StreamFunc) (<-chan StreamChunk, error) {
	ctx, span := t.tracer.Start(ctx, "chat "+req.Model,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	t.setRequestAttrs(span, req)

	start := time.Now()
	ch, err := fn(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	// Wrap channel to capture final usage and end span
	out := make(chan StreamChunk)
	go func() {
		defer span.End()
		defer close(out)
		var lastChunk StreamChunk
		for chunk := range ch {
			if chunk.Error != nil {
				span.RecordError(chunk.Error)
				span.SetStatus(codes.Error, chunk.Error.Error())
			}
			lastChunk = chunk
			out <- chunk
		}
		latency := time.Since(start)
		if lastChunk.Usage != nil {
			resp := &Response{
				Model:        req.Model,
				Usage:        *lastChunk.Usage,
				Latency:      latency,
				Provider:     t.provider,
				FinishReason: "stop",
			}
			t.setResponseAttrs(span, resp, latency)
		}
		span.SetStatus(codes.Ok, "")
	}()

	return out, nil
}

// setRequestAttrs sets OpenTelemetry span attributes from the request.
func (t *Tracer) setRequestAttrs(span trace.Span, req *Request) {
	span.SetAttributes(
		attribute.String(AttrGenAISystem, t.provider),
		attribute.String(AttrGenAIOperationName, "chat"),
		attribute.String(AttrGenAIRequestModel, req.Model),
	)
	if req.Temperature != nil {
		span.SetAttributes(attribute.Float64(AttrGenAIRequestTemp, *req.Temperature))
	}
	if req.TopP != nil {
		span.SetAttributes(attribute.Float64(AttrGenAIRequestTopP, *req.TopP))
	}
	if req.MaxTokens != nil {
		span.SetAttributes(attribute.Int(AttrGenAIRequestMaxTok, *req.MaxTokens))
	}
}

// setResponseAttrs sets OpenTelemetry span attributes from the response.
func (t *Tracer) setResponseAttrs(span trace.Span, resp *Response, latency time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String(AttrGenAIResponseModel, resp.Model),
		attribute.String(AttrGenAIResponseID, resp.ID),
		attribute.String(AttrGenAIFinishReasons, resp.FinishReason),
		attribute.Int(AttrGenAIInputTokens, resp.Usage.InputTokens),
		attribute.Int(AttrGenAIOutputTokens, resp.Usage.OutputTokens),
		attribute.Int(AttrGenAITotalTokens, resp.Usage.TotalTokens),
	}

	// Calculate cost if calculator is available
	if t.costCalc != nil {
		cost := t.costCalc.Calculate(resp.Model, resp.Usage)
		if cost > 0 {
			attrs = append(attrs, attribute.Float64(AttrGenAICostUSD, cost))
		}
	}

	span.SetAttributes(attrs...)
}
