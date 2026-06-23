// Command middleware demonstrates LLMTrace middleware capabilities.
//
// This example shows how to chain multiple middlewares for logging,
// timing, and cost tracking.
//
// Usage:
//
//	export OPENAI_API_KEY="sk-..."
//	go run ./examples/middleware
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/provider/openai"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func main() {
	// Set up OpenTelemetry with in-memory exporter.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Create provider.
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("Set OPENAI_API_KEY environment variable")
	}

	provider := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithModel("gpt-4o-mini"),
	)

	// Create tracer with cost calculator.
	tracer := llmtrace.NewTracer("middleware-demo",
		llmtrace.WithProvider("openai"),
		llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
	)

	// Define middleware 1: Logging
	loggingMiddleware := func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			fmt.Printf("[LOG] Sending request to %s with %d messages\n",
				req.Model, len(req.Messages))
			start := time.Now()
			resp, err := next(ctx, req)
			duration := time.Since(start)
			if err != nil {
				fmt.Printf("[LOG] Request failed after %v: %v\n", duration, err)
			} else {
				fmt.Printf("[LOG] Request completed in %v, %d tokens used\n",
					duration, resp.Usage.TotalTokens)
			}
			return resp, err
		}
	}

	// Define middleware 2: Cost tracking
	costMiddleware := func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return nil, err
			}
			calc := llmtrace.NewCostCalculator()
			cost := calc.Calculate(resp.Model, resp.Usage)
			fmt.Printf("[COST] Model: %s, Tokens: %d, Cost: $%.6f\n",
				resp.Model, resp.Usage.TotalTokens, cost)
			return resp, nil
		}
	}

	// Chain middlewares: logging wraps cost wraps provider
	chain := llmtrace.Chain(loggingMiddleware, costMiddleware)

	// Make a request using the chain
	ctx := context.Background()
	req := &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleSystem, Content: "You are a helpful assistant."},
			{Role: llmtrace.RoleUser, Content: "Explain Go channels in one sentence."},
		},
		MaxTokens: llmtrace.IntPtr(100),
	}

	resp, err := tracer.Chat(ctx, req, provider,
		llmtrace.WithCallMiddleware(chain),
		llmtrace.WithCallRetry(llmtrace.DefaultRetryConfig()),
	)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("\nResponse: %s\n", resp.Content)
	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Tokens: %d in, %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)

	// Show captured spans.
	spans := exporter.GetSpans()
	fmt.Printf("\nCaptured %d span(s):\n", len(spans))
	for _, s := range spans {
		fmt.Printf("  - %s (%s)\n", s.Name, s.Status.Code)
	}
}
