package guardrails

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atop0914/llmtrace"
)

// --- Input Rules ---

// maxPromptLengthRule validates that the total prompt length doesn't exceed a limit.
type maxPromptLengthRule struct {
	maxLen int
}

// MaxPromptLength creates a rule that blocks prompts exceeding the given character count.
func MaxPromptLength(maxLen int) Rule {
	return &maxPromptLengthRule{maxLen: maxLen}
}

func (r *maxPromptLengthRule) Name() string    { return "max_prompt_length" }
func (r *maxPromptLengthRule) WhichSide() Side { return SideInput }
func (r *maxPromptLengthRule) Level() Severity { return SeverityBlock }
func (r *maxPromptLengthRule) ValidateInput(req *llmtrace.Request) error {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	if total > r.maxLen {
		return fmt.Errorf("prompt length %d exceeds maximum %d", total, r.maxLen)
	}
	return nil
}
func (r *maxPromptLengthRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// minPromptLengthRule validates minimum prompt length.
type minPromptLengthRule struct {
	minLen int
}

// MinPromptLength creates a rule that blocks prompts shorter than the given character count.
func MinPromptLength(minLen int) Rule {
	return &minPromptLengthRule{minLen: minLen}
}

func (r *minPromptLengthRule) Name() string    { return "min_prompt_length" }
func (r *minPromptLengthRule) WhichSide() Side { return SideInput }
func (r *minPromptLengthRule) Level() Severity { return SeverityWarn }
func (r *minPromptLengthRule) ValidateInput(req *llmtrace.Request) error {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	if total < r.minLen {
		return fmt.Errorf("prompt length %d is below minimum %d", total, r.minLen)
	}
	return nil
}
func (r *minPromptLengthRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// maxMessagesRule validates the number of messages in the conversation.
type maxMessagesRule struct {
	maxCount int
}

// MaxMessages creates a rule that blocks requests with more than the given number of messages.
func MaxMessages(maxCount int) Rule {
	return &maxMessagesRule{maxCount: maxCount}
}

func (r *maxMessagesRule) Name() string    { return "max_messages" }
func (r *maxMessagesRule) WhichSide() Side { return SideInput }
func (r *maxMessagesRule) Level() Severity { return SeverityBlock }
func (r *maxMessagesRule) ValidateInput(req *llmtrace.Request) error {
	if len(req.Messages) > r.maxCount {
		return fmt.Errorf("message count %d exceeds maximum %d", len(req.Messages), r.maxCount)
	}
	return nil
}
func (r *maxMessagesRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// blockedTermsRule checks for forbidden terms in the prompt.
type blockedTermsRule struct {
	terms    []string
	severity Severity
}

// BlockedTerms creates a rule that blocks prompts containing any of the given terms (case-insensitive).
func BlockedTerms(terms []string) Rule {
	return &blockedTermsRule{terms: terms, severity: SeverityBlock}
}

// WarnedTerms creates a rule that warns (but allows) prompts containing any of the given terms.
func WarnedTerms(terms []string) Rule {
	return &blockedTermsRule{terms: terms, severity: SeverityWarn}
}

func (r *blockedTermsRule) Name() string    { return "blocked_terms" }
func (r *blockedTermsRule) WhichSide() Side { return SideInput }
func (r *blockedTermsRule) Level() Severity { return r.severity }
func (r *blockedTermsRule) ValidateInput(req *llmtrace.Request) error {
	for _, m := range req.Messages {
		lower := strings.ToLower(m.Content)
		for _, term := range r.terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				return fmt.Errorf("prompt contains blocked term: %q", term)
			}
		}
	}
	return nil
}
func (r *blockedTermsRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// blockedPatternRule checks for forbidden regex patterns in the prompt.
type blockedPatternRule struct {
	name     string
	pattern  *regexp.Regexp
	severity Severity
}

// BlockedPattern creates a rule that blocks prompts matching the given regex.
func BlockedPattern(name string, pattern *regexp.Regexp) Rule {
	return &blockedPatternRule{name: name, pattern: pattern, severity: SeverityBlock}
}

// WarnedPattern creates a rule that warns on prompts matching the given regex.
func WarnedPattern(name string, pattern *regexp.Regexp) Rule {
	return &blockedPatternRule{name: name, pattern: pattern, severity: SeverityWarn}
}

func (r *blockedPatternRule) Name() string    { return r.name }
func (r *blockedPatternRule) WhichSide() Side { return SideInput }
func (r *blockedPatternRule) Level() Severity { return r.severity }
func (r *blockedPatternRule) ValidateInput(req *llmtrace.Request) error {
	for _, m := range req.Messages {
		if r.pattern.MatchString(m.Content) {
			return fmt.Errorf("prompt matches blocked pattern: %s", r.name)
		}
	}
	return nil
}
func (r *blockedPatternRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// requiredRoleRule checks that certain roles are present in the conversation.
type requiredRoleRule struct {
	roles []llmtrace.Role
}

// RequiredRoles creates a rule that requires at least one message with each of the given roles.
func RequiredRoles(roles ...llmtrace.Role) Rule {
	return &requiredRoleRule{roles: roles}
}

func (r *requiredRoleRule) Name() string    { return "required_roles" }
func (r *requiredRoleRule) WhichSide() Side { return SideInput }
func (r *requiredRoleRule) Level() Severity { return SeverityBlock }
func (r *requiredRoleRule) ValidateInput(req *llmtrace.Request) error {
	present := make(map[llmtrace.Role]bool)
	for _, m := range req.Messages {
		present[m.Role] = true
	}
	for _, role := range r.roles {
		if !present[role] {
			return fmt.Errorf("missing required role: %s", role)
		}
	}
	return nil
}
func (r *requiredRoleRule) ValidateOutput(_ *llmtrace.Request, _ *llmtrace.Response) error {
	return nil
}

// --- Output Rules ---

// minResponseLengthRule validates minimum response length.
type minResponseLengthRule struct {
	minLen int
}

// MinResponseLength creates a rule that warns when the response is shorter than the given length.
func MinResponseLength(minLen int) Rule {
	return &minResponseLengthRule{minLen: minLen}
}

func (r *minResponseLengthRule) Name() string                            { return "min_response_length" }
func (r *minResponseLengthRule) WhichSide() Side                         { return SideOutput }
func (r *minResponseLengthRule) Level() Severity                         { return SeverityWarn }
func (r *minResponseLengthRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *minResponseLengthRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	if len(resp.Content) < r.minLen {
		return fmt.Errorf("response length %d is below minimum %d", len(resp.Content), r.minLen)
	}
	return nil
}

// maxResponseLengthRule validates maximum response length.
type maxResponseLengthRule struct {
	maxLen int
}

// MaxResponseLength creates a rule that blocks responses exceeding the given character count.
func MaxResponseLength(maxLen int) Rule {
	return &maxResponseLengthRule{maxLen: maxLen}
}

func (r *maxResponseLengthRule) Name() string                            { return "max_response_length" }
func (r *maxResponseLengthRule) WhichSide() Side                         { return SideOutput }
func (r *maxResponseLengthRule) Level() Severity                         { return SeverityBlock }
func (r *maxResponseLengthRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *maxResponseLengthRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	if len(resp.Content) > r.maxLen {
		return fmt.Errorf("response length %d exceeds maximum %d", len(resp.Content), r.maxLen)
	}
	return nil
}

// requiredFinishReasonRule checks that the finish reason matches expected values.
type requiredFinishReasonRule struct {
	reasons []string
}

// RequiredFinishReason creates a rule that blocks responses with unexpected finish reasons.
// Common values: "stop", "length", "tool_calls", "content_filter".
func RequiredFinishReason(reasons ...string) Rule {
	return &requiredFinishReasonRule{reasons: reasons}
}

func (r *requiredFinishReasonRule) Name() string                            { return "required_finish_reason" }
func (r *requiredFinishReasonRule) WhichSide() Side                         { return SideOutput }
func (r *requiredFinishReasonRule) Level() Severity                         { return SeverityWarn }
func (r *requiredFinishReasonRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *requiredFinishReasonRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	if resp.FinishReason == "" {
		return nil // no finish reason info available, don't flag
	}
	for _, reason := range r.reasons {
		if resp.FinishReason == reason {
			return nil
		}
	}
	return fmt.Errorf("unexpected finish reason: %q (expected: %v)", resp.FinishReason, r.reasons)
}

// blockedOutputTermsRule checks for forbidden terms in the response.
type blockedOutputTermsRule struct {
	terms    []string
	severity Severity
}

// BlockedOutputTerms creates a rule that blocks responses containing any of the given terms.
func BlockedOutputTerms(terms []string) Rule {
	return &blockedOutputTermsRule{terms: terms, severity: SeverityBlock}
}

// WarnedOutputTerms creates a rule that warns on responses containing any of the given terms.
func WarnedOutputTerms(terms []string) Rule {
	return &blockedOutputTermsRule{terms: terms, severity: SeverityWarn}
}

func (r *blockedOutputTermsRule) Name() string                            { return "blocked_output_terms" }
func (r *blockedOutputTermsRule) WhichSide() Side                         { return SideOutput }
func (r *blockedOutputTermsRule) Level() Severity                         { return r.severity }
func (r *blockedOutputTermsRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *blockedOutputTermsRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	lower := strings.ToLower(resp.Content)
	for _, term := range r.terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return fmt.Errorf("response contains blocked term: %q", term)
		}
	}
	return nil
}

// maxTokenUsageRule checks that the response doesn't exceed a token limit.
type maxTokenUsageRule struct {
	maxTokens int
}

// MaxTokenUsage creates a rule that blocks responses exceeding the given total token count.
func MaxTokenUsage(maxTokens int) Rule {
	return &maxTokenUsageRule{maxTokens: maxTokens}
}

func (r *maxTokenUsageRule) Name() string                            { return "max_token_usage" }
func (r *maxTokenUsageRule) WhichSide() Side                         { return SideOutput }
func (r *maxTokenUsageRule) Level() Severity                         { return SeverityBlock }
func (r *maxTokenUsageRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *maxTokenUsageRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	if resp.Usage.TotalTokens > r.maxTokens {
		return fmt.Errorf("total tokens %d exceeds maximum %d", resp.Usage.TotalTokens, r.maxTokens)
	}
	return nil
}

// outputPatternRule checks the response against a regex pattern.
type outputPatternRule struct {
	name      string
	pattern   *regexp.Regexp
	severity  Severity
	mustMatch bool // if true, response MUST match; if false, must NOT match
}

// OutputMustMatch creates a rule that requires the response to match the given pattern.
func OutputMustMatch(name string, pattern *regexp.Regexp) Rule {
	return &outputPatternRule{name: name, pattern: pattern, severity: SeverityBlock, mustMatch: true}
}

// OutputMustNotMatch creates a rule that blocks responses matching the given pattern.
func OutputMustNotMatch(name string, pattern *regexp.Regexp) Rule {
	return &outputPatternRule{name: name, pattern: pattern, severity: SeverityBlock, mustMatch: false}
}

func (r *outputPatternRule) Name() string                            { return r.name }
func (r *outputPatternRule) WhichSide() Side                         { return SideOutput }
func (r *outputPatternRule) Level() Severity                         { return r.severity }
func (r *outputPatternRule) ValidateInput(_ *llmtrace.Request) error { return nil }
func (r *outputPatternRule) ValidateOutput(_ *llmtrace.Request, resp *llmtrace.Response) error {
	matched := r.pattern.MatchString(resp.Content)
	if r.mustMatch && !matched {
		return fmt.Errorf("response does not match required pattern: %s", r.name)
	}
	if !r.mustMatch && matched {
		return fmt.Errorf("response matches blocked pattern: %s", r.name)
	}
	return nil
}
