package prompt

import (
	"fmt"
	"testing"
)

func BenchmarkRegistry_Render(b *testing.B) {
	reg := NewRegistry()
	reg.Register(Template{
		Name:    "bench",
		Version: "1.0",
		Content: "Summarize in {{.Style}} style:\n\n{{.Text}}",
		Vars: []VarDef{
			{Name: "Style", Required: true, Default: "concise"},
			{Name: "Text", Required: true},
		},
	})
	vars := map[string]string{
		"Style": "formal",
		"Text":  "This is a long text that needs to be summarized by the LLM model.",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := reg.Render("bench", vars)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegistry_RenderLargeTemplate(b *testing.B) {
	reg := NewRegistry()
	// Template with many variables
	content := "System: You are a {{.Role}} assistant.\n"
	for i := 0; i < 20; i++ {
		content += fmt.Sprintf("Section %d: {{.Var%d}}\n", i, i)
	}
	vars := map[string]string{"Role": "helpful"}
	for i := 0; i < 20; i++ {
		vars[fmt.Sprintf("Var%d", i)] = fmt.Sprintf("value_%d", i)
	}
	varDefs := []VarDef{{Name: "Role", Required: true}}
	for i := 0; i < 20; i++ {
		varDefs = append(varDefs, VarDef{Name: fmt.Sprintf("Var%d", i), Required: true})
	}
	reg.Register(Template{
		Name:    "large",
		Version: "1.0",
		Content: content,
		Vars:    varDefs,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := reg.Render("large", vars)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEstimateTokens(b *testing.B) {
	msgs := []RenderedMessage{
		{Role: "system", Content: "You are a helpful assistant that answers questions about Go programming."},
		{Role: "user", Content: "Explain the difference between goroutines and threads in detail."},
		{Role: "assistant", Content: "Goroutines are lightweight threads managed by the Go runtime..."},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(msgs)
	}
}

func BenchmarkExperiment_Select(b *testing.B) {
	reg := NewRegistry()
	for i := 0; i < 10; i++ {
		reg.Register(Template{
			Name:    "qa",
			Version: fmt.Sprintf("%d.0", i),
			Content: fmt.Sprintf("Template variant %d: {{.Q}}", i),
			Vars:    []VarDef{{Name: "Q", Required: true}},
		})
	}
	variants := make([]Variant, 10)
	for i := 0; i < 10; i++ {
		variants[i] = Variant{
			Name:         fmt.Sprintf("v%d", i),
			TemplateName: "qa",
			Version:      fmt.Sprintf("%d.0", i),
			Weight:       1.0,
		}
	}
	exp, _ := NewExperiment(ExperimentConfig{
		Name:     "bench",
		Variants: variants,
	}, reg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exp.Select()
	}
}
