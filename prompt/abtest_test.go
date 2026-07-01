package prompt

import (
	"strings"
	"testing"
)

func setupTestRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(Template{
		Name:    "qa",
		Version: "1.0",
		Content: "Answer briefly: {{.Question}}",
		Vars:    []VarDef{{Name: "Question", Required: true}},
	})
	reg.Register(Template{
		Name:    "qa",
		Version: "2.0",
		Content: "Provide a detailed answer to: {{.Question}}",
		Vars:    []VarDef{{Name: "Question", Required: true}},
	})
	return reg
}

func TestNewExperiment(t *testing.T) {
	reg := setupTestRegistry()

	t.Run("valid experiment", func(t *testing.T) {
		exp, err := NewExperiment(ExperimentConfig{
			Name: "qa-test",
			Variants: []Variant{
				{Name: "brief", TemplateName: "qa", Version: "1.0", Weight: 1.0},
				{Name: "detailed", TemplateName: "qa", Version: "2.0", Weight: 1.0},
			},
		}, reg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exp == nil {
			t.Fatal("expected non-nil experiment")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		_, err := NewExperiment(ExperimentConfig{
			Variants: []Variant{
				{Name: "a", TemplateName: "qa", Version: "1.0", Weight: 1.0},
				{Name: "b", TemplateName: "qa", Version: "2.0", Weight: 1.0},
			},
		}, reg)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("too few variants", func(t *testing.T) {
		_, err := NewExperiment(ExperimentConfig{
			Name: "one",
			Variants: []Variant{
				{Name: "a", TemplateName: "qa", Version: "1.0", Weight: 1.0},
			},
		}, reg)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid weight", func(t *testing.T) {
		_, err := NewExperiment(ExperimentConfig{
			Name: "bad-weight",
			Variants: []Variant{
				{Name: "a", TemplateName: "qa", Version: "1.0", Weight: 0},
				{Name: "b", TemplateName: "qa", Version: "2.0", Weight: 1.0},
			},
		}, reg)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nonexistent template", func(t *testing.T) {
		_, err := NewExperiment(ExperimentConfig{
			Name: "missing-tmpl",
			Variants: []Variant{
				{Name: "a", TemplateName: "qa", Version: "1.0", Weight: 1.0},
				{Name: "b", TemplateName: "nope", Version: "1.0", Weight: 1.0},
			},
		}, reg)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestExperiment_Select(t *testing.T) {
	reg := setupTestRegistry()
	exp, _ := NewExperiment(ExperimentConfig{
		Name: "qa-test",
		Variants: []Variant{
			{Name: "brief", TemplateName: "qa", Version: "1.0", Weight: 1.0},
			{Name: "detailed", TemplateName: "qa", Version: "2.0", Weight: 1.0},
		},
	}, reg)

	// Select multiple times and verify we get valid variant names
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		v := exp.Select()
		if v.Name != "brief" && v.Name != "detailed" {
			t.Errorf("unexpected variant: %s", v.Name)
		}
		seen[v.Name] = true
	}
	// With enough iterations, both should appear
	if !seen["brief"] || !seen["detailed"] {
		t.Errorf("expected both variants to be selected, got %v", seen)
	}
}

func TestExperiment_SelectWeighted(t *testing.T) {
	reg := setupTestRegistry()
	exp, _ := NewExperiment(ExperimentConfig{
		Name: "weighted",
		Variants: []Variant{
			{Name: "rare", TemplateName: "qa", Version: "1.0", Weight: 0.01},
			{Name: "common", TemplateName: "qa", Version: "2.0", Weight: 0.99},
		},
	}, reg)

	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		v := exp.Select()
		counts[v.Name]++
	}
	// "common" should be selected much more than "rare"
	if counts["common"] < 900 {
		t.Errorf("expected common to be selected ~990 times, got %d", counts["common"])
	}
}

func TestExperiment_SelectAndRender(t *testing.T) {
	reg := setupTestRegistry()
	exp, _ := NewExperiment(ExperimentConfig{
		Name: "render-test",
		Variants: []Variant{
			{Name: "brief", TemplateName: "qa", Version: "1.0", Weight: 1.0},
			{Name: "detailed", TemplateName: "qa", Version: "2.0", Weight: 1.0},
		},
	}, reg)

	variant, msgs, err := exp.SelectAndRender(map[string]string{
		"Question": "What is Go?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected non-empty messages")
	}
	if !strings.Contains(msgs[0].Content, "What is Go?") {
		t.Errorf("expected question in output: %s", msgs[0].Content)
	}
	if variant.Name == "" {
		t.Error("variant name should not be empty")
	}
}

func TestExperiment_Stats(t *testing.T) {
	reg := setupTestRegistry()
	exp, _ := NewExperiment(ExperimentConfig{
		Name: "stats-test",
		Variants: []Variant{
			{Name: "brief", TemplateName: "qa", Version: "1.0", Weight: 1.0},
			{Name: "detailed", TemplateName: "qa", Version: "2.0", Weight: 1.0},
		},
	}, reg)

	for i := 0; i < 50; i++ {
		exp.Select()
	}

	stats := exp.Stats()
	total := stats["brief"] + stats["detailed"]
	if total != 50 {
		t.Errorf("total should be 50, got %d", total)
	}
}

func TestExperiment_Reset(t *testing.T) {
	reg := setupTestRegistry()
	exp, _ := NewExperiment(ExperimentConfig{
		Name: "reset-test",
		Variants: []Variant{
			{Name: "brief", TemplateName: "qa", Version: "1.0", Weight: 1.0},
			{Name: "detailed", TemplateName: "qa", Version: "2.0", Weight: 1.0},
		},
	}, reg)

	for i := 0; i < 10; i++ {
		exp.Select()
	}
	exp.Reset()
	stats := exp.Stats()
	if len(stats) != 0 {
		t.Errorf("expected empty stats after reset, got %v", stats)
	}
}
