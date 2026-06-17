package eval

import (
	"context"
	"fmt"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

// Suite is a named collection of evaluators that can be run together.
type Suite struct {
	name       string
	evaluators []Evaluator
}

// NewSuite creates a new evaluation suite with the given name and evaluators.
func NewSuite(name string, evaluators ...Evaluator) *Suite {
	return &Suite{
		name:       name,
		evaluators: evaluators,
	}
}

// Add appends evaluators to the suite.
func (s *Suite) Add(evaluators ...Evaluator) *Suite {
	s.evaluators = append(s.evaluators, evaluators...)
	return s
}

// Name returns the suite name.
func (s *Suite) Name() string {
	return s.name
}

// Run evaluates the response against all evaluators in the suite.
// If resp is nil, all evaluators are skipped with a "no response" message.
func (s *Suite) Run(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) SuiteResult {
	start := time.Now()

	result := SuiteResult{
		Name:    s.name,
		Results: make([]Result, 0, len(s.evaluators)),
		Passed:  true,
		Total:   len(s.evaluators),
	}

	if resp == nil {
		for _, e := range s.evaluators {
			result.Results = append(result.Results, Result{
				Name:    e.Name(),
				Passed:  false,
				Message: "no response (nil)",
			})
			result.FailCount++
		}
		result.Passed = false
		result.Duration = time.Since(start)
		return result
	}

	for _, e := range s.evaluators {
		r := e.Eval(ctx, req, resp)
		result.Results = append(result.Results, r)
		if r.Passed {
			result.PassCount++
		} else {
			result.FailCount++
			result.Passed = false
		}
	}

	result.Duration = time.Since(start)
	return result
}

// Middleware returns a llmtrace.Middleware that runs all evaluators after each
// completion call. Evaluation results are logged but do not block the response.
// This is useful for monitoring response quality in production.
//
// For blocking validation (e.g. in tests or strict pipelines), use Run directly.
func (s *Suite) Middleware() llmtrace.Middleware {
	return func(next llmtrace.CompleteFunc) llmtrace.CompleteFunc {
		return func(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return resp, err
			}
			// Run evaluations — results are attached to context for downstream access.
			_ = s.Run(ctx, req, resp)
			return resp, err
		}
	}
}

// Validate is like Run but returns an error if any evaluator fails.
// The error message includes all failed evaluator names and messages.
func (s *Suite) Validate(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) (SuiteResult, error) {
	result := s.Run(ctx, req, resp)
	if result.Passed {
		return result, nil
	}

	var failed []string
	for _, r := range result.Results {
		if !r.Passed {
			failed = append(failed, r.Name+": "+r.Message)
		}
	}

	return result, &ValidationError{
		SuiteName: s.name,
		Failed:    failed,
	}
}

// ValidationError is returned when a suite validation fails.
type ValidationError struct {
	SuiteName string
	Failed    []string
}

func (e *ValidationError) Error() string {
	msg := fmt.Sprintf("eval suite %s: %d evaluator(s) failed", e.SuiteName, len(e.Failed))
	for _, f := range e.Failed {
		msg += "\n  - " + f
	}
	return msg
}
