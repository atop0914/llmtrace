package llmtrace

import (
	"regexp"
	"strings"
	"sync"
)

// SanitizeRule defines a pattern-based rule for detecting and redacting sensitive data.
type SanitizeRule struct {
	// Name is a human-readable identifier for this rule.
	Name string

	// Pattern is the compiled regex to match sensitive data.
	Pattern *regexp.Regexp

	// Replacement is the string to substitute. If empty, defaults to "[REDACTED]".
	Replacement string
}

// Sanitizer performs pattern-based detection and redaction of sensitive data in strings.
// It is safe for concurrent use.
type Sanitizer struct {
	mu    sync.RWMutex
	rules []SanitizeRule

	// defaultReplacement is used when a rule has no custom Replacement.
	defaultReplacement string
}

// SanitizerOption configures a Sanitizer.
type SanitizerOption func(*Sanitizer)

// WithDefaultReplacement sets the default redaction replacement text.
func WithDefaultReplacement(replacement string) SanitizerOption {
	return func(s *Sanitizer) {
		s.defaultReplacement = replacement
	}
}

// WithCustomRules adds user-defined sanitization rules on top of the defaults.
func WithCustomRules(rules ...SanitizeRule) SanitizerOption {
	return func(s *Sanitizer) {
		s.rules = append(s.rules, rules...)
	}
}

// WithOnlyCustomRules replaces all default rules with user-defined ones.
func WithOnlyCustomRules(rules ...SanitizeRule) SanitizerOption {
	return func(s *Sanitizer) {
		s.rules = rules
	}
}

// NewSanitizer creates a Sanitizer with built-in sensitive data patterns.
// Built-in patterns detect: bearer tokens, JWTs, OpenAI/GitHub/Slack tokens,
// AWS keys, private keys, generic API keys, emails, credit cards,
// SSNs, phone numbers, passwords in URLs and config, and IP addresses.
//
// Rules are ordered from most specific to least specific to prevent
// greedy generic patterns from consuming specific token formats.
func NewSanitizer(opts ...SanitizerOption) *Sanitizer {
	s := &Sanitizer{
		defaultReplacement: "[REDACTED]",
		rules:              defaultRules(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Sanitize redacts all sensitive data matches in the input string.
func (s *Sanitizer) Sanitize(input string) string {
	if input == "" {
		return input
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := input
	for _, rule := range s.rules {
		replacement := rule.Replacement
		if replacement == "" {
			replacement = s.defaultReplacement
		}
		result = rule.Pattern.ReplaceAllString(result, replacement)
	}
	return result
}

// AddRule adds a sanitization rule dynamically. Thread-safe.
func (s *Sanitizer) AddRule(rule SanitizeRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, rule)
}

// Rules returns a copy of the current rules. Thread-safe.
func (s *Sanitizer) Rules() []SanitizeRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rules := make([]SanitizeRule, len(s.rules))
	copy(rules, s.rules)
	return rules
}

// SanitizeMap applies sanitization to all string values in a map.
func (s *Sanitizer) SanitizeMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			result[k] = s.Sanitize(str)
		} else {
			result[k] = v
		}
	}
	return result
}

// defaultRules returns the built-in sensitive data detection patterns.
// Order matters: URL and structural patterns come first, then specific
// provider tokens, then generic patterns to prevent false positives.
func defaultRules() []SanitizeRule {
	return []SanitizeRule{
		// === Structural / URL patterns (must come before generic patterns) ===

		// Passwords in URLs (http://user:password@host)
		// Must come BEFORE email and password_field to prevent them from
		// consuming the password@host portion.
		{
			Name:        "url_password",
			Pattern:     regexp.MustCompile(`(https?://[^:]+:)[^@]+(@)`),
			Replacement: "${1}[PASSWORD_REDACTED]${2}",
		},
		// Private keys (PEM format) — must come early to avoid partial matches
		{
			Name:        "private_key",
			Pattern:     regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE KEY-----[\s\S]+?-----END\s+(?:RSA\s+)?PRIVATE KEY-----`),
			Replacement: "[PRIVATE_KEY_REDACTED]",
		},

		// === Bearer and JWT tokens ===

		// Bearer tokens in headers (must come before generic api_key)
		{
			Name:        "bearer_token",
			Pattern:     regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9_\-\.]{20,}`),
			Replacement: "[BEARER_REDACTED]",
		},
		// JWT tokens (eyJ... format with header.payload.signature)
		// Must come before api_key to prevent "Token: eyJ..." from being
		// partially consumed by the generic api_key pattern.
		{
			Name:        "jwt",
			Pattern:     regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_\-]+`),
			Replacement: "[JWT_REDACTED]",
		},

		// === Provider-specific tokens (most specific first) ===

		// OpenAI-style keys (sk-proj-..., sk-ant-..., sk-live-..., sk-test-...)
		{
			Name:        "openai_key",
			Pattern:     regexp.MustCompile(`sk-(?:proj|ant|live|test)-[a-zA-Z0-9_\-]{20,}`),
			Replacement: "[OPENAI_KEY_REDACTED]",
		},
		// GitHub PATs (ghp_) and GitLab PATs (glpat-)
		{
			Name:        "scm_token",
			Pattern:     regexp.MustCompile(`(?:ghp_[a-zA-Z0-9]{36,}|glpat-[a-zA-Z0-9\-]{20,})`),
			Replacement: "[TOKEN_REDACTED]",
		},
		// Slack tokens (xoxb-, xoxp-, xoxa-)
		{
			Name:        "slack_token",
			Pattern:     regexp.MustCompile(`xox[bpars]-[a-zA-Z0-9\-]+`),
			Replacement: "[SLACK_TOKEN_REDACTED]",
		},

		// === Cloud provider credentials ===

		// AWS Access Key IDs
		{
			Name:        "aws_key",
			Pattern:     regexp.MustCompile(`(AKIA[0-9A-Z]{16})`),
			Replacement: "[AWS_KEY_REDACTED]",
		},
		// AWS Secret Access Keys
		{
			Name:        "aws_secret",
			Pattern:     regexp.MustCompile(`(?i)(?:aws_secret_access_key|aws_secret)[\s:=]+[A-Za-z0-9/+=]{40}`),
			Replacement: "[AWS_SECRET_REDACTED]",
		},

		// === Generic keys and tokens (must come after specific patterns) ===

		// Generic API keys — matches api_key=, secret=, apikey:, etc.
		// Note: "token" is excluded to prevent false positives with JWT "Token:" headers.
		{
			Name:        "api_key",
			Pattern:     regexp.MustCompile(`(?i)(?:api[_-]?key|secret|apikey)[\s:=]+['\"]?([a-zA-Z0-9_\-]{16,})`),
			Replacement: "[API_KEY_REDACTED]",
		},

		// === PII ===

		// Email addresses
		{
			Name:        "email",
			Pattern:     regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			Replacement: "[EMAIL_REDACTED]",
		},
		// Credit card numbers (Visa, Mastercard, Amex, Discover, Diners, JCB)
		{
			Name:        "credit_card",
			Pattern:     regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12}|3(?:0[0-5]|[68][0-9])[0-9]{11}|(?:2131|1800|35\d{3})\d{11})\b`),
			Replacement: "[CARD_REDACTED]",
		},
		// US Social Security Numbers
		{
			Name:        "ssn",
			Pattern:     regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			Replacement: "[SSN_REDACTED]",
		},
		// Phone numbers (US format)
		{
			Name:        "phone",
			Pattern:     regexp.MustCompile(`(?:\+?1[\s.-]?)?\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}`),
			Replacement: "[PHONE_REDACTED]",
		},

		// === Credentials in configuration ===

		// Password assignments in config/env (password=, passwd=, pwd=)
		{
			Name:        "password_field",
			Pattern:     regexp.MustCompile(`(?i)(?:password|passwd|pwd)[\s:=]+['\"]?([^\s'\"]{4,})`),
			Replacement: "[PASSWORD_REDACTED]",
		},

		// === Network ===

		// IPv4 addresses (optional — can be disabled if too aggressive)
		{
			Name:        "ipv4",
			Pattern:     regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`),
			Replacement: "[IP_REDACTED]",
		},
	}
}

// MaskString masks a string showing only the first and last few characters.
// Example: MaskString("abcdefghijklmnop", 3) => "abc********nop"
func MaskString(s string, visibleChars int) string {
	runes := []rune(s)
	if len(runes) <= visibleChars*2 {
		return s
	}
	return string(runes[:visibleChars]) + strings.Repeat("*", 8) + string(runes[len(runes)-visibleChars:])
}
