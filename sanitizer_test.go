package llmtrace

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestSanitizer_DefaultRules(t *testing.T) {
	s := NewSanitizer()
	rules := s.Rules()
	if len(rules) < 10 {
		t.Errorf("expected at least 10 default rules, got %d", len(rules))
	}

	expectedNames := map[string]bool{
		"api_key":        false,
		"bearer_token":   false,
		"aws_key":        false,
		"aws_secret":     false,
		"private_key":    false,
		"email":          false,
		"credit_card":    false,
		"ssn":            false,
		"phone":          false,
		"url_password":   false,
		"password_field": false,
		"ipv4":           false,
		"jwt":            false,
		"openai_key":     false,
		"scm_token":      false,
		"slack_token":    false,
	}

	for _, r := range rules {
		if _, ok := expectedNames[r.Name]; ok {
			expectedNames[r.Name] = true
		}
	}
	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected rule %q not found in default rules", name)
		}
	}
}

func TestSanitizer_EmptyInput(t *testing.T) {
	s := NewSanitizer()
	if got := s.Sanitize(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSanitizer_CleanInput(t *testing.T) {
	s := NewSanitizer()
	input := "This is a normal message with no sensitive data."
	if got := s.Sanitize(input); got != input {
		t.Errorf("expected unchanged output, got %q", got)
	}
}

func TestSanitizer_APIKey_Generic(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"api_key assignment", "api_key = mySecretKey12345678"},
		{"APIKEY assignment", "APIKEY: abcdefghijklmnop1234"},
		{"secret assignment", "secret: myVeryLongSecretValue123"},
		{"api-key assignment", "api-key=anotherSecretValue99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[API_KEY_REDACTED]") {
				t.Errorf("expected API key redacted, got %q", got)
			}
		})
	}
}

func TestSanitizer_BearerToken(t *testing.T) {
	s := NewSanitizer()
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[BEARER_REDACTED]") {
		t.Errorf("expected bearer token to be redacted, got %q", got)
	}
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Error("bearer token value still present in output")
	}
}

func TestSanitizer_OpenAIKey(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"sk-proj key", "sk-proj-abcdefghijklmnopqrstuvwxyz123456"},
		{"sk-ant key", "sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456"},
		{"sk-live key", "sk-live-abcdefghijklmnopqrstuvwxyz1234567890"},
		{"sk-test key", "sk-test-abcdefghijklmnopqrstuvwxyz1234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[OPENAI_KEY_REDACTED]") {
				t.Errorf("expected OpenAI key redacted, got %q", got)
			}
		})
	}
}

func TestSanitizer_AWSKey(t *testing.T) {
	s := NewSanitizer()
	input := "AWS Access Key: AKIAIOSFODNN7EXAMPLE"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[AWS_KEY_REDACTED]") {
		t.Errorf("expected AWS key redacted, got %q", got)
	}
}

func TestSanitizer_AWSSecret(t *testing.T) {
	s := NewSanitizer()
	input := "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[AWS_SECRET_REDACTED]") {
		t.Errorf("expected AWS secret redacted, got %q", got)
	}
}

func TestSanitizer_PrivateKey(t *testing.T) {
	s := NewSanitizer()
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHL5wZhGhO3x0aGO\n-----END RSA PRIVATE KEY-----"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[PRIVATE_KEY_REDACTED]") {
		t.Errorf("expected private key redacted, got %q", got)
	}
	if strings.Contains(got, "MIIEpAIBAAKCAQEA") {
		t.Error("private key content still present")
	}
}

func TestSanitizer_Email(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple email",
			input: "Contact us at support@example.com for help",
			want:  "Contact us at [EMAIL_REDACTED] for help",
		},
		{
			name:  "email with subdomain",
			input: "user@mail.company.co.uk",
			want:  "[EMAIL_REDACTED]",
		},
		{
			name:  "multiple emails",
			input: "Send to alice@test.com and bob@test.com",
			want:  "Send to [EMAIL_REDACTED] and [EMAIL_REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizer_CreditCard(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"Visa", "4111111111111111"},
		{"Mastercard", "5500000000000004"},
		{"Amex", "378282246310005"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[CARD_REDACTED]") {
				t.Errorf("expected credit card redacted, got %q", got)
			}
		})
	}
}

func TestSanitizer_SSN(t *testing.T) {
	s := NewSanitizer()
	input := "User SSN: 123-45-6789"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[SSN_REDACTED]") {
		t.Errorf("expected SSN redacted, got %q", got)
	}
	if strings.Contains(got, "123-45-6789") {
		t.Error("SSN still present in output")
	}
}

func TestSanitizer_Phone(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"US format", "Call 555-123-4567 now"},
		{"parentheses", "Call (555) 123-4567 now"},
		{"dotted", "Call 555.123.4567 now"},
		{"with country code", "Call +1 555-123-4567 now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[PHONE_REDACTED]") {
				t.Errorf("expected phone redacted, got %q", got)
			}
		})
	}
}

func TestSanitizer_URLPassword(t *testing.T) {
	s := NewSanitizer()
	input := "https://admin:supersecret123@database.example.com:5432/mydb"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[PASSWORD_REDACTED]") {
		t.Errorf("expected URL password redacted, got %q", got)
	}
	if strings.Contains(got, "supersecret123") {
		t.Error("password still present in URL")
	}
	// Should preserve the URL structure
	if !strings.Contains(got, "https://admin:") || !strings.Contains(got, "@database.example.com") {
		t.Errorf("URL structure not preserved: %q", got)
	}
}

func TestSanitizer_PasswordField(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"password=", "password=MySecretPass123"},
		{"passwd:", "passwd: AnotherSecret456"},
		{"PWD=", "PWD=TopSecret789"},
		{"quoted", `password="quotedSecret123"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[PASSWORD_REDACTED]") {
				t.Errorf("expected password redacted in %q, got %q", tt.input, got)
			}
		})
	}
}

func TestSanitizer_JWT(t *testing.T) {
	s := NewSanitizer()
	// Use a standalone JWT (no "Bearer" or "Token:" prefix)
	input := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456ghi"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[JWT_REDACTED]") {
		t.Errorf("expected JWT redacted, got %q", got)
	}
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Error("JWT header still present in output")
	}
}

func TestSanitizer_JWTWithBearerPrefix(t *testing.T) {
	s := NewSanitizer()
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456ghi"
	got := s.Sanitize(input)
	// Bearer pattern should match first
	if !strings.Contains(got, "[BEARER_REDACTED]") {
		t.Errorf("expected bearer redaction, got %q", got)
	}
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Error("JWT still present after bearer redaction")
	}
}

func TestSanitizer_SCMToken(t *testing.T) {
	s := NewSanitizer()
	tests := []struct {
		name  string
		input string
	}{
		{"GitHub PAT", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"},
		{"GitLab PAT", "glpat-abcdefghijklmnopqrst"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Sanitize(tt.input)
			if !strings.Contains(got, "[TOKEN_REDACTED]") {
				t.Errorf("expected SCM token redacted in %q, got %q", tt.input, got)
			}
		})
	}
}

func TestSanitizer_SlackToken(t *testing.T) {
	s := NewSanitizer()
	input := "Slack token: xoxb-1234567890abcdefghijklmnop"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[SLACK_TOKEN_REDACTED]") {
		t.Errorf("expected Slack token redacted, got %q", got)
	}
}

func TestSanitizer_IPAddress(t *testing.T) {
	s := NewSanitizer()
	input := "Server at 192.168.1.100 responded"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[IP_REDACTED]") {
		t.Errorf("expected IP redacted, got %q", got)
	}
}

func TestSanitizer_MultipleSensitiveData(t *testing.T) {
	s := NewSanitizer()
	input := "User john@example.com (SSN: 123-45-6789) called from 555-123-4567"
	got := s.Sanitize(input)

	if strings.Contains(got, "john@example.com") {
		t.Error("email not redacted")
	}
	if strings.Contains(got, "123-45-6789") {
		t.Error("SSN not redacted")
	}
	if strings.Contains(got, "555-123-4567") {
		t.Error("phone not redacted")
	}
}

func TestSanitizer_CustomReplacement(t *testing.T) {
	s := NewSanitizer(WithDefaultReplacement("***"))
	input := "Contact user@example.com"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[EMAIL_REDACTED]") {
		t.Errorf("expected email redaction, got %q", got)
	}
}

func TestSanitizer_CustomRules(t *testing.T) {
	s := NewSanitizer(WithCustomRules(SanitizeRule{
		Name:        "employee_id",
		Pattern:     regexp.MustCompile(`EMP-\d{6}`),
		Replacement: "[EMP_ID_REDACTED]",
	}))

	input := "Employee EMP-123456 accessed the system"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[EMP_ID_REDACTED]") {
		t.Errorf("expected employee ID redacted, got %q", got)
	}
	if strings.Contains(got, "EMP-123456") {
		t.Error("employee ID still present")
	}
	// Default rules should still work
	inputEmail := "user@example.com"
	gotEmail := s.Sanitize(inputEmail)
	if !strings.Contains(gotEmail, "[EMAIL_REDACTED]") {
		t.Error("default email rule should still work with custom rules")
	}
}

func TestSanitizer_OnlyCustomRules(t *testing.T) {
	s := NewSanitizer(WithOnlyCustomRules(SanitizeRule{
		Name:        "custom",
		Pattern:     regexp.MustCompile(`CUSTOM-\d+`),
		Replacement: "[CUSTOM_REDACTED]",
	}))

	// Custom rule should work
	input1 := "CUSTOM-12345"
	got1 := s.Sanitize(input1)
	if !strings.Contains(got1, "[CUSTOM_REDACTED]") {
		t.Errorf("expected custom rule to work, got %q", got1)
	}

	// Default rules should NOT work
	input2 := "user@example.com"
	got2 := s.Sanitize(input2)
	if strings.Contains(got2, "[EMAIL_REDACTED]") {
		t.Error("default email rule should not be active with WithOnlyCustomRules")
	}
}

func TestSanitizer_AddRule(t *testing.T) {
	s := NewSanitizer()
	initialRules := len(s.Rules())

	s.AddRule(SanitizeRule{
		Name:        "custom_id",
		Pattern:     regexp.MustCompile(`ID:\d{8}`),
		Replacement: "[ID_REDACTED]",
	})

	if got := len(s.Rules()); got != initialRules+1 {
		t.Errorf("expected %d rules after add, got %d", initialRules+1, got)
	}

	input := "Record ID:12345678 processed"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[ID_REDACTED]") {
		t.Errorf("expected custom ID redacted, got %q", got)
	}
}

func TestSanitizer_ConcurrentAccess(t *testing.T) {
	s := NewSanitizer()
	input := "User john@example.com with API key AKIAIOSFODNN7EXAMPLE"

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := s.Sanitize(input)
			if strings.Contains(got, "john@example.com") {
				t.Error("email not redacted in concurrent access")
			}
		}()
	}
	wg.Wait()

	// Also test concurrent AddRule
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AddRule(SanitizeRule{
				Name:        "concurrent_rule",
				Pattern:     regexp.MustCompile(`CONC-\d+`),
				Replacement: "[CONC_REDACTED]",
			})
		}()
	}
	wg.Wait()
}

func TestSanitizer_SanitizeMap(t *testing.T) {
	s := NewSanitizer()
	input := map[string]any{
		"provider":    "openai",
		"model":       "gpt-4o",
		"api_key":     "***",
		"email":       "user@example.com",
		"temperature": 0.7,
		"max_tokens":  100,
	}

	got := s.SanitizeMap(input)

	// Non-string values should pass through
	if got["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", got["temperature"])
	}
	if got["max_tokens"] != 100 {
		t.Errorf("expected max_tokens 100, got %v", got["max_tokens"])
	}

	// String values should be sanitized
	if got["provider"] != "openai" {
		t.Errorf("expected provider 'openai', got %v", got["provider"])
	}
	if got["model"] != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %v", got["model"])
	}
	if str, ok := got["api_key"].(string); ok {
		if strings.Contains(str, "abcdefghijklmnop1234") {
			t.Error("api_key not sanitized in map")
		}
	}
	if str, ok := got["email"].(string); ok {
		if strings.Contains(str, "user@example.com") {
			t.Error("email not sanitized in map")
		}
	}
}

func TestSanitizer_SanitizeMap_NilInput(t *testing.T) {
	s := NewSanitizer()
	got := s.SanitizeMap(nil)
	if got == nil {
		t.Fatal("expected non-nil map from nil input")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestMaskString(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		visibleChars int
		want         string
	}{
		{"long string", "abcdefghijklmnop", 3, "abc********nop"},
		{"short string", "abc", 3, "abc"},
		{"exact boundary", "abcdef", 3, "abcdef"},
		{"single char", "a", 1, "a"},
		{"empty", "", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskString(tt.input, tt.visibleChars)
			if got != tt.want {
				t.Errorf("MaskString(%q, %d) = %q, want %q", tt.input, tt.visibleChars, got, tt.want)
			}
		})
	}
}

func TestSanitizer_WithCustomRuleReplacement(t *testing.T) {
	s := NewSanitizer(WithCustomRules(SanitizeRule{
		Name:        "internal_id",
		Pattern:     regexp.MustCompile(`INT-[A-Z0-9]{10}`),
		Replacement: "", // empty, should use default replacement
	}))

	input := "ID: INT-ABC1234567"
	got := s.Sanitize(input)
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected default replacement for empty Replacement, got %q", got)
	}
}

func TestSanitizer_NonOverlappingMatches(t *testing.T) {
	s := NewSanitizer()
	input := "Email user@host.com and another@host.com"
	got := s.Sanitize(input)
	if strings.Contains(got, "user@host.com") || strings.Contains(got, "another@host.com") {
		t.Errorf("not all emails redacted: %q", got)
	}
	count := strings.Count(got, "[EMAIL_REDACTED]")
	if count != 2 {
		t.Errorf("expected 2 email redactions, got %d in %q", count, got)
	}
}

func TestSanitizer_RealWorldScenario(t *testing.T) {
	s := NewSanitizer()
	input := "api_key: mySecretApiKey123456789012, user: john.doe@company.com, password: MySecretPassword123, url: https://admin:dbpass123@db.internal.com:5432/prod"

	got := s.Sanitize(input)

	if strings.Contains(got, "mySecretApiKey123456789012") {
		t.Error("api_key not sanitized")
	}
	if strings.Contains(got, "john.doe@company.com") {
		t.Error("email not sanitized")
	}
	if strings.Contains(got, "MySecretPassword123") {
		t.Error("password not sanitized")
	}
	if strings.Contains(got, "dbpass123") {
		t.Error("URL password not sanitized")
	}

	if !strings.Contains(got, "[EMAIL_REDACTED]") {
		t.Error("expected email redaction")
	}
	if !strings.Contains(got, "[PASSWORD_REDACTED]") {
		t.Error("expected password redaction")
	}
	if !strings.Contains(got, "[API_KEY_REDACTED]") {
		t.Error("expected API key redaction")
	}
}

func TestSanitizer_PriorityOrdering(t *testing.T) {
	s := NewSanitizer()

	// URL password should be redacted before email can consume it
	input := "https://user:***@ssw0rd@host.com/path"
	got := s.Sanitize(input)
	if strings.Contains(got, "p@ssw0rd") {
		t.Errorf("URL password not redacted: %q", got)
	}

	// JWT should be redacted as JWT, not partially by api_key
	inputJWT := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abc123"
	gotJWT := s.Sanitize(inputJWT)
	if !strings.Contains(gotJWT, "[JWT_REDACTED]") {
		t.Errorf("JWT not properly redacted: %q", gotJWT)
	}
}

func BenchmarkSanitizer_Sanitize(b *testing.B) {
	s := NewSanitizer()
	input := "User john@example.com accessed API with AKIAIOSFODNN7EXAMPLE from 192.168.1.100"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sanitize(input)
	}
}

func BenchmarkSanitizer_SanitizeNoMatch(b *testing.B) {
	s := NewSanitizer()
	input := "This is a normal log message with no sensitive data at all"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sanitize(input)
	}
}

func BenchmarkSanitizer_SanitizeMap(b *testing.B) {
	s := NewSanitizer()
	input := map[string]any{
		"provider":    "openai",
		"model":       "gpt-4o",
		"api_key":     "***",
		"email":       "user@example.com",
		"temperature": 0.7,
		"max_tokens":  100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SanitizeMap(input)
	}
}
