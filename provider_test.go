package llmtrace

import (
	"testing"
)

func TestApplyConfig(t *testing.T) {
	cfg := ApplyConfig(
		WithAPIKey("sk-test"),
		WithBaseURL("https://proxy.example.com/v1"),
		WithDefaultModel("gpt-4"),
		WithMaxRetries(3),
		WithExtra("region", "us-east-1"),
	)

	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-test")
	}
	if cfg.BaseURL != "https://proxy.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://proxy.example.com/v1")
	}
	if cfg.DefaultModel != "gpt-4" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "gpt-4")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.Extra["region"] != "us-east-1" {
		t.Errorf("Extra[region] = %v, want us-east-1", cfg.Extra["region"])
	}
}

func TestApplyConfig_Empty(t *testing.T) {
	cfg := ApplyConfig()
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", cfg.BaseURL)
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
	}
}

func TestWithExtra_Initialization(t *testing.T) {
	cfg := ApplyConfig(WithExtra("key", "val"))
	if cfg.Extra == nil {
		t.Fatal("Extra should be initialized")
	}
	if len(cfg.Extra) != 1 {
		t.Errorf("Extra length = %d, want 1", len(cfg.Extra))
	}
}
