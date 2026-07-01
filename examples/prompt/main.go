// Example demonstrates the prompt template management package.
package main

import (
	"fmt"
	"log"

	"github.com/atop0914/llmtrace/prompt"
)

func main() {
	// Create a registry
	reg := prompt.NewRegistry()

	// Register versioned templates
	if err := reg.Register(prompt.Template{
		Name:    "system",
		Version: "1.0",
		Content: "You are a helpful {{.Domain}} assistant. Be {{.Tone}}.",
		Vars: []prompt.VarDef{
			{Name: "Domain", Required: true, Default: "general"},
			{Name: "Tone", Required: false, Default: "concise"},
		},
		Tags: []string{"system", "v1"},
	}); err != nil {
		log.Fatal(err)
	}

	if err := reg.Register(prompt.Template{
		Name:    "summarizer",
		Version: "1.0",
		Content: "Summarize the following text in {{.Style}} style:\n\n{{.Text}}",
		Vars: []prompt.VarDef{
			{Name: "Style", Required: true, Default: "concise"},
			{Name: "Text", Required: true},
		},
		Tags: []string{"summarization"},
	}); err != nil {
		log.Fatal(err)
	}

	if err := reg.Register(prompt.Template{
		Name:    "summarizer",
		Version: "2.0",
		Content: "Please provide a {{.Style}} summary of the following. Focus on key points:\n\n{{.Text}}",
		Vars: []prompt.VarDef{
			{Name: "Style", Required: true, Default: "concise"},
			{Name: "Text", Required: true},
		},
		Tags: []string{"summarization", "improved"},
	}); err != nil {
		log.Fatal(err)
	}

	// List templates
	fmt.Println("=== Registered Templates ===")
	for _, name := range reg.List() {
		versions := reg.Versions(name)
		fmt.Printf("  %s: %v\n", name, versions)
	}

	// Render a single template
	fmt.Println("\n=== Render Summarizer v2 ===")
	msgs, err := reg.RenderVersion("summarizer", "2.0", map[string]string{
		"Style": "bullet points",
		"Text":  "Go is a statically typed, compiled language designed at Google. It is syntactically similar to C, but with memory safety, garbage collection, and structural typing.",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range msgs {
		fmt.Printf("[%s] %s\n", m.Role, m.Content)
	}

	// Chain templates (system + user)
	fmt.Println("\n=== Chained Templates ===")
	msgs, err = reg.Chain([]string{"system", "summarizer"}, map[string]string{
		"Domain": "programming",
		"Tone":   "technical but accessible",
		"Style":  "executive summary",
		"Text":   "Go was created in 2007 at Google by Robert Griesemer, Rob Pike, and Ken Thompson.",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range msgs {
		fmt.Printf("[%s] %s\n", m.Role, m.Content)
	}

	// Estimate tokens
	fmt.Printf("\n=== Token Estimation ===\n")
	tokens := prompt.EstimateTokens(msgs)
	fmt.Printf("Estimated tokens: %d\n", tokens)

	// Extract variables from content
	fmt.Println("\n=== Variable Extraction ===")
	vars := prompt.VariablesFromContent("Hello {{.Name}}, welcome to {{.Place}}!")
	fmt.Printf("Variables found: %v\n", vars)

	// A/B testing
	fmt.Println("\n=== A/B Testing ===")
	exp, err := prompt.NewExperiment(prompt.ExperimentConfig{
		Name: "summarizer-ab",
		Variants: []prompt.Variant{
			{Name: "v1-brief", TemplateName: "summarizer", Version: "1.0", Weight: 1.0},
			{Name: "v2-improved", TemplateName: "summarizer", Version: "2.0", Weight: 2.0},
		},
	}, reg)
	if err != nil {
		log.Fatal(err)
	}

	vars2 := map[string]string{"Text": "Go is a programming language."}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		variant, _, err := exp.SelectAndRender(vars2)
		if err != nil {
			log.Fatal(err)
		}
		counts[variant.Name]++
	}
	fmt.Printf("Selection distribution: %v\n", counts)
}
