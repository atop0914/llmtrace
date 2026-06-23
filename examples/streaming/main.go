// Command streaming demonstrates LLMTrace streaming capabilities.
//
// This example shows how to use streaming with OpenTelemetry tracing.
//
// Usage:
//
//	export OPENAI_API_KEY="sk-..."
//	go run ./examples/streaming
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
	tracer := llmtrace.NewTracer("streaming-demo",
		llmtrace.WithProvider("openai"),
		llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
	)

	// Make a streaming request.
	ctx := context.Background()
	req := &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Write a haiku about programming."},
		},
		MaxTokens: llmtrace.IntPtr(100),
	}

	fmt.Println("Starting stream...")
	ch, err := tracer.ChatStream(ctx, req, provider)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	// Read chunks as they arrive.
	fmt.Print("Response: ")
	var totalContent string
	for chunk := range ch {
		if chunk.Error != nil {
			fmt.Printf("\nStream error: %v\n", chunk.Error)
			break
		}
		fmt.Print(chunk.Content)
		totalContent += chunk.Content

		// Print usage info when available (final chunk)
		if chunk.Usage != nil {
			fmt.Printf("\n\nTokens: %d in, %d out, %d total\n",
				chunk.Usage.InputTokens,
				chunk.Usage.OutputTokens,
				chunk.Usage.TotalTokens)
		}
	}

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
