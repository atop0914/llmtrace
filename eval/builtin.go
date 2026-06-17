package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

// MinLength returns an evaluator that checks the response content has
// at least min characters.
func MinLength(min int) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("min_length(%d)", min),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			length := len(resp.Content)
			passed := length >= min
			msg := fmt.Sprintf("content length %d >= %d", length, min)
			if !passed {
				msg = fmt.Sprintf("content length %d < %d (minimum)", length, min)
			}
			return Result{
				Name:     fmt.Sprintf("min_length(%d)", min),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// MaxLength returns an evaluator that checks the response content has
// at most max characters.
func MaxLength(max int) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("max_length(%d)", max),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			length := len(resp.Content)
			passed := length <= max
			msg := fmt.Sprintf("content length %d <= %d", length, max)
			if !passed {
				msg = fmt.Sprintf("content length %d > %d (maximum)", length, max)
			}
			return Result{
				Name:     fmt.Sprintf("max_length(%d)", max),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// Contains returns an evaluator that checks the response content contains
// the given substring (case-sensitive).
func Contains(substr string) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("contains(%q)", substr),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed := strings.Contains(resp.Content, substr)
			msg := fmt.Sprintf("content contains %q", substr)
			if !passed {
				msg = fmt.Sprintf("content does not contain %q", substr)
			}
			return Result{
				Name:     fmt.Sprintf("contains(%q)", substr),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// ContainsAny returns an evaluator that checks the response content contains
// at least one of the given substrings.
func ContainsAny(substrings ...string) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("contains_any(%v)", substrings),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			for _, s := range substrings {
				if strings.Contains(resp.Content, s) {
					return Result{
						Name:     fmt.Sprintf("contains_any(%v)", substrings),
						Passed:   true,
						Message:  fmt.Sprintf("content contains %q", s),
						Duration: time.Since(start),
					}
				}
			}
			return Result{
				Name:     fmt.Sprintf("contains_any(%v)", substrings),
				Passed:   false,
				Message:  fmt.Sprintf("content does not contain any of %v", substrings),
				Duration: time.Since(start),
			}
		},
	}
}

// NotContains returns an evaluator that checks the response content does NOT
// contain the given substring.
func NotContains(substr string) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("not_contains(%q)", substr),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed := !strings.Contains(resp.Content, substr)
			msg := fmt.Sprintf("content does not contain %q", substr)
			if !passed {
				msg = fmt.Sprintf("content unexpectedly contains %q", substr)
			}
			return Result{
				Name:     fmt.Sprintf("not_contains(%q)", substr),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// ValidJSON returns an evaluator that checks the response content is valid JSON.
func ValidJSON() Evaluator {
	return EvalFunc{
		FnName: "valid_json",
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			var js json.RawMessage
			err := json.Unmarshal([]byte(resp.Content), &js)
			passed := err == nil
			msg := "content is valid JSON"
			if !passed {
				msg = fmt.Sprintf("content is not valid JSON: %v", err)
			}
			return Result{
				Name:     "valid_json",
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// FinishReason returns an evaluator that checks the response's finish reason
// matches one of the expected values (e.g. "stop", "length", "tool_calls").
func FinishReason(expected ...string) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("finish_reason(%v)", expected),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			for _, e := range expected {
				if resp.FinishReason == e {
					return Result{
						Name:     fmt.Sprintf("finish_reason(%v)", expected),
						Passed:   true,
						Message:  fmt.Sprintf("finish reason is %q", resp.FinishReason),
						Duration: time.Since(start),
					}
				}
			}
			return Result{
				Name:     fmt.Sprintf("finish_reason(%v)", expected),
				Passed:   false,
				Message:  fmt.Sprintf("finish reason is %q, expected one of %v", resp.FinishReason, expected),
				Duration: time.Since(start),
			}
		},
	}
}

// RegexMatch returns an evaluator that checks the response content matches
// the given regular expression pattern.
func RegexMatch(pattern string) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("regex_match(%q)", pattern),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			re, err := regexp.Compile(pattern)
			if err != nil {
				return Result{
					Name:     fmt.Sprintf("regex_match(%q)", pattern),
					Passed:   false,
					Message:  fmt.Sprintf("invalid regex pattern: %v", err),
					Duration: time.Since(start),
				}
			}
			passed := re.MatchString(resp.Content)
			msg := fmt.Sprintf("content matches pattern %q", pattern)
			if !passed {
				msg = fmt.Sprintf("content does not match pattern %q", pattern)
			}
			return Result{
				Name:     fmt.Sprintf("regex_match(%q)", pattern),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// NonEmpty returns an evaluator that checks the response content is not empty.
func NonEmpty() Evaluator {
	return EvalFunc{
		FnName: "non_empty",
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed := strings.TrimSpace(resp.Content) != ""
			msg := "content is not empty"
			if !passed {
				msg = "content is empty"
			}
			return Result{
				Name:     "non_empty",
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// TokenLimit returns an evaluator that checks the response token usage
// does not exceed the specified maximum.
func TokenLimit(maxTokens int) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("token_limit(%d)", maxTokens),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			total := resp.Usage.TotalTokens
			passed := total <= maxTokens
			msg := fmt.Sprintf("total tokens %d <= %d", total, maxTokens)
			if !passed {
				msg = fmt.Sprintf("total tokens %d > %d (limit)", total, maxTokens)
			}
			return Result{
				Name:     fmt.Sprintf("token_limit(%d)", maxTokens),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// Custom returns an evaluator from a user-defined function.
// The name parameter is used for reporting, and fn performs the evaluation.
func Custom(name string, fn func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) (bool, string)) Evaluator {
	return EvalFunc{
		FnName: name,
		Fn: func(ctx context.Context, req *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed, msg := fn(ctx, req, resp)
			return Result{
				Name:     name,
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// ResponseID returns an evaluator that checks the response ID is non-empty,
// indicating the provider returned a valid response.
func ResponseID() Evaluator {
	return EvalFunc{
		FnName: "response_id",
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed := resp.ID != ""
			msg := fmt.Sprintf("response ID is %q", resp.ID)
			if !passed {
				msg = "response ID is empty"
			}
			return Result{
				Name:     "response_id",
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

// MaxLatency returns an evaluator that checks the response latency does not
// exceed the specified duration.
func MaxLatency(d time.Duration) Evaluator {
	return EvalFunc{
		FnName: fmt.Sprintf("max_latency(%v)", d),
		Fn: func(_ context.Context, _ *llmtrace.Request, resp *llmtrace.Response) Result {
			start := time.Now()
			passed := resp.Latency <= d
			msg := fmt.Sprintf("latency %v <= %v", resp.Latency, d)
			if !passed {
				msg = fmt.Sprintf("latency %v > %v (limit)", resp.Latency, d)
			}
			return Result{
				Name:     fmt.Sprintf("max_latency(%v)", d),
				Passed:   passed,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}
