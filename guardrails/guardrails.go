// Package guardrails provides composable input and output validators for LLM calls.
//
// Guardrails enforce policies in real-time as part of the middleware pipeline,
// validating prompts before they reach the LLM and checking responses before
// they are returned to the caller. This is distinct from:
//   - Sanitizer: which redacts PII after the fact
//   - Eval: which runs post-hoc quality assessments
//
// Usage:
//
//	gate := guardrails.NewGate(
//	    guardrails.WithInputRules(
//	        guardrails.MaxPromptLength(4096),
//	        guardrails.BlockedTerms([]string{"jailbreak", "ignore instructions"}),
//	    ),
//	    guardrails.WithOutputRules(
//	        guardrails.MinResponseLength(10),
//	        guardrails.RequiredFinishReason("stop"),
//	    ),
//	)
//	gate.OnViolation(func(v guardrails.Violation) { log.Println(v) })
//
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(gate.Middleware()),
//	)
package guardrails

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/atop0914/llmtrace"
)

// Side indicates whether a rule applies to input or output.
type Side int

const (
	// SideInput rules validate the request before the LLM call.
	SideInput Side = iota
	// SideOutput rules validate the response after the LLM call.
	SideOutput
)

func (s Side) String() string {
	if s == SideInput {
		return "input"
	}
	return "output"
}

// Severity indicates how a violation should be handled.
type Severity int

const (
	// SeverityWarn logs the violation but allows the call to proceed.
	SeverityWarn Severity = iota
	// SeverityBlock returns an error and prevents the response from being returned.
	SeverityBlock
)

func (s Severity) String() string {
	if s == SeverityWarn {
		return "warn"
	}
	return "block"
}

// Violation describes a rule that was triggered.
type Violation struct {
	// RuleName identifies the rule that was violated.
	RuleName string `json:"rule_name"`
	// Side is input or output.
	Side Side `json:"side"`
	// Severity is warn or block.
	Severity Severity `json:"severity"`
	// Message is a human-readable explanation.
	Message string `json:"message"`
	// Timestamp is when the violation occurred.
	Timestamp time.Time `json:"timestamp"`
	// Model is the model being called (if known).
	Model string `json:"model,omitempty"`
	// Provider is the provider being used (if known).
	Provider string `json:"provider,omitempty"`
}

// Rule is a single validation rule that can check input or output.
type Rule interface {
	// Name returns a human-readable identifier for this rule.
	Name() string
	// WhichSide returns whether this rule applies to input or output.
	WhichSide() Side
	// Severity returns the severity level for violations of this rule.
	Level() Severity
	// ValidateInput checks the request before the LLM call.
	// Return nil if the request passes validation.
	// Only called for SideInput rules.
	ValidateInput(req *llmtrace.Request) error
	// ValidateOutput checks the response after the LLM call.
	// Return nil if the response passes validation.
	// Only called for SideOutput rules.
	ValidateOutput(req *llmtrace.Request, resp *llmtrace.Response) error
}

// GateConfig configures a Gate.
type GateConfig struct {
	// InputRules validates requests before the LLM call.
	InputRules []Rule
	// OutputRules validates responses after the LLM call.
	OutputRules []Rule
	// FailOpen allows the call to proceed even if a block-level rule fails.
	// Default is false (fail-closed): blocked violations return an error.
	FailOpen bool
}

// Option configures a Gate.
type Option func(*GateConfig)

// WithInputRules sets the input validation rules.
func WithInputRules(rules ...Rule) Option {
	return func(c *GateConfig) {
		c.InputRules = append(c.InputRules, rules...)
	}
}

// WithOutputRules sets the output validation rules.
func WithOutputRules(rules ...Rule) Option {
	return func(c *GateConfig) {
		c.OutputRules = append(c.OutputRules, rules...)
	}
}

// WithFailOpen allows calls to proceed even when block-level rules trigger.
func WithFailOpen(failOpen bool) Option {
	return func(c *GateConfig) {
		c.FailOpen = failOpen
	}
}

// Gate enforces input and output validation rules as middleware.
type Gate struct {
	config      GateConfig
	mu          sync.RWMutex
	onViolation func(Violation)
	stats       GateStats
}

// GateStats tracks rule violation counts.
type GateStats struct {
	// TotalViolations is the total number of violations detected.
	TotalViolations int64 `json:"total_violations"`
	// BlockedCalls is the number of calls blocked by block-level rules.
	BlockedCalls int64 `json:"blocked_calls"`
	// WarnedCalls is the number of calls that had warn-level violations.
	WarnedCalls int64 `json:"warned_calls"`
	// ByRule breaks down violation counts by rule name.
	ByRule map[string]int64 `json:"by_rule"`
}

// ErrBlockedByGate is returned when a block-level rule is violated.
type ErrBlockedByGate struct {
	Violations []Violation
}

func (e *ErrBlockedByGate) Error() string {
	rules := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		rules[i] = v.RuleName
	}
	return fmt.Sprintf("blocked by guardrails: %s", strings.Join(rules, ", "))
}

// NewGate creates a new Gate with the given options.
func NewGate(opts ...Option) *Gate {
	cfg := GateConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gate{
		config: cfg,
		stats:  GateStats{ByRule: make(map[string]int64)},
	}
}

// OnViolation registers a callback that is called whenever a rule is violated.
func (g *Gate) OnViolation(fn func(Violation)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onViolation = fn
}

// Stats returns a snapshot of the gate's violation statistics.
func (g *Gate) Stats() GateStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	byRule := make(map[string]int64, len(g.stats.ByRule))
	for k, v := range g.stats.ByRule {
		byRule[k] = v
	}
	return GateStats{
		TotalViolations: g.stats.TotalViolations,
		BlockedCalls:    g.stats.BlockedCalls,
		WarnedCalls:     g.stats.WarnedCalls,
		ByRule:          byRule,
	}
}

// Middleware returns a llmtrace.Middleware that enforces guardrails.
func (g *Gate) Middleware() llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			// Phase 1: validate input
			violations := g.validateInput(req)
			if hasBlock(violations) && !g.config.FailOpen {
				g.recordViolations(violations, req)
				return nil, &ErrBlockedByGate{Violations: violations}
			}

			// Call the next middleware / provider
			resp, err := next(ctx, req)
			if err != nil {
				return resp, err
			}

			// Phase 2: validate output
			outViolations := g.validateOutput(req, resp)
			violations = append(violations, outViolations...)
			if hasBlock(outViolations) && !g.config.FailOpen {
				g.recordViolations(violations, req)
				return nil, &ErrBlockedByGate{Violations: outViolations}
			}

			g.recordViolations(violations, req)
			return resp, nil
		}
	}
}

// StreamMiddleware returns a llmtrace.StreamMiddleware that enforces guardrails on streams.
func (g *Gate) StreamMiddleware() llmtrace.StreamMiddleware {
	return func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			// Phase 1: validate input
			violations := g.validateInput(req)
			if hasBlock(violations) && !g.config.FailOpen {
				g.recordViolations(violations, req)
				return nil, &ErrBlockedByGate{Violations: violations}
			}

			// Call the next middleware / provider
			ch, err := next(ctx, req)
			if err != nil {
				return ch, err
			}

			g.recordViolations(violations, req)

			// Phase 2: wrap channel to validate output chunks
			out := make(chan llmtrace.StreamChunk, 1)
			go func() {
				defer close(out)
				for chunk := range ch {
					if chunk.Error != nil {
						out <- chunk
						continue
					}
					// Validate the chunk content
					if resp := chunkToResponse(req, chunk); resp != nil {
						chunkViolations := g.validateOutput(req, resp)
						if hasBlock(chunkViolations) && !g.config.FailOpen {
							g.recordViolations(chunkViolations, req)
							out <- llmtrace.StreamChunk{Error: &ErrBlockedByGate{Violations: chunkViolations}}
							return
						}
						g.recordViolations(chunkViolations, req)
					}
					out <- chunk
				}
			}()
			return out, nil
		}
	}
}

func (g *Gate) validateInput(req *llmtrace.Request) []Violation {
	var violations []Violation
	for _, rule := range g.config.InputRules {
		if rule.WhichSide() != SideInput {
			continue
		}
		if err := rule.ValidateInput(req); err != nil {
			violations = append(violations, Violation{
				RuleName:  rule.Name(),
				Side:      SideInput,
				Severity:  rule.Level(),
				Message:   err.Error(),
				Timestamp: time.Now(),
				Model:     req.Model,
			})
		}
	}
	return violations
}

func (g *Gate) validateOutput(req *llmtrace.Request, resp *llmtrace.Response) []Violation {
	var violations []Violation
	for _, rule := range g.config.OutputRules {
		if rule.WhichSide() != SideOutput {
			continue
		}
		if err := rule.ValidateOutput(req, resp); err != nil {
			violations = append(violations, Violation{
				RuleName:  rule.Name(),
				Side:      SideOutput,
				Severity:  rule.Level(),
				Message:   err.Error(),
				Timestamp: time.Now(),
				Model:     req.Model,
				Provider:  resp.Provider,
			})
		}
	}
	return violations
}

func (g *Gate) recordViolations(violations []Violation, req *llmtrace.Request) {
	if len(violations) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	hasBlock := false
	hasWarn := false
	for _, v := range violations {
		g.stats.TotalViolations++
		g.stats.ByRule[v.RuleName]++
		if v.Severity == SeverityBlock {
			hasBlock = true
		} else {
			hasWarn = true
		}
		if g.onViolation != nil {
			g.onViolation(v)
		}
	}
	if hasBlock {
		g.stats.BlockedCalls++
	} else if hasWarn {
		g.stats.WarnedCalls++
	}
}

func hasBlock(violations []Violation) bool {
	for _, v := range violations {
		if v.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

func chunkToResponse(req *llmtrace.Request, chunk llmtrace.StreamChunk) *llmtrace.Response {
	if chunk.Content == "" && chunk.Usage == nil {
		return nil
	}
	resp := &llmtrace.Response{
		Model:    req.Model,
		Content:  chunk.Content,
		Provider: req.Model, // best-effort; middleware chain sets this
	}
	if chunk.Usage != nil {
		resp.Usage = *chunk.Usage
	}
	return resp
}
