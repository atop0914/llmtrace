package configwatch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, dir string, cfg *Config) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func boolPtr(b bool) *bool { return &b }

func TestNew_EmptyPath(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNew_FileNotFound(t *testing.T) {
	_, err := New("/nonexistent/config.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNew_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{invalid json}"), 0o644)

	_, err := New(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNew_LoadsInitialConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		LogLevel: "debug",
		Providers: []ProviderConfig{
			{Name: "openai", DefaultModel: "gpt-4"},
		},
	}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	got := w.Config()
	if got.LogLevel != "debug" {
		t.Errorf("log_level = %q, want %q", got.LogLevel, "debug")
	}
	if len(got.Providers) != 1 || got.Providers[0].Name != "openai" {
		t.Errorf("providers = %v, want openai", got.Providers)
	}
}

func TestConfig_AtomicAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Concurrent reads should be safe
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := w.Config()
			if c == nil {
				t.Error("got nil config")
			}
		}()
	}
	wg.Wait()
}

func TestWatcher_ReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var changeCount atomic.Int32
	w, err := New(path,
		WithPollInterval(50*time.Millisecond),
		WithListener(ListenerFunc(func(old, new *Config) {
			changeCount.Add(1)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Update config
	cfg2 := &Config{LogLevel: "debug"}
	data, _ := json.Marshal(cfg2)
	os.WriteFile(path, data, 0o644)

	// Wait for reload
	time.Sleep(200 * time.Millisecond)

	if w.Config().LogLevel != "debug" {
		t.Errorf("log_level = %q, want %q", w.Config().LogLevel, "debug")
	}
	if n := changeCount.Load(); n != 1 {
		t.Errorf("change count = %d, want 1", n)
	}
}

func TestWatcher_NoReloadOnSameContent(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var changeCount atomic.Int32
	w, err := New(path,
		WithPollInterval(50*time.Millisecond),
		WithListener(ListenerFunc(func(old, new *Config) {
			changeCount.Add(1)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write same content (touch file)
	data, _ := json.Marshal(cfg)
	os.WriteFile(path, data, 0o644)

	time.Sleep(200 * time.Millisecond)

	if n := changeCount.Load(); n != 0 {
		t.Errorf("change count = %d, want 0 (same content)", n)
	}
}

func TestWatcher_ValidationRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var errCount atomic.Int32
	validate := func(c *Config) error {
		if c.LogLevel == "invalid" {
			return errors.New("bad log level")
		}
		return nil
	}

	w, err := New(path,
		WithPollInterval(50*time.Millisecond),
		WithValidate(validate),
		WithOnError(func(e error) {
			errCount.Add(1)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write invalid config
	cfg2 := &Config{LogLevel: "invalid"}
	data, _ := json.Marshal(cfg2)
	os.WriteFile(path, data, 0o644)

	time.Sleep(200 * time.Millisecond)

	// Old config should still be in effect
	if w.Config().LogLevel != "info" {
		t.Errorf("log_level = %q, want %q (should keep old)", w.Config().LogLevel, "info")
	}
	if n := errCount.Load(); n < 1 {
		t.Errorf("error count = %d, want >= 1", n)
	}
}

func TestWatcher_ValidationRejectsInitial(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "bad"}
	path := writeTempConfig(t, dir, cfg)

	validate := func(c *Config) error {
		if c.LogLevel == "bad" {
			return errors.New("bad log level")
		}
		return nil
	}

	_, err := New(path, WithValidate(validate))
	if err == nil {
		t.Fatal("expected error for invalid initial config")
	}
}

func TestWatcher_ForceReload(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var changeCount atomic.Int32
	w, err := New(path,
		WithListener(ListenerFunc(func(old, new *Config) {
			changeCount.Add(1)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Update file
	cfg2 := &Config{LogLevel: "debug"}
	data, _ := json.Marshal(cfg2)
	os.WriteFile(path, data, 0o644)

	// Force reload
	newCfg, err := w.ForceReload()
	if err != nil {
		t.Fatal(err)
	}
	if newCfg.LogLevel != "debug" {
		t.Errorf("log_level = %q, want %q", newCfg.LogLevel, "debug")
	}
	if n := changeCount.Load(); n != 1 {
		t.Errorf("change count = %d, want 1", n)
	}
}

func TestWatcher_ForceReloadOnClosed(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	_, err = w.ForceReload()
	if err == nil {
		t.Fatal("expected error on closed watcher")
	}
}

func TestWatcher_AddListenerAfterConstruction(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path, WithPollInterval(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	var called atomic.Bool
	w.AddListener(ListenerFunc(func(old, new *Config) {
		called.Store(true)
	}))

	// Update config
	cfg2 := &Config{LogLevel: "debug"}
	data, _ := json.Marshal(cfg2)
	os.WriteFile(path, data, 0o644)

	time.Sleep(200 * time.Millisecond)

	if !called.Load() {
		t.Error("added listener was not called")
	}
}

func TestWatcher_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestListenerFunc(t *testing.T) {
	var got string
	f := ListenerFunc(func(old, new *Config) {
		got = new.LogLevel
	})
	f.OnConfigChange(&Config{LogLevel: "old"}, &Config{LogLevel: "new"})
	if got != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LogLevel != "info" {
		t.Errorf("log_level = %q, want %q", cfg.LogLevel, "info")
	}
	if !Enabled(cfg.MetricsEnabled) {
		t.Error("metrics should be enabled by default")
	}
	if cfg.RateLimit == nil || cfg.RateLimit.RequestsPerSecond != 100 {
		t.Error("rate limit should default to 100 rps")
	}
	if cfg.CircuitBreaker == nil || cfg.CircuitBreaker.FailureThreshold != 5 {
		t.Error("circuit breaker should default to 5 failures")
	}
	if cfg.Cache == nil || cfg.Cache.MaxEntries != 1000 {
		t.Error("cache should default to 1000 entries")
	}
	if cfg.Budget == nil || cfg.Budget.MaxCostUSD != 100 {
		t.Error("budget should default to $100")
	}
}

func TestEnabled(t *testing.T) {
	if !Enabled(nil) {
		t.Error("nil should be treated as enabled")
	}
	if !Enabled(boolPtr(true)) {
		t.Error("true should be enabled")
	}
	if Enabled(boolPtr(false)) {
		t.Error("false should be disabled")
	}
}

func TestMergeConfig(t *testing.T) {
	base := &Config{
		LogLevel: "info",
		RateLimit: &RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             200,
		},
	}

	// Merge with overlay
	overlay := &Config{
		LogLevel: "debug",
	}
	merged := MergeConfig(base, overlay)
	if merged.LogLevel != "debug" {
		t.Errorf("log_level = %q, want %q", merged.LogLevel, "debug")
	}
	if merged.RateLimit.RequestsPerSecond != 100 {
		t.Error("rate limit should be preserved from base")
	}

	// Merge with nil overlay
	merged2 := MergeConfig(base, nil)
	if merged2.LogLevel != "info" {
		t.Error("nil overlay should return base")
	}

	// Merge with nil base
	merged3 := MergeConfig(nil, overlay)
	if merged3.LogLevel != "debug" {
		t.Error("nil base should return overlay")
	}

	// Merge both nil
	merged4 := MergeConfig(nil, nil)
	if merged4 != nil {
		t.Error("both nil should return nil")
	}
}

func TestMergeConfig_AllFields(t *testing.T) {
	base := &Config{
		LogLevel:       "info",
		MetricsEnabled: boolPtr(true),
		Providers:      []ProviderConfig{{Name: "openai"}},
		RateLimit:      &RateLimitConfig{RequestsPerSecond: 50},
		CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 3},
		Cache:          &CacheConfig{MaxEntries: 500},
		Budget:         &BudgetConfig{MaxCostUSD: 50},
	}

	overlay := &Config{
		LogLevel:         "debug",
		MetricsEnabled:   boolPtr(false),
		DashboardEnabled: boolPtr(false),
		Providers:        []ProviderConfig{{Name: "anthropic"}},
		RateLimit:        &RateLimitConfig{RequestsPerSecond: 200},
		CircuitBreaker:   &CircuitBreakerConfig{FailureThreshold: 10},
		Cache:            &CacheConfig{MaxEntries: 2000},
		Budget:           &BudgetConfig{MaxCostUSD: 500},
	}

	merged := MergeConfig(base, overlay)

	if merged.LogLevel != "debug" {
		t.Error("log level should be overridden")
	}
	if Enabled(merged.MetricsEnabled) {
		t.Error("metrics should be disabled")
	}
	if Enabled(merged.DashboardEnabled) {
		t.Error("dashboard should be disabled by overlay")
	}
	if len(merged.Providers) != 1 || merged.Providers[0].Name != "anthropic" {
		t.Error("providers should be overridden")
	}
	if merged.RateLimit.RequestsPerSecond != 200 {
		t.Error("rate limit should be overridden")
	}
	if merged.CircuitBreaker.FailureThreshold != 10 {
		t.Error("circuit breaker should be overridden")
	}
	if merged.Cache.MaxEntries != 2000 {
		t.Error("cache should be overridden")
	}
	if merged.Budget.MaxCostUSD != 500 {
		t.Error("budget should be overridden")
	}
}

func TestWatcher_FileDeletedKeepsOldConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var errCount atomic.Int32
	w, err := New(path,
		WithPollInterval(50*time.Millisecond),
		WithOnError(func(e error) {
			errCount.Add(1)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Delete config file
	os.Remove(path)

	time.Sleep(200 * time.Millisecond)

	// Old config should still be in effect
	if w.Config().LogLevel != "info" {
		t.Errorf("log_level = %q, want %q (should keep old)", w.Config().LogLevel, "info")
	}
	if n := errCount.Load(); n < 1 {
		t.Error("expected at least one error callback")
	}
}

func TestWatcher_ProviderConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Providers: []ProviderConfig{
			{Name: "openai", APIKey: "sk-test", DefaultModel: "gpt-4", Priority: 1},
			{Name: "anthropic", BaseURL: "https://api.anthropic.com", Priority: 2},
		},
	}
	path := writeTempConfig(t, dir, cfg)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	got := w.Config()
	if len(got.Providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(got.Providers))
	}
	if got.Providers[0].Name != "openai" || got.Providers[0].APIKey != "sk-test" {
		t.Errorf("provider 0 = %v", got.Providers[0])
	}
	if got.Providers[1].Name != "anthropic" || got.Providers[1].BaseURL != "https://api.anthropic.com" {
		t.Errorf("provider 1 = %v", got.Providers[1])
	}
}

func TestWatcher_MultipleReloads(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{LogLevel: "info"}
	path := writeTempConfig(t, dir, cfg)

	var lastLevel atomic.Value
	lastLevel.Store("info")

	w, err := New(path,
		WithPollInterval(50*time.Millisecond),
		WithListener(ListenerFunc(func(old, new *Config) {
			lastLevel.Store(new.LogLevel)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	levels := []string{"debug", "warn", "error", "info"}
	for _, level := range levels {
		cfg := &Config{LogLevel: level}
		data, _ := json.Marshal(cfg)
		os.WriteFile(path, data, 0o644)
		time.Sleep(150 * time.Millisecond)
	}

	// Wait for final reload
	time.Sleep(100 * time.Millisecond)

	if got := lastLevel.Load().(string); got != "info" {
		t.Errorf("last level = %q, want %q", got, "info")
	}
}

func TestWatcher_RawFields(t *testing.T) {
	dir := t.TempDir()
	// Write config with extra fields
	raw := `{"log_level": "info", "custom_field": "value", "nested": {"key": 42}}`
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(raw), 0o644)

	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	cfg := w.Config()
	if cfg.LogLevel != "info" {
		t.Errorf("log_level = %q", cfg.LogLevel)
	}
	if cfg.Raw == nil {
		t.Fatal("Raw should not be nil")
	}
	if _, ok := cfg.Raw["custom_field"]; !ok {
		t.Error("custom_field should be in Raw")
	}
	if _, ok := cfg.Raw["nested"]; !ok {
		t.Error("nested should be in Raw")
	}
}
