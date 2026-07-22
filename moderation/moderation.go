// Package moderation provides content filtering for LLM inputs and outputs.
// It supports word/phrase blocklists, regex patterns, PII detection,
// content length limits, and configurable actions (block, redact, log).
package moderation

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Action defines what happens when content matches a rule.
type Action int

const (
	// ActionBlock rejects the content entirely.
	ActionBlock Action = iota
	// ActionRedact replaces matched content with a placeholder.
	ActionRedact
	// ActionLog allows the content but records the match.
	ActionLog
)

func (a Action) String() string {
	switch a {
	case ActionBlock:
		return "block"
	case ActionRedact:
		return "redact"
	case ActionLog:
		return "log"
	default:
		return "unknown"
	}
}

// Severity indicates how critical a rule violation is.
type Severity int

const (
	SeverityLow Severity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Rule is a single content moderation rule.
type Rule struct {
	// Name is a human-readable identifier.
	Name string
	// Description explains what this rule checks.
	Description string
	// Action to take when the rule is triggered.
	Action Action
	// Severity of the violation.
	Severity Severity
	// checkFunc performs the actual content check.
	checkFunc func(text string) []Match
}

// Match represents a single rule violation found in the content.
type Match struct {
	// RuleName is the name of the triggered rule.
	RuleName string
	// Action is the action configured for the rule.
	Action Action
	// Severity of the violation.
	Severity Severity
	// Matched is the actual text that triggered the rule.
	Matched string
	// Start position in the original text (byte offset).
	Start int
	// End position in the original text (byte offset, exclusive).
	End int
	// Replacement is what the matched text was replaced with (for redact action).
	Replacement string
}

// Result is the output of a moderation check.
type Result struct {
	// Allowed indicates whether the content passed moderation.
	Allowed bool
	// Original is the input text.
	Original string
	// Filtered is the text after redaction (same as Original if no redactions).
	Filtered string
	// Matches lists all rule violations found.
	Matches []Match
	// CheckedAt records when the check was performed.
	CheckedAt time.Time
	// Duration of the check.
	Duration time.Duration
}

// Config for the Engine.
type Config struct {
	// DefaultAction when no specific rule action is set.
	DefaultAction Action
	// RedactPlaceholder replaces matched content in redact mode.
	RedactPlaceholder string
	// MaxInputLength in bytes (0 = no limit).
	MaxInputLength int
	// MaxOutputLength in bytes (0 = no limit).
	MaxOutputLength int
	// CaseSensitive word/phrase matching (default: false).
	CaseSensitive bool
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultAction:     ActionBlock,
		RedactPlaceholder: "[REDACTED]",
		MaxInputLength:    0,
		MaxOutputLength:   0,
		CaseSensitive:     false,
	}
}

// Engine performs content moderation checks.
type Engine struct {
	config Config
	rules  []Rule
	mu     sync.RWMutex
}

// New creates a new moderation Engine with the given config.
func New(cfg Config) *Engine {
	if cfg.RedactPlaceholder == "" {
		cfg.RedactPlaceholder = "[REDACTED]"
	}
	return &Engine{config: cfg}
}

// AddRule adds a custom rule to the engine.
func (e *Engine) AddRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, r)
}

// Rules returns a copy of the current rules.
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	cp := make([]Rule, len(e.rules))
	copy(cp, e.rules)
	return cp
}

// Check evaluates the text against all rules and returns the result.
func (e *Engine) Check(ctx context.Context, text string) (*Result, error) {
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("moderation: context canceled: %w", err)
	}

	e.mu.RLock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	cfg := e.config
	e.mu.RUnlock()

	result := &Result{
		Original:  text,
		Filtered:  text,
		Allowed:   true,
		CheckedAt: start,
	}

	// Length check
	if cfg.MaxInputLength > 0 && len(text) > cfg.MaxInputLength {
		result.Allowed = false
		result.Matches = append(result.Matches, Match{
			RuleName: "max_length",
			Action:   ActionBlock,
			Severity: SeverityMedium,
			Matched:  fmt.Sprintf("content length %d exceeds limit %d", len(text), cfg.MaxInputLength),
		})
		result.Duration = time.Since(start)
		return result, nil
	}

	// Apply rules
	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("moderation: context canceled during rule %q: %w", rule.Name, err)
		}
		matches := rule.checkFunc(text)
		for _, m := range matches {
			m.RuleName = rule.Name
			m.Action = rule.Action
			m.Severity = rule.Severity
			result.Matches = append(result.Matches, m)

			if rule.Action == ActionBlock {
				result.Allowed = false
			}
		}
	}

	// Apply redactions
	filtered := text
	for _, m := range result.Matches {
		if m.Action == ActionRedact && m.Start >= 0 && m.End <= len(filtered) && m.Start < m.End {
			placeholder := cfg.RedactPlaceholder
			filtered = filtered[:m.Start] + placeholder + filtered[m.End:]
			// Adjust subsequent match offsets
			delta := len(placeholder) - (m.End - m.Start)
			for j := range result.Matches {
				if result.Matches[j].Start > m.End {
					result.Matches[j].Start += delta
					result.Matches[j].End += delta
				}
			}
		}
	}
	result.Filtered = filtered

	result.Duration = time.Since(start)
	return result, nil
}

// CheckInput is a convenience method for moderating user input.
func (e *Engine) CheckInput(ctx context.Context, text string) (*Result, error) {
	return e.Check(ctx, text)
}

// CheckOutput is a convenience method for moderating LLM output.
// It uses MaxOutputLength instead of MaxInputLength.
func (e *Engine) CheckOutput(ctx context.Context, text string) (*Result, error) {
	e.mu.RLock()
	maxOut := e.config.MaxOutputLength
	e.mu.RUnlock()

	if maxOut > 0 && len(text) > maxOut {
		return &Result{
			Original:  text,
			Filtered:  text,
			Allowed:   false,
			CheckedAt: time.Now(),
			Matches: []Match{{
				RuleName: "max_output_length",
				Action:   ActionBlock,
				Severity: SeverityMedium,
				Matched:  fmt.Sprintf("output length %d exceeds limit %d", len(text), maxOut),
			}},
		}, nil
	}
	return e.Check(ctx, text)
}

// --- Built-in rule constructors ---

// NewWordBlocklist creates a rule that blocks or redacts specific words/phrases.
func NewWordBlocklist(name string, words []string, action Action, severity Severity, caseSensitive bool) Rule {
	normalized := make([]string, len(words))
	for i, w := range words {
		if caseSensitive {
			normalized[i] = w
		} else {
			normalized[i] = strings.ToLower(w)
		}
	}

	return Rule{
		Name:        name,
		Description: fmt.Sprintf("Blocklist with %d words/phrases", len(words)),
		Action:      action,
		Severity:    severity,
		checkFunc: func(text string) []Match {
			var matches []Match
			check := text
			if !caseSensitive {
				check = strings.ToLower(text)
			}
			for _, word := range normalized {
				idx := 0
				for {
					pos := strings.Index(check[idx:], word)
					if pos < 0 {
						break
					}
					absPos := idx + pos
					matches = append(matches, Match{
						Matched: text[absPos : absPos+len(word)],
						Start:   absPos,
						End:     absPos + len(word),
					})
					idx = absPos + len(word)
				}
			}
			return matches
		},
	}
}

// NewRegexRule creates a rule that matches a compiled regex pattern.
func NewRegexRule(name, description string, re *regexp.Regexp, action Action, severity Severity) Rule {
	return Rule{
		Name:        name,
		Description: description,
		Action:      action,
		Severity:    severity,
		checkFunc: func(text string) []Match {
			locs := re.FindAllStringIndex(text, -1)
			matches := make([]Match, 0, len(locs))
			for _, loc := range locs {
				matches = append(matches, Match{
					Matched: text[loc[0]:loc[1]],
					Start:   loc[0],
					End:     loc[1],
				})
			}
			return matches
		},
	}
}

// NewPIIDetector creates a rule that detects common PII patterns:
// email addresses, phone numbers, SSNs, and credit card numbers.
func NewPIIDetector(action Action, severity Severity) Rule {
	patterns := map[string]string{
		"email":       `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
		"phone":       `(\+?1[\s\-]?)?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{4}`,
		"ssn":         `\b\d{3}[\-\s]?\d{2}[\-\s]?\d{4}\b`,
		"credit_card": `\b(?:\d{4}[\-\s]?){3}\d{4}\b`,
	}

	compiled := make(map[string]*regexp.Regexp)
	for name, pat := range patterns {
		compiled[name] = regexp.MustCompile(pat)
	}

	return Rule{
		Name:        "pii_detector",
		Description: "Detects emails, phone numbers, SSNs, and credit card numbers",
		Action:      action,
		Severity:    severity,
		checkFunc: func(text string) []Match {
			var matches []Match
			for piiType, re := range compiled {
				locs := re.FindAllStringIndex(text, -1)
				for _, loc := range locs {
					matches = append(matches, Match{
						Matched: fmt.Sprintf("[PII:%s]", piiType),
						Start:   loc[0],
						End:     loc[1],
					})
				}
			}
			return matches
		},
	}
}

// NewMaxLengthRule creates a rule that triggers when content exceeds a byte length.
func NewMaxLengthRule(maxBytes int, action Action) Rule {
	return Rule{
		Name:        "content_length",
		Description: fmt.Sprintf("Content must not exceed %d bytes", maxBytes),
		Action:      action,
		Severity:    SeverityMedium,
		checkFunc: func(text string) []Match {
			if len(text) > maxBytes {
				return []Match{{
					Matched: fmt.Sprintf("length %d > %d", len(text), maxBytes),
					Start:   0,
					End:     0,
				}}
			}
			return nil
		},
	}
}
