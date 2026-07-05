// Package prompt provides versioned prompt template management for LLM applications.
//
// Prompt templates use Go's text/template syntax for variable interpolation,
// with support for versioning, template chaining, A/B testing, and integration
// with the tokencount package for pre-call token estimation.
//
// Usage:
//
//	reg := prompt.NewRegistry()
//	reg.Register(prompt.Template{
//	    Name:    "summarizer",
//	    Version: "1.0",
//	    Content: "Summarize the following text in {{.Style}} style:\n\n{{.Text}}",
//	    Vars:    []prompt.VarDef{{Name: "Style", Required: true}, {Name: "Text", Required: true}},
//	})
//
//	msgs, err := reg.Render("summarizer", map[string]string{
//	    "Style": "concise",
//	    "Text":  "Long article...",
//	})
package prompt

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// VarDef describes a template variable.
type VarDef struct {
	// Name is the variable name (matches {{.Name}} in the template).
	Name string
	// Required indicates whether the variable must be provided.
	Required bool
	// Default is the fallback value if the variable is not provided.
	Default string
	// Description explains what this variable is for.
	Description string
}

// Template is a versioned prompt template.
type Template struct {
	// Name identifies the template (e.g. "summarizer", "code-review").
	Name string
	// Version is a semver or label (e.g. "1.0", "v2", "stable").
	Version string
	// Content is the Go template text.
	Content string
	// Vars declares the expected variables.
	Vars []VarDef
	// Tags for categorization (e.g. ["summarization", "production"]).
	Tags []string
}

// RenderedMessage is a single message produced by rendering a template.
type RenderedMessage struct {
	Role    string
	Content string
}

// Registry stores and manages versioned prompt templates.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]map[string]Template // name -> version -> Template
}

// NewRegistry creates an empty template registry.
func NewRegistry() *Registry {
	return &Registry{
		templates: make(map[string]map[string]Template),
	}
}

// Register adds or updates a template. Overwrites if name+version already exists.
func (r *Registry) Register(t Template) error {
	if t.Name == "" {
		return fmt.Errorf("prompt: template name is required")
	}
	if t.Version == "" {
		return fmt.Errorf("prompt: template version is required")
	}
	// Validate that the template compiles
	if _, err := r.compile(t); err != nil {
		return fmt.Errorf("prompt: template %s@%s: %w", t.Name, t.Version, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.templates[t.Name] == nil {
		r.templates[t.Name] = make(map[string]Template)
	}
	r.templates[t.Name][t.Version] = t
	return nil
}

// Get retrieves a template by name and version.
func (r *Registry) Get(name, version string) (Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.templates[name]
	if !ok {
		return Template{}, fmt.Errorf("prompt: template %q not found", name)
	}
	t, ok := versions[version]
	if !ok {
		return Template{}, fmt.Errorf("prompt: template %q version %q not found", name, version)
	}
	return t, nil
}

// Latest returns the template with the highest version (lexicographic sort).
func (r *Registry) Latest(name string) (Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.templates[name]
	if !ok || len(versions) == 0 {
		return Template{}, fmt.Errorf("prompt: template %q not found", name)
	}
	var sorted []string
	for v := range versions {
		sorted = append(sorted, v)
	}
	sort.Strings(sorted)
	return versions[sorted[len(sorted)-1]], nil
}

// Versions returns all registered versions for a template name.
func (r *Registry) Versions(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.templates[name]
	if !ok {
		return nil
	}
	var result []string
	for v := range versions {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// List returns all registered template names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Delete removes a specific version of a template.
func (r *Registry) Delete(name, version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	versions, ok := r.templates[name]
	if !ok {
		return fmt.Errorf("prompt: template %q not found", name)
	}
	if _, ok := versions[version]; !ok {
		return fmt.Errorf("prompt: template %q version %q not found", name, version)
	}
	delete(versions, version)
	if len(versions) == 0 {
		delete(r.templates, name)
	}
	return nil
}

// Render renders a template with the given variables, returning messages.
// The template Content is treated as a user message. For system+user patterns,
// use Chain or register a template with a role marker.
func (r *Registry) Render(name string, vars map[string]string) ([]RenderedMessage, error) {
	return r.RenderVersion(name, "", vars)
}

// RenderVersion renders a specific version (empty string = latest).
func (r *Registry) RenderVersion(name, version string, vars map[string]string) ([]RenderedMessage, error) {
	var t Template
	var err error
	if version == "" {
		t, err = r.Latest(name)
	} else {
		t, err = r.Get(name, version)
	}
	if err != nil {
		return nil, err
	}
	return r.renderTemplate(t, vars)
}

// Validate checks that all required variables are provided.
func (r *Registry) Validate(name string, vars map[string]string) error {
	return r.ValidateVersion(name, "", vars)
}

// ValidateVersion checks variables against a specific template version.
func (r *Registry) ValidateVersion(name, version string, vars map[string]string) error {
	var t Template
	var err error
	if version == "" {
		t, err = r.Latest(name)
	} else {
		t, err = r.Get(name, version)
	}
	if err != nil {
		return err
	}
	return r.validateVars(t, vars)
}

func (r *Registry) validateVars(t Template, vars map[string]string) error {
	for _, v := range t.Vars {
		val, ok := vars[v.Name]
		if (!ok || val == "") && v.Required && v.Default == "" {
			return fmt.Errorf("prompt: required variable %q is missing", v.Name)
		}
	}
	return nil
}

func (r *Registry) mergeVars(t Template, vars map[string]string) map[string]string {
	merged := make(map[string]string, len(t.Vars))
	// Apply defaults first
	for _, v := range t.Vars {
		if v.Default != "" {
			merged[v.Name] = v.Default
		}
	}
	// Override with provided vars
	for k, v := range vars {
		merged[k] = v
	}
	return merged
}

func (r *Registry) compile(t Template) (*template.Template, error) {
	return template.New(t.Name).Parse(t.Content)
}

func (r *Registry) renderTemplate(t Template, vars map[string]string) ([]RenderedMessage, error) {
	if err := r.validateVars(t, vars); err != nil {
		return nil, err
	}
	merged := r.mergeVars(t, vars)

	tmpl, err := r.compile(t)
	if err != nil {
		return nil, fmt.Errorf("prompt: template compile error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, merged); err != nil {
		return nil, fmt.Errorf("prompt: template execution error: %w", err)
	}

	return []RenderedMessage{
		{Role: "user", Content: buf.String()},
	}, nil
}

// Chain combines multiple templates into a sequence of messages.
// The first template is rendered as a system message, remaining as user messages.
func (r *Registry) Chain(names []string, vars map[string]string) ([]RenderedMessage, error) {
	var msgs []RenderedMessage
	for i, name := range names {
		rendered, err := r.Render(name, vars)
		if err != nil {
			return nil, fmt.Errorf("prompt: chain step %d (%s): %w", i, name, err)
		}
		// First template becomes system message
		if i == 0 && len(rendered) > 0 {
			rendered[0].Role = "system"
		}
		msgs = append(msgs, rendered...)
	}
	return msgs, nil
}

// ChainVersion combines specific versions of templates.
func (r *Registry) ChainVersion(entries []ChainEntry, vars map[string]string) ([]RenderedMessage, error) {
	var msgs []RenderedMessage
	for i, entry := range entries {
		rendered, err := r.RenderVersion(entry.Name, entry.Version, vars)
		if err != nil {
			return nil, fmt.Errorf("prompt: chain step %d (%s@%s): %w", i, entry.Name, entry.Version, err)
		}
		if entry.Role != "" {
			for j := range rendered {
				rendered[j].Role = entry.Role
			}
		} else if i == 0 && len(rendered) > 0 {
			rendered[0].Role = "system"
		}
		msgs = append(msgs, rendered...)
	}
	return msgs, nil
}

// ChainEntry specifies a template and optional role override for chaining.
type ChainEntry struct {
	Name    string
	Version string
	Role    string
}

// EstimateTokens estimates the total token count for rendered messages.
// Uses a simple heuristic: ~4 chars per token (consistent with tokencount).
func EstimateTokens(msgs []RenderedMessage) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
		if len(m.Content)%4 > 0 {
			total++
		}
		total += 4 // overhead per message (role, formatting)
	}
	return total
}

// Variables extracts the variable names referenced in a template.
func Variables(content string) ([]string, error) {
	if _, err := template.New("extract").Parse(content); err != nil {
		return nil, err
	}
	return VariablesFromContent(content), nil
}

// VariablesFromContent extracts variable names from template content using string parsing.
func VariablesFromContent(content string) []string {
	var vars []string
	seen := make(map[string]bool)
	// Find {{.VarName}} patterns
	s := content
	for {
		idx := strings.Index(s, "{{.")
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx:], "}}")
		if end < 0 {
			break
		}
		name := s[idx+3 : idx+end]
		name = strings.TrimSpace(name)
		// Skip pipeline actions (e.g., {{.Var | printf}})
		if pipeIdx := strings.Index(name, "|"); pipeIdx >= 0 {
			name = strings.TrimSpace(name[:pipeIdx])
		}
		if name != "" && !seen[name] {
			seen[name] = true
			vars = append(vars, name)
		}
		s = s[idx+end:]
	}
	sort.Strings(vars)
	return vars
}
