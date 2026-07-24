package moderation

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEngine_Check_WordBlocklist(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("profanity", []string{"badword", "evil"}, ActionBlock, SeverityHigh, false))

	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantN   int
	}{
		{"clean text", "hello world", true, 0},
		{"contains blocked word", "this is a badword test", false, 1},
		{"case insensitive", "This is a BADWORD test", false, 1},
		{"multiple matches", "badword and evil together", false, 2},
		{"partial match no hit", "badwords are not bad", false, 1}, // "badword" is substring of "badwords"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.Check(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Allowed != tt.wantOK {
				t.Errorf("Allowed = %v, want %v", result.Allowed, tt.wantOK)
			}
			if len(result.Matches) != tt.wantN {
				t.Errorf("Matches = %d, want %d (matches: %v)", len(result.Matches), tt.wantN, result.Matches)
			}
		})
	}
}

func TestEngine_Check_CaseSensitive(t *testing.T) {
	engine := New(Config{
		DefaultAction:     ActionBlock,
		RedactPlaceholder: "[REDACTED]",
		CaseSensitive:     true,
	})
	engine.AddRule(NewWordBlocklist("test", []string{"BAD"}, ActionBlock, SeverityMedium, true))

	result, _ := engine.Check(context.Background(), "bad is not BAD")
	if result.Allowed {
		t.Error("expected BAD to be blocked")
	}
	// Only exact case "BAD" should match, not "bad"
	if len(result.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(result.Matches))
	}
}

func TestEngine_Check_RegexRule(t *testing.T) {
	engine := New(DefaultConfig())
	re := regexp.MustCompile(`\b\d{3}-\d{4}\b`)
	engine.AddRule(NewRegexRule("phone_pattern", "Matches XXX-XXXX", re, ActionLog, SeverityLow))

	result, _ := engine.Check(context.Background(), "call 555-1234 now")
	if !result.Allowed {
		t.Error("log action should still allow content")
	}
	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].Matched != "555-1234" {
		t.Errorf("matched %q, want %q", result.Matches[0].Matched, "555-1234")
	}
}

func TestEngine_Check_Redact(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("secret", []string{"password123"}, ActionRedact, SeverityHigh, false))

	result, _ := engine.Check(context.Background(), "my password is password123 ok")
	if !result.Allowed {
		t.Error("redact action should still allow content")
	}
	if !strings.Contains(result.Filtered, "[REDACTED]") {
		t.Errorf("expected redaction in filtered text, got %q", result.Filtered)
	}
	if strings.Contains(result.Filtered, "password123") {
		t.Error("filtered text should not contain original word")
	}
}

func TestEngine_Check_RedactCustomPlaceholder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RedactPlaceholder = "***"
	engine := New(cfg)
	engine.AddRule(NewWordBlocklist("secret", []string{"secret"}, ActionRedact, SeverityHigh, false))

	result, _ := engine.Check(context.Background(), "the secret word")
	if !strings.Contains(result.Filtered, "***") {
		t.Errorf("expected custom placeholder, got %q", result.Filtered)
	}
}

func TestEngine_CheckOutput(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("harmful", []string{"dangerous"}, ActionBlock, SeverityCritical, false))

	// Blocked output
	result, _ := engine.CheckOutput(context.Background(), "this is dangerous content")
	if result.Allowed {
		t.Error("expected output to be blocked")
	}

	// Clean output
	result, _ = engine.CheckOutput(context.Background(), "safe output")
	if !result.Allowed {
		t.Error("expected clean output to be allowed")
	}
}

func TestEngine_Check_ContextCanceled(t *testing.T) {
	engine := New(DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := engine.Check(ctx, "test")
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestEngine_Check_MaxInputLength(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInputLength = 10
	engine := New(cfg)

	result, _ := engine.Check(context.Background(), "short")
	if !result.Allowed {
		t.Error("short text should be allowed")
	}

	result, _ = engine.Check(context.Background(), "this is a very long text that exceeds the limit")
	if result.Allowed {
		t.Error("long text should be blocked")
	}
}

func TestEngine_CheckOutput_MaxOutputLength(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxOutputLength = 20
	engine := New(cfg)

	result, _ := engine.CheckOutput(context.Background(), "short")
	if !result.Allowed {
		t.Error("short output should be allowed")
	}

	result, _ = engine.CheckOutput(context.Background(), "this is a very long output that exceeds the configured limit")
	if result.Allowed {
		t.Error("long output should be blocked")
	}
}

func TestEngine_Check_MultipleRules(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("profanity", []string{"damn"}, ActionBlock, SeverityMedium, false))
	engine.AddRule(NewWordBlocklist("threats", []string{"kill"}, ActionBlock, SeverityCritical, false))

	result, _ := engine.Check(context.Background(), "I will kill you damn it")
	if result.Allowed {
		t.Error("should be blocked")
	}
	if len(result.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(result.Matches))
	}
}

func TestEngine_Check_NoRules(t *testing.T) {
	engine := New(DefaultConfig())
	result, _ := engine.Check(context.Background(), "anything goes")
	if !result.Allowed {
		t.Error("should allow when no rules")
	}
	if len(result.Matches) != 0 {
		t.Error("should have no matches")
	}
}

func TestEngine_Rules(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("r1", []string{"a"}, ActionBlock, SeverityLow, false))
	engine.AddRule(NewWordBlocklist("r2", []string{"b"}, ActionLog, SeverityHigh, false))

	rules := engine.Rules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Name != "r1" || rules[1].Name != "r2" {
		t.Error("rules not in expected order")
	}
}

func TestEngine_Check_ResultTimestamp(t *testing.T) {
	engine := New(DefaultConfig())
	before := time.Now()
	result, _ := engine.Check(context.Background(), "test")
	after := time.Now()

	if result.CheckedAt.Before(before) || result.CheckedAt.After(after) {
		t.Error("CheckedAt should be between before and after")
	}
	if result.Duration < 0 {
		t.Error("Duration should be non-negative")
	}
}

func TestNewPIIDetector(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewPIIDetector(ActionRedact, SeverityHigh))

	tests := []struct {
		name     string
		input    string
		wantN    int
		wantRedact bool
	}{
		{"email", "contact me at user@example.com", 1, true},
		{"phone", "call 555-123-4567", 1, true},
		{"no PII", "hello world", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := engine.Check(context.Background(), tt.input)
			if len(result.Matches) != tt.wantN {
				t.Errorf("matches = %d, want %d", len(result.Matches), tt.wantN)
			}
			if tt.wantRedact && result.Filtered == result.Original {
				t.Error("expected redaction to change filtered text")
			}
		})
	}
}

func TestNewMaxLengthRule(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewMaxLengthRule(20, ActionBlock))

	result, _ := engine.Check(context.Background(), "short")
	if !result.Allowed {
		t.Error("short text should be allowed")
	}

	result, _ = engine.Check(context.Background(), "this text is definitely longer than twenty bytes")
	if result.Allowed {
		t.Error("long text should be blocked by max length rule")
	}
}

func TestAction_String(t *testing.T) {
	if ActionBlock.String() != "block" {
		t.Errorf("ActionBlock.String() = %q", ActionBlock.String())
	}
	if ActionRedact.String() != "redact" {
		t.Errorf("ActionRedact.String() = %q", ActionRedact.String())
	}
	if ActionLog.String() != "log" {
		t.Errorf("ActionLog.String() = %q", ActionLog.String())
	}
	if Action(99).String() != "unknown" {
		t.Error("unknown action should return 'unknown'")
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityLow, "low"},
		{SeverityMedium, "medium"},
		{SeverityHigh, "high"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if tt.s.String() != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.s, tt.s.String(), tt.want)
		}
	}
}

func TestBlockedError(t *testing.T) {
	err := &blockedError{reason: "test"}
	if !IsBlocked(err) {
		t.Error("IsBlocked should return true for blockedError")
	}
	if err.Error() != "content blocked by moderation: test" {
		t.Errorf("Error() = %q", err.Error())
	}
	if IsBlocked(nil) {
		t.Error("IsBlocked should return false for nil")
	}
}

func TestWithContext(t *testing.T) {
	r := &Result{Allowed: true}
	ctx := WithResult(context.Background(), r)

	got, ok := ResultFromContext(ctx)
	if !ok || got != r {
		t.Error("expected to retrieve result from context")
	}

	_, ok = ResultFromContext(context.Background())
	if ok {
		t.Error("should not find result in empty context")
	}
}

func TestEngine_Check_RedactMultipleOverlapping(t *testing.T) {
	engine := New(DefaultConfig())
	engine.AddRule(NewWordBlocklist("test", []string{"ab", "bc"}, ActionRedact, SeverityMedium, false))

	result, _ := engine.Check(context.Background(), "abc")
	if !strings.Contains(result.Filtered, "[REDACTED]") {
		t.Errorf("expected redaction, got %q", result.Filtered)
	}
}
