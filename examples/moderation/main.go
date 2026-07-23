// Command moderation demonstrates the content moderation package.
//
// It shows how to use blocklist rules, regex patterns, PII detection,
// and the LLM middleware for input/output filtering.
//
// Usage:
//
//	go run ./examples/moderation
package main

import (
	"context"
	"fmt"
	"log"
	"regexp"

	"github.com/atop0914/llmtrace/moderation"
)

func main() {
	ctx := context.Background()

	// Create a moderation engine with default config.
	engine := moderation.New(moderation.DefaultConfig())

	// --- Add rules ---

	// 1. Word blocklist: block specific terms
	engine.AddRule(moderation.NewWordBlocklist(
		"profanity",
		[]string{"spam", "scam", "phishing"},
		moderation.ActionBlock,
		moderation.SeverityHigh,
		false, // case-insensitive
	))

	// 2. Regex rule: detect URLs (log only, don't block)
	engine.AddRule(moderation.NewRegexRule(
		"url_detector",
		"Detects URLs in content",
		regexp.MustCompile(`https?://[^\s]+`),
		moderation.ActionLog,
		moderation.SeverityLow,
	))

	// 3. PII detector: redact emails and phone numbers
	engine.AddRule(moderation.NewPIIDetector(
		moderation.ActionRedact,
		moderation.SeverityMedium,
	))

	// 4. Custom regex: detect credit card patterns (block)
	engine.AddRule(moderation.NewRegexRule(
		"credit_card_strict",
		"Strict credit card number detection",
		regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`),
		moderation.ActionBlock,
		moderation.SeverityCritical,
	))

	// --- Test various inputs ---

	testCases := []struct {
		name  string
		input string
	}{
		{"clean", "Hello, I need help with my Go project."},
		{"spam keyword", "Check out this spam offer!"},
		{"PII email", "Contact me at john@example.com for details."},
		{"PII phone", "Call me at (555) 123-4567 or +1-800-555-0199."},
		{"URL detected", "See https://example.com/docs for more info."},
		{"credit card", "My card number is 4111-1111-1111-1111."},
		{"mixed", "Email user@spam.com about the scam at https://phishing.net"},
	}

	for _, tc := range testCases {
		fmt.Printf("\n--- %s ---\n", tc.name)
		fmt.Printf("Input:  %q\n", tc.input)

		result, err := engine.Check(ctx, tc.input)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		fmt.Printf("Allowed: %v\n", result.Allowed)
		if result.Filtered != result.Original {
			fmt.Printf("Filtered: %q\n", result.Filtered)
		}
		if len(result.Matches) > 0 {
			fmt.Printf("Matches:\n")
			for _, m := range result.Matches {
				fmt.Printf("  - [%s] %s rule %q: %q (pos %d-%d)\n",
					m.Severity, m.Action, m.RuleName, m.Matched, m.Start, m.End)
			}
		}
	}

	// --- Demonstrate middleware integration ---
	fmt.Println("\n--- LLM Middleware Integration ---")
	fmt.Println("Use moderation.Middleware(engine) for input filtering:")
	fmt.Println("  tracer.Use(moderation.Middleware(engine))")
	fmt.Println()
	fmt.Println("Use moderation.OutputMiddleware(engine) for output filtering:")
	fmt.Println("  tracer.Use(moderation.OutputMiddleware(engine))")
	fmt.Println()
	fmt.Println("Check moderation.IsBlocked(err) to detect blocked content errors.")
}
