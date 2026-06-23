// Command dashboard demonstrates LLMTrace with the web dashboard.
//
// This example starts an HTTP server with the LLMTrace dashboard,
// allowing you to view metrics in a web browser.
//
// Usage:
//
//	go run ./examples/dashboard
//	# Open http://localhost:8080 in your browser
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/dashboard"
	"github.com/atop0914/llmtrace/metrics"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func main() {
	// Set up OpenTelemetry with in-memory exporter.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Create metrics registry with namespace.
	registry := metrics.NewRegistry("llmtrace")

	// Create a mock provider for demo purposes.
	provider := &demoProvider{name: "openai", model: "gpt-4o-mini"}

	// Create tracer with cost calculator.
	tracer := llmtrace.NewTracer("dashboard-demo",
		llmtrace.WithProvider("openai"),
		llmtrace.WithCostCalculator(llmtrace.NewCostCalculator()),
	)

	// Create dashboard handler.
	handler := dashboard.Handler(registry, dashboard.Config{
		SSEInterval: 2 * time.Second,
	})

	// Start background demo requests.
	go func() {
		for {
			ctx := context.Background()
			req := &llmtrace.Request{
				Model: "gpt-4o-mini",
				Messages: []llmtrace.Message{
					{Role: llmtrace.RoleUser, Content: "Hello"},
				},
			}
			_, err := tracer.Chat(ctx, req, provider)
			if err != nil {
				log.Printf("Demo request failed: %v", err)
			}
			time.Sleep(2 * time.Second)
		}
	}()

	// Start HTTP server.
	addr := ":8080"
	fmt.Printf("Dashboard available at http://localhost%s\n", addr)
	fmt.Println("Press Ctrl+C to stop")
	log.Fatal(http.ListenAndServe(addr, handler))
}

// demoProvider is a mock provider for demonstration purposes.
type demoProvider struct {
	name  string
	model string
}

func (p *demoProvider) Name() string            { return p.name }
func (p *demoProvider) DefaultModel() string    { return p.model }
func (p *demoProvider) SupportsStreaming() bool { return true }

func (p *demoProvider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	return &llmtrace.Response{
		ID:           "demo-123",
		Model:        p.model,
		Content:      "This is a demo response from the dashboard example.",
		FinishReason: "stop",
		Usage: llmtrace.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
		Provider: p.name,
	}, nil
}

func (p *demoProvider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	ch := make(chan llmtrace.StreamChunk)
	go func() {
		defer close(ch)
		ch <- llmtrace.StreamChunk{Content: "Demo "}
		ch <- llmtrace.StreamChunk{Content: "streaming "}
		ch <- llmtrace.StreamChunk{Content: "response"}
		ch <- llmtrace.StreamChunk{
			Content: "",
			Usage: &llmtrace.Usage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		}
	}()
	return ch, nil
}
