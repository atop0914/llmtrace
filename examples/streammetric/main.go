// Command streammetric demonstrates streaming performance metrics collection.
//
// This example shows how to use the streammetric package to track
// Time to First Token (TTFT), inter-chunk latency, and tokens per second
// for streaming LLM responses.
//
// Usage:
//
//	export OPENAI_API_KEY="sk-..."
//	go run ./examples/streammetric
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/provider/openai"
	"github.com/atop0914/llmtrace/streammetric"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("Set OPENAI_API_KEY environment variable")
	}

	provider := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithModel("gpt-4o-mini"),
	)

	tracer := llmtrace.NewTracer("streammetric-demo",
		llmtrace.WithProvider("openai"),
	)

	ctx := context.Background()
	req := &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "Explain quantum computing in 3 sentences."},
		},
		MaxTokens: llmtrace.IntPtr(200),
	}

	// Method 1: Direct collector usage
	fmt.Println("=== Method 1: Direct Collector ===")
	collector := streammetric.NewCollector()

	ch, err := tracer.ChatStream(ctx, req, provider)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	// Wrap the stream channel for metrics collection
	wrappedCh := collector.Wrap(ch)

	fmt.Print("Response: ")
	for chunk := range wrappedCh {
		if chunk.Error != nil {
			fmt.Printf("\nError: %v\n", chunk.Error)
			break
		}
		fmt.Print(chunk.Content)
	}

	// Get metrics after stream completes
	m := collector.Metrics()
	fmt.Printf("\n\nStream Metrics:\n")
	fmt.Printf("  Time to First Token: %v\n", m.TTFT)
	fmt.Printf("  Total Duration:      %v\n", m.TotalDuration)
	fmt.Printf("  Chunks:              %d\n", m.ChunkCount)
	fmt.Printf("  Output Tokens:       %d\n", m.TotalTokens)
	fmt.Printf("  Tokens/sec:          %.1f\n", m.TokensPerSecond)
	fmt.Printf("  Avg Chunk Latency:   %v\n", m.AvgInterChunkLatency)
	fmt.Printf("  Min Chunk Latency:   %v\n", m.MinInterChunkLatency)
	fmt.Printf("  Max Chunk Latency:   %v\n", m.MaxInterChunkLatency)
	fmt.Printf("  P50 Chunk Latency:   %v\n", m.P50InterChunkLatency)
	fmt.Printf("  P99 Chunk Latency:   %v\n", m.P99InterChunkLatency)

	// Method 2: StreamMiddleware (automatic per-call metrics)
	fmt.Println("\n=== Method 2: StreamMiddleware ===")

	metricsMW := streammetric.WithStreamMetrics(func(req *llmtrace.Request, m streammetric.Metrics) {
		fmt.Printf("[metrics] model=%s ttft=%v tps=%.1f chunks=%d\n",
			req.Model, m.TTFT, m.TokensPerSecond, m.ChunkCount)
	})

	// Use middleware with a new request
	req2 := &llmtrace.Request{
		Model: "gpt-4o-mini",
		Messages: []llmtrace.Message{
			{Role: llmtrace.RoleUser, Content: "What is 2+2?"},
		},
		MaxTokens: llmtrace.IntPtr(50),
	}

	streamFn := llmtrace.ChainStream(metricsMW)(provider.Stream)
	ch2, err := streamFn(ctx, req2)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	fmt.Print("Response: ")
	for chunk := range ch2 {
		if chunk.Error != nil {
			fmt.Printf("\nError: %v\n", chunk.Error)
			break
		}
		fmt.Print(chunk.Content)
	}
	fmt.Println()

	// Wait for async metrics callback
	time.Sleep(100 * time.Millisecond)
}
