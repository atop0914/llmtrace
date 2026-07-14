// Package main demonstrates the LLM-as-judge evaluation framework.
//
// This example shows how to use the eval package to evaluate LLM responses
// both with rule-based evaluators and LLM-as-judge scoring.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/eval"
)

func main() {
	ctx := context.Background()

	// --- 1. Rule-based Evaluators ---
	fmt.Println("=== Rule-based Evaluation Suite ===")

	suite := eval.NewSuite("quality-checks",
		eval.MinLength(10),
		eval.MaxLength(4000),
		eval.NonEmpty(),
		eval.Contains("quantum"),
		eval.MaxLatency(5*time.Second),
		eval.TokenLimit(500),
		eval.Custom("no_apology", func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) (bool, string) {
			ok := !strings.Contains(resp.Content, "I'm sorry")
			return ok, "response does not contain apology"
		}),
	)

	// Simulate a request and response
	req := &llmtrace.Request{
		Model:    "gpt-4o",
		Messages: []llmtrace.Message{{Role: "user", Content: "Explain quantum computing"}},
	}

	goodResp := &llmtrace.Response{
		Content:      "Quantum computing uses qubits to perform calculations.",
		Model:        "gpt-4o",
		FinishReason: "stop",
		Usage:        llmtrace.Usage{TotalTokens: 150},
	}

	result := suite.Run(ctx, req, goodResp)
	fmt.Printf("Suite: %s\n", result.Name)
	fmt.Printf("Passed: %v (%d/%d)\n", result.Passed, result.PassCount, result.Total)
	for _, r := range result.Results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", status, r.Name, r.Message)
	}
	fmt.Println()

	// --- 2. Individual Evaluators ---
	fmt.Println("--- Individual Evaluators ---")

	tests := []struct {
		name     string
		eval     eval.Evaluator
		response *llmtrace.Response
	}{
		{
			name:     "MinLength(100) — short response",
			eval:     eval.MinLength(100),
			response: &llmtrace.Response{Content: "Too short"},
		},
		{
			name:     "Contains('quantum') — keyword present",
			eval:     eval.Contains("quantum"),
			response: &llmtrace.Response{Content: "Quantum computing is fascinating"},
		},
		{
			name:     "RegexMatch(`\\d+`) — contains numbers",
			eval:     eval.RegexMatch(`\d+`),
			response: &llmtrace.Response{Content: "GPT-4 was released in 2023"},
		},
	}

	for _, tt := range tests {
		r := tt.eval.Eval(ctx, req, tt.response)
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", status, tt.name, r.Message)
	}
	fmt.Println()

	// --- 3. LLM-as-Judge (requires a real provider) ---
	fmt.Println("--- LLM-as-Judge ---")
	fmt.Println("LLM-as-judge requires a real LLM provider to score responses.")
	fmt.Println()
	fmt.Println("Example usage:")
	fmt.Println()
	fmt.Println("  provider := openai.New(openai.WithAPIKey(\"sk-...\"))")
	fmt.Println()
	fmt.Println("  // Create a relevance judge")
	fmt.Println("  judge := eval.NewRelevanceJudge(provider,")
	fmt.Println("      eval.WithJudgeModel(\"gpt-4o\"),")
	fmt.Println("      eval.WithPassThreshold(0.6),")
	fmt.Println("  )")
	fmt.Println()
	fmt.Println("  // Evaluate a response")
	fmt.Println("  result := judge.Eval(ctx, req, resp)")
	fmt.Printf("  %s\n", `fmt.Printf("Score: %.2f, Passed: %v\n", result.Score, result.Passed)`)
	fmt.Println()

	// Show available criteria
	fmt.Println("Available pre-built criteria:")
	criteria := []eval.Criterion{
		eval.Relevance,
		eval.Coherence,
		eval.Helpfulness,
		eval.Toxicity,
		eval.Factuality,
		eval.InstructionFollowing,
	}
	for _, c := range criteria {
		fmt.Printf("  - %s: %s\n", c.Name, c.Description)
	}
	fmt.Println()

	// Show pre-built judges
	fmt.Println("Pre-built judge constructors:")
	fmt.Println("  - NewRelevanceJudge(provider)            — evaluates relevance to query")
	fmt.Println("  - NewQualityJudge(provider)              — overall response quality")
	fmt.Println("  - NewSafetyJudge(provider)               — content safety")
	fmt.Println("  - NewFactualityJudge(provider)           — factual accuracy")
	fmt.Println("  - NewJudge(provider, WithCriteria(...))  — custom criteria")

	fmt.Println()
	fmt.Println("=== Done ===")
}
