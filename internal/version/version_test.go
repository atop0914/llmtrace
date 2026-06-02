package version

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	if info.GitCommit != GitCommit {
		t.Errorf("GitCommit = %q, want %q", info.GitCommit, GitCommit)
	}
	if info.BuildDate != BuildDate {
		t.Errorf("BuildDate = %q, want %q", info.BuildDate, BuildDate)
	}
}

func TestInfo_String(t *testing.T) {
	info := Info{
		Version:   "v1.0.0",
		GitCommit: "abc1234",
		BuildDate: "2026-05-22",
	}
	s := info.String()

	if !strings.Contains(s, "llmtrace") {
		t.Errorf("String() should contain 'llmtrace', got %q", s)
	}
	if !strings.Contains(s, "v1.0.0") {
		t.Errorf("String() should contain version, got %q", s)
	}
	if !strings.Contains(s, "abc1234") {
		t.Errorf("String() should contain commit, got %q", s)
	}
	if !strings.Contains(s, "2026-05-22") {
		t.Errorf("String() should contain build date, got %q", s)
	}
}

func TestDefaults(t *testing.T) {
	// Default values should be set
	if Version == "" {
		t.Error("Version should not be empty")
	}
	if GitCommit == "" {
		t.Error("GitCommit should not be empty")
	}
	if BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}
}
