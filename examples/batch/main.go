// Command batch demonstrates concurrent async batch execution for LLM requests.
//
// This example shows how to use the batch package to send multiple LLM
// requests concurrently with configurable parallelism, collect aggregate
// metrics, and handle per-item errors.
//
// Usage:
//
//	export OPENAI_API_KEY="***"
//	go run ./examples/batch
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/batch"
	"github.com/atop0914/llmtrace/provider/openai"
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

	ctx := context.Background()

	// Define a batch of 5 questions
	questions := []string{
		"What is Go?",
		"Explain goroutines in 2 sentences.",
		"What is a channel in Go?",
		"How does the garbage collector work in Go?",
		"What are Go interfaces?",
	}

	requests := make([]*llmtrace.Request, len(questions))
	for i, q := range questions {
		requests[i] = &llmtrace.Request{
			Model: "gpt-4o-mini",
			Messages: []llmtrace.Message{
				{Role: llmtrace.RoleUser, Content: q},
			},
			MaxTokens: llmtrace.IntPtr(100),
		}
	}

	// Create a batcher with concurrency limit and progress tracking
	b := batch.New(provider,
		batch.WithMaxConcurrency(3),
		batch.WithTimeout(30*time.Second),
		batch.WithPerItemTimeout(10*time.Second),
		batch.WithOnProgress(func(item int, result *batch.Result) {
			status := "✓"
			if result.Error != nil {
				status = "✗"
			}
			fmt.Printf("  [%s] Item %d completed in %v\n", status, item, result.Latency)
		}),
		batch.WithName("go-faq"),
	)

	fmt.Printf("Sending %d requests with max concurrency 3...\n\n", len(requests))

	start := time.Now()
	resp, err := b.Run(ctx, requests)
	if err != nil {
		log.Fatalf("Batch failed: %v", err)
	}

	fmt.Printf("\n--- Results ---\n")
	for i, r := range resp.Items {
		if r.Error != nil {
			fmt.Printf("Q%d: ERROR: %v\n", i+1, r.Error)
		} else {
			fmt.Printf("Q%d: %s\n", i+1, r.Response.Content)
		}
	}

	fmt.Printf("\n--- Batch Metrics ---\n")
	m := resp.Metrics
	fmt.Printf("  Total Requests:  %d\n", m.TotalRequests)
	fmt.Printf("  Successful:      %d\n", m.Successful)
	fmt.Printf("  Failed:          %d\n", m.Failed)
	fmt.Printf("  Total Tokens:    %d\n", m.TotalTokens)
	fmt.Printf("  Input Tokens:    %d\n", m.InputTokens)
	fmt.Printf("  Output Tokens:   %d\n", m.OutputTokens)
	fmt.Printf("  Wall Clock:      %v\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("  Total Latency:   %v\n", m.TotalLatency)
	fmt.Printf("  Avg Latency:     %v\n", m.AvgLatency)
	fmt.Printf("  Min Latency:     %v\n", m.MinLatency)
	fmt.Printf("  Max Latency:     %v\n", m.MaxLatency)
	fmt.Printf("  Canceled:        %v\n", resp.Canceled)
}
