// Package eval provides automated evaluation of LLM responses.
//
// Evaluators check LLM response quality against configurable criteria such as
// content length, JSON validity, required content, and custom assertions.
// They integrate with the llmtrace middleware pipeline for real-time quality
// monitoring and can be composed into suites for batch evaluation.
//
// Usage:
//
//	eval := eval.NewSuite("quality-checks",
//	    eval.MinLength(10),
//	    eval.MaxLength(4000),
//	    eval.Contains("answer"),
//	    eval.ValidJSON(),
//	)
//
//	// Run as middleware
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(eval.Middleware()),
//	)
//
//	// Or evaluate after the fact
//	result := eval.Run(ctx, req, resp)
package eval

import (
	"context"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

// Evaluator evaluates an LLM response against a single criterion.
type Evaluator interface {
	// Name returns a human-readable name for this evaluator.
	Name() string

	// Eval evaluates the response and returns a Result.
	Eval(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) Result
}

// EvalFunc is a function adapter for the Evaluator interface.
type EvalFunc struct {
	// FnName is the evaluator name.
	FnName string

	// Fn is the evaluation function.
	Fn func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) Result
}

// Name returns the evaluator name.
func (f EvalFunc) Name() string { return f.FnName }

// Eval runs the evaluation function.
func (f EvalFunc) Eval(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) Result {
	return f.Fn(ctx, req, resp)
}

// Result holds the outcome of a single evaluation.
type Result struct {
	// Name is the evaluator name.
	Name string `json:"name"`

	// Passed reports whether the evaluation passed.
	Passed bool `json:"passed"`

	// Score is an optional numeric score (0.0–1.0). Zero means unscored.
	Score float64 `json:"score,omitempty"`

	// Message provides a human-readable explanation.
	Message string `json:"message,omitempty"`

	// Duration is how long the evaluation took.
	Duration time.Duration `json:"duration_ns"`
}

// SuiteResult holds the outcomes of a suite evaluation run.
type SuiteResult struct {
	// Name is the suite name.
	Name string `json:"name"`

	// Results contains individual evaluator outcomes.
	Results []Result `json:"results"`

	// Passed is true only if all evaluators passed.
	Passed bool `json:"passed"`

	// Total is the number of evaluators.
	Total int `json:"total"`

	// PassCount is the number that passed.
	PassCount int `json:"pass_count"`

	// FailCount is the number that failed.
	FailCount int `json:"fail_count"`

	// Duration is the total suite execution time.
	Duration time.Duration `json:"duration_ns"`
}
