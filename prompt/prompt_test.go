package prompt

import (
	"strings"
	"testing"
)

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()

	t.Run("valid template", func(t *testing.T) {
		err := reg.Register(Template{
			Name:    "greeting",
			Version: "1.0",
			Content: "Hello, {{.Name}}!",
			Vars:    []VarDef{{Name: "Name", Required: true}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		err := reg.Register(Template{
			Version: "1.0",
			Content: "Hello",
		})
		if err == nil {
			t.Fatal("expected error for missing name")
		}
	})

	t.Run("missing version", func(t *testing.T) {
		err := reg.Register(Template{
			Name:    "test",
			Content: "Hello",
		})
		if err == nil {
			t.Fatal("expected error for missing version")
		}
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		err := reg.Register(Template{
			Name:    "bad",
			Version: "1.0",
			Content: "Hello, {{.Name",
		})
		if err == nil {
			t.Fatal("expected error for invalid template")
		}
	})
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "greeting", Version: "1.0", Content: "Hello!"})
	reg.Register(Template{Name: "greeting", Version: "2.0", Content: "Hi!"})

	t.Run("existing", func(t *testing.T) {
		tmpl, err := reg.Get("greeting", "1.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl.Content != "Hello!" {
			t.Errorf("got %q, want %q", tmpl.Content, "Hello!")
		}
	})

	t.Run("nonexistent template", func(t *testing.T) {
		_, err := reg.Get("missing", "1.0")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nonexistent version", func(t *testing.T) {
		_, err := reg.Get("greeting", "99.0")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRegistry_Latest(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "greeting", Version: "1.0", Content: "Hello!"})
	reg.Register(Template{Name: "greeting", Version: "2.0", Content: "Hi!"})

	tmpl, err := reg.Latest("greeting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Version != "2.0" {
		t.Errorf("got version %q, want %q", tmpl.Version, "2.0")
	}

	_, err = reg.Latest("missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegistry_Versions(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "greeting", Version: "1.0", Content: "Hello!"})
	reg.Register(Template{Name: "greeting", Version: "2.0", Content: "Hi!"})
	reg.Register(Template{Name: "greeting", Version: "1.5", Content: "Hey!"})

	versions := reg.Versions("greeting")
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	// Should be sorted
	if versions[0] != "1.0" || versions[1] != "1.5" || versions[2] != "2.0" {
		t.Errorf("unexpected order: %v", versions)
	}

	empty := reg.Versions("missing")
	if len(empty) != 0 {
		t.Errorf("expected empty, got %v", empty)
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "beta", Version: "1.0", Content: "B"})
	reg.Register(Template{Name: "alpha", Version: "1.0", Content: "A"})

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("unexpected order: %v", names)
	}
}

func TestRegistry_Delete(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "greeting", Version: "1.0", Content: "Hello!"})
	reg.Register(Template{Name: "greeting", Version: "2.0", Content: "Hi!"})

	t.Run("delete existing", func(t *testing.T) {
		err := reg.Delete("greeting", "1.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		versions := reg.Versions("greeting")
		if len(versions) != 1 {
			t.Errorf("got %d versions, want 1", len(versions))
		}
	})

	t.Run("delete last version removes template", func(t *testing.T) {
		err := reg.Delete("greeting", "2.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := reg.List()
		for _, n := range names {
			if n == "greeting" {
				t.Error("template should have been removed")
			}
		}
	})

	t.Run("delete nonexistent", func(t *testing.T) {
		err := reg.Delete("missing", "1.0")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRegistry_Render(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{
		Name:    "summarizer",
		Version: "1.0",
		Content: "Summarize in {{.Style}} style:\n\n{{.Text}}",
		Vars: []VarDef{
			{Name: "Style", Required: true, Default: "concise"},
			{Name: "Text", Required: true},
		},
	})

	t.Run("all vars provided", func(t *testing.T) {
		msgs, err := reg.Render("summarizer", map[string]string{
			"Style": "formal",
			"Text":  "Hello world",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		if msgs[0].Role != "user" {
			t.Errorf("got role %q, want %q", msgs[0].Role, "user")
		}
		if !strings.Contains(msgs[0].Content, "formal") {
			t.Errorf("expected 'formal' in content: %s", msgs[0].Content)
		}
	})

	t.Run("default value applied", func(t *testing.T) {
		msgs, err := reg.Render("summarizer", map[string]string{
			"Text": "Hello world",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(msgs[0].Content, "concise") {
			t.Errorf("expected default 'concise' in content: %s", msgs[0].Content)
		}
	})

	t.Run("missing required var", func(t *testing.T) {
		_, err := reg.Render("summarizer", map[string]string{
			"Style": "formal",
		})
		if err == nil {
			t.Fatal("expected error for missing required var")
		}
	})

	t.Run("nonexistent template", func(t *testing.T) {
		_, err := reg.Render("missing", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRegistry_Chain(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{
		Name:    "system",
		Version: "1.0",
		Content: "You are a helpful {{.Role}} assistant.",
		Vars:    []VarDef{{Name: "Role", Default: "general"}},
	})
	reg.Register(Template{
		Name:    "user",
		Version: "1.0",
		Content: "Please help with: {{.Task}}",
		Vars:    []VarDef{{Name: "Task", Required: true}},
	})

	msgs, err := reg.Chain([]string{"system", "user"}, map[string]string{
		"Role": "coding",
		"Task": "write tests",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message should be system, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("second message should be user, got %q", msgs[1].Role)
	}
	if !strings.Contains(msgs[0].Content, "coding") {
		t.Errorf("expected 'coding' in system message: %s", msgs[0].Content)
	}
}

func TestRegistry_ChainVersion(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{Name: "sys", Version: "1.0", Content: "System v1"})
	reg.Register(Template{Name: "sys", Version: "2.0", Content: "System v2"})
	reg.Register(Template{Name: "ask", Version: "1.0", Content: "Ask: {{.Q}}"})

	msgs, err := reg.ChainVersion([]ChainEntry{
		{Name: "sys", Version: "2.0", Role: "system"},
		{Name: "ask", Version: "1.0"},
	}, map[string]string{"Q": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected system role, got %q", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "v2") {
		t.Errorf("expected v2 content: %s", msgs[0].Content)
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []RenderedMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello world!"},
	}
	tokens := EstimateTokens(msgs)
	// "You are helpful." = 16 chars -> 4 tokens + 4 overhead = 8
	// "Hello world!" = 12 chars -> 3 tokens + 4 overhead = 7
	// Total = 15
	if tokens != 15 {
		t.Errorf("got %d tokens, want 15", tokens)
	}

	empty := EstimateTokens(nil)
	if empty != 0 {
		t.Errorf("got %d tokens for nil, want 0", empty)
	}
}

func TestVariablesFromContent(t *testing.T) {
	tests := []struct {
		content string
		want    []string
	}{
		{"Hello {{.Name}}!", []string{"Name"}},
		{"{{.A}} and {{.B}}", []string{"A", "B"}},
		{"{{.A}} and {{.A}}", []string{"A"}}, // dedup
		{"no variables here", nil},
		{"{{.First}} {{.Second}} {{.Third}}", []string{"First", "Second", "Third"}},
	}
	for _, tt := range tests {
		got := VariablesFromContent(tt.content)
		if len(got) != len(tt.want) {
			t.Errorf("VariablesFromContent(%q) = %v, want %v", tt.content, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("VariablesFromContent(%q)[%d] = %q, want %q", tt.content, i, got[i], tt.want[i])
			}
		}
	}
}

func TestRegistry_Validate(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Template{
		Name:    "test",
		Version: "1.0",
		Content: "{{.Required}} and {{.Optional}}",
		Vars: []VarDef{
			{Name: "Required", Required: true},
			{Name: "Optional", Required: false, Default: "default"},
		},
	})

	t.Run("all provided", func(t *testing.T) {
		err := reg.Validate("test", map[string]string{
			"Required": "val",
			"Optional": "custom",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("optional missing but has default", func(t *testing.T) {
		err := reg.Validate("test", map[string]string{
			"Required": "val",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("required missing", func(t *testing.T) {
		err := reg.Validate("test", map[string]string{
			"Optional": "custom",
		})
		if err == nil {
			t.Fatal("expected error for missing required var")
		}
	})
}
