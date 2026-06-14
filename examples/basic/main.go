// Command basic demonstrates LLMTrace usage with OpenAI provider.
//
// Usage:
//
//	export OPENAI_API_KEY="sk-..."
//	go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/provider/openai"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func main() {
	// Set up OpenTelemetry with a stdout exporter for demo purposes.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Create an OpenAI provider.
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("Set OPENAI_API_KEY environment variable")
	}

	provider := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithModel("gpt-4o-mini"),
	)

	// Create a tracer with cost tracking.
	tracer := llmtrace.NewTracer("my-service",
		llmtrace.WithProvider("openai"),
		llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
	)

	// Use the Chat convenience method with retry.
	ctx := context.Background()
	resp, err := tracer.Chat(ctx, &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Explain what OpenTelemetry is in one sentence."},
		},
		MaxTokens: llmtrace.IntPtr(100),
	}, provider,
		llmtrace.WithCallRetry(llmtrace.DefaultRetryConfig()),
		llmtrace.WithCallMiddleware(llmtrace.WithCompleteHook(func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response, err error) {
			if err != nil {
				fmt.Printf("Hook: request failed: %v\n", err)
			} else {
				fmt.Printf("Hook: %s used %d tokens, cost $%.4f\n",
					resp.Model, resp.Usage.TotalTokens,
					llmtrace.NewCostCalculator().Calculate(resp.Model, resp.Usage))
			}
		})),
	)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Tokens: %d in, %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	fmt.Printf("Latency: %v\n", resp.Latency)

	// Show captured spans.
	spans := exporter.GetSpans()
	fmt.Printf("\nCaptured %d span(s):\n", len(spans))
	for _, s := range spans {
		fmt.Printf("  - %s (%s)\n", s.Name, s.Status.Code)
		for _, a := range s.Attributes {
			fmt.Printf("    %s = %v\n", a.Key, a.Value)
		}
	}
}
