// Example: Token counting and context window management
//
// This example demonstrates how to use the tokencount package to:
// - Estimate token counts for messages
// - Validate requests against context windows
// - Estimate costs before making API calls
// - Truncate conversations to fit within limits
// - Get model recommendations
package main

import (
	"fmt"
	"strings"

	"github.com/atop0914/llmtrace/tokencount"
)

func main() {
	// Create a manager with default model registry
	mgr := tokencount.NewManager()

	// 1. Estimate tokens for a text
	text := "The quick brown fox jumps over the lazy dog."
	tokens := tokencount.EstimateTokens(text, 4.0)
	fmt.Printf("1. Token estimate for %q: %d tokens\n\n", text, tokens)

	// 2. Validate a request against a model's context window
	messages := []tokencount.Message{
		{Role: "system", Content: "You are a helpful coding assistant."},
		{Role: "user", Content: "Write a Go function to calculate fibonacci numbers."},
	}

	check := mgr.ValidateRequest("gpt-4o", messages, 500)
	fmt.Printf("2. Context check for gpt-4o:\n")
	fmt.Printf("   Fits: %v\n", check.FitsContext)
	fmt.Printf("   Input tokens: %d\n", check.InputTokens)
	fmt.Printf("   Available for output: %d\n", check.AvailableForOutput)
	fmt.Printf("   Suggested max tokens: %d\n", check.SuggestedMaxTokens)
	fmt.Printf("   Usage ratio: %.4f\n\n", check.UsageRatio)

	// 3. Estimate cost
	cost, _ := mgr.EstimateCost("gpt-4o", check.InputTokens, 500)
	fmt.Printf("3. Estimated cost for gpt-4o (%d in + 500 out): $%.6f\n\n", check.InputTokens, cost)

	// 4. Compare costs across models
	fmt.Println("4. Cost comparison (1000 input + 500 output tokens):")
	models := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514", "gemini-2.0-flash"}
	for _, model := range models {
		c, err := mgr.EstimateCost(model, 1000, 500)
		if err != nil {
			fmt.Printf("   %-30s: error: %v\n", model, err)
			continue
		}
		fmt.Printf("   %-30s: $%.6f\n", model, c)
	}

	// 5. Truncate a long conversation to fit
	fmt.Println("\n5. Truncation example:")
	longMessages := []tokencount.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: strings.Repeat("Tell me about Go. ", 500)},
		{Role: "assistant", Content: strings.Repeat("Go is a great language. ", 500)},
		{Role: "user", Content: strings.Repeat("What about concurrency? ", 500)},
	}
	original := len(longMessages)
	truncated, wasTruncated := mgr.TruncateToFit("gpt-4o", longMessages, 1000)
	fmt.Printf("   Original messages: %d\n", original)
	fmt.Printf("   Truncated: %v\n", wasTruncated)
	fmt.Printf("   Remaining messages: %d\n", len(truncated))
	if len(truncated) > 0 {
		fmt.Printf("   First message role: %s\n", truncated[0].Role)
	}

	// 6. Recommend cheapest model
	fmt.Println("\n6. Model recommendation:")
	simpleMessages := []tokencount.Message{
		{Role: "user", Content: "What is 2+2?"},
	}
	recommended := mgr.RecommendModel(simpleMessages, 100)
	fmt.Printf("   For a simple question (100 output tokens): %s\n", recommended)
}
