// Package configwatch provides hot-reloadable configuration for LLMTrace.
//
// It watches a JSON config file for changes, performs atomic config swaps,
// and notifies registered listeners. File change detection uses SHA-256
// hashing to avoid spurious reloads from mtime-only changes.
//
// Usage:
//
//	watcher, err := configwatch.New("/etc/llmtrace/config.json",
//	    configwatch.WithPollInterval(5*time.Second),
//	    configwatch.WithListener(configwatch.ListenerFunc(func(old, new *configwatch.Config) {
//	        log.Printf("config reloaded: providers=%d", len(new.Providers))
//	    })),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer watcher.Close()
//
//	cfg := watcher.Config() // always returns the latest valid config
package configwatch

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Config represents the LLMTrace runtime configuration.
type Config struct {
	// Providers lists configured LLM provider settings.
	Providers []ProviderConfig `json:"providers,omitempty"`

	// RateLimit configures global rate limiting.
	RateLimit *RateLimitConfig `json:"rate_limit,omitempty"`

	// CircuitBreaker configures circuit breaker behavior.
	CircuitBreaker *CircuitBreakerConfig `json:"circuit_breaker,omitempty"`

	// Cache configures response caching.
	Cache *CacheConfig `json:"cache,omitempty"`

	// Budget configures cost budget tracking.
	Budget *BudgetConfig `json:"budget,omitempty"`

	// LogLevel controls logging verbosity ("debug", "info", "warn", "error").
	LogLevel string `json:"log_level,omitempty"`

	// MetricsEnabled controls whether metrics collection is active.
	MetricsEnabled *bool `json:"metrics_enabled,omitempty"`

	// DashboardEnabled controls whether the dashboard is served.
	DashboardEnabled *bool `json:"dashboard_enabled,omitempty"`

	// Raw holds any additional unstructured fields.
	Raw map[string]json.RawMessage `json:"-"`
}

// ProviderConfig holds settings for a single LLM provider.
type ProviderConfig struct {
	// Name is the provider identifier (e.g. "openai", "anthropic").
	Name string `json:"name"`

	// APIKey is the provider API key (supports env var: "${ENV_VAR}").
	APIKey string `json:"api_key,omitempty"`

	// BaseURL overrides the default API endpoint.
	BaseURL string `json:"base_url,omitempty"`

	// DefaultModel sets the default model for this provider.
	DefaultModel string `json:"default_model,omitempty"`

	// Enabled controls whether this provider is active.
	Enabled *bool `json:"enabled,omitempty"`

	// Priority determines failover order (lower = higher priority).
	Priority int `json:"priority,omitempty"`
}

// RateLimitConfig configures token bucket rate limiting.
type RateLimitConfig struct {
	// RequestsPerSecond is the maximum request rate.
	RequestsPerSecond float64 `json:"requests_per_second"`

	// Burst is the maximum burst size.
	Burst int `json:"burst"`

	// Enabled controls whether rate limiting is active.
	Enabled *bool `json:"enabled,omitempty"`
}

// CircuitBreakerConfig configures the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening.
	FailureThreshold int `json:"failure_threshold"`

	// SuccessThreshold is the number of successes before closing.
	SuccessThreshold int `json:"success_threshold"`

	// Timeout is how long the circuit stays open before half-open.
	TimeoutSeconds float64 `json:"timeout_seconds"`

	// Enabled controls whether circuit breaker is active.
	Enabled *bool `json:"enabled,omitempty"`
}

// CacheConfig configures response caching.
type CacheConfig struct {
	// MaxEntries is the maximum number of cached responses.
	MaxEntries int `json:"max_entries"`

	// TTLSeconds is the cache entry time-to-live.
	TTLSeconds float64 `json:"ttl_seconds"`

	// Enabled controls whether caching is active.
	Enabled *bool `json:"enabled,omitempty"`
}

// BudgetConfig configures cost budget tracking.
type BudgetConfig struct {
	// MaxCostUSD is the maximum allowed cost per period.
	MaxCostUSD float64 `json:"max_cost_usd"`

	// Period is the budget period ("daily", "weekly", "monthly").
	Period string `json:"period"`

	// AlertThreshold is the percentage (0-100) at which to alert.
	AlertThreshold float64 `json:"alert_threshold"`

	// Enabled controls whether budget tracking is active.
	Enabled *bool `json:"enabled,omitempty"`
}

// Listener is notified when the configuration changes.
type Listener interface {
	OnConfigChange(oldCfg, newCfg *Config)
}

// ListenerFunc is a function adapter for Listener.
type ListenerFunc func(oldCfg, newCfg *Config)

// OnConfigChange calls the function.
func (f ListenerFunc) OnConfigChange(oldCfg, newCfg *Config) {
	f(oldCfg, newCfg)
}

// Watcher watches a config file for changes and provides atomic access.
type Watcher struct {
	path     string
	interval time.Duration

	current  atomic.Pointer[Config]
	hash     atomic.Value // [32]byte
	listeners []Listener

	mu     sync.RWMutex
	done   chan struct{}
	closed atomic.Bool

	// onError is called when config reload fails.
	onError func(error)

	// validate is an optional config validation function.
	validate func(*Config) error
}

// Option configures a Watcher.
type Option func(*Watcher)

// WithPollInterval sets how often the file is checked for changes.
// Default: 5 seconds.
func WithPollInterval(d time.Duration) Option {
	return func(w *Watcher) {
		w.interval = d
	}
}

// WithListener adds a listener that is called on config changes.
func WithListener(l Listener) Option {
	return func(w *Watcher) {
		w.listeners = append(w.listeners, l)
	}
}

// WithOnError sets an error handler for reload failures.
// By default, errors are silently ignored.
func WithOnError(fn func(error)) Option {
	return func(w *Watcher) {
		w.onError = fn
	}
}

// WithValidate sets a validation function applied to new configs.
// If validation fails, the new config is rejected.
func WithValidate(fn func(*Config) error) Option {
	return func(w *Watcher) {
		w.validate = fn
	}
}

// New creates a Watcher that watches the given file path.
// It loads the initial config immediately and starts background polling.
func New(path string, opts ...Option) (*Watcher, error) {
	if path == "" {
		return nil, errors.New("configwatch: path must not be empty")
	}

	w := &Watcher{
		path:     path,
		interval: 5 * time.Second,
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}

	// Load initial config
	cfg, hash, err := w.loadFile()
	if err != nil {
		return nil, fmt.Errorf("configwatch: initial load failed: %w", err)
	}
	if w.validate != nil {
		if err := w.validate(cfg); err != nil {
			return nil, fmt.Errorf("configwatch: initial config invalid: %w", err)
		}
	}
	w.current.Store(cfg)
	w.hash.Store(hash)

	// Start background watcher
	go w.watchLoop()

	return w, nil
}

// Config returns the current configuration. This is safe for concurrent use
// and never blocks — it reads an atomically-swapped pointer.
func (w *Watcher) Config() *Config {
	return w.current.Load()
}

// Close stops the file watcher. After Close returns, no further reloads occur.
func (w *Watcher) Close() error {
	if w.closed.Swap(true) {
		return nil // already closed
	}
	close(w.done)
	return nil
}

// ForceReload immediately reloads the config from disk, bypassing the
// hash check. Returns the new config or an error.
func (w *Watcher) ForceReload() (*Config, error) {
	if w.closed.Load() {
		return nil, errors.New("configwatch: watcher is closed")
	}

	cfg, hash, err := w.loadFile()
	if err != nil {
		return nil, err
	}
	if w.validate != nil {
		if err := w.validate(cfg); err != nil {
			return nil, fmt.Errorf("configwatch: validation failed: %w", err)
		}
	}

	old := w.current.Swap(cfg)
	w.hash.Store(hash)

	// Notify listeners
	for _, l := range w.listeners {
		l.OnConfigChange(old, cfg)
	}

	return cfg, nil
}

// AddListener adds a listener after construction. Safe for concurrent use.
func (w *Watcher) AddListener(l Listener) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listeners = append(w.listeners, l)
}

// watchLoop polls the config file at the configured interval.
func (w *Watcher) watchLoop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.checkAndReload()
		}
	}
}

// checkAndReload checks if the file has changed and reloads if so.
func (w *Watcher) checkAndReload() {
	cfg, hash, err := w.loadFile()
	if err != nil {
		if w.onError != nil {
			w.onError(err)
		}
		return
	}

	// Compare hashes — skip if unchanged
	currentHash := w.hash.Load().([32]byte)
	if hash == currentHash {
		return
	}

	// Validate before accepting
	if w.validate != nil {
		if err := w.validate(cfg); err != nil {
			if w.onError != nil {
				w.onError(fmt.Errorf("configwatch: validation failed, keeping old config: %w", err))
			}
			return
		}
	}

	// Atomic swap
	old := w.current.Swap(cfg)
	w.hash.Store(hash)

	// Notify listeners (outside lock)
	w.mu.RLock()
	listeners := make([]Listener, len(w.listeners))
	copy(listeners, w.listeners)
	w.mu.RUnlock()

	for _, l := range listeners {
		l.OnConfigChange(old, cfg)
	}
}

// loadFile reads and parses the config file, returning the config and its hash.
func (w *Watcher) loadFile() (*Config, [32]byte, error) {
	data, err := os.ReadFile(w.path)
	if err != nil {
		return nil, [32]byte{}, err
	}

	hash := sha256.Sum256(data)

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, [32]byte{}, fmt.Errorf("configwatch: json parse error: %w", err)
	}

	// Parse raw fields for extensibility
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		cfg.Raw = raw
	}

	return cfg, hash, nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	enabled := true
	return &Config{
		LogLevel:         "info",
		MetricsEnabled:   &enabled,
		DashboardEnabled: &enabled,
		RateLimit: &RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             200,
			Enabled:           &enabled,
		},
		CircuitBreaker: &CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 3,
			TimeoutSeconds:   30,
			Enabled:          &enabled,
		},
		Cache: &CacheConfig{
			MaxEntries: 1000,
			TTLSeconds: 300,
			Enabled:    &enabled,
		},
		Budget: &BudgetConfig{
			MaxCostUSD:     100,
			Period:         "daily",
			AlertThreshold: 80,
			Enabled:        &enabled,
		},
	}
}

// Enabled returns true if the bool pointer is non-nil and true.
// Returns false if the pointer is nil (considers nil as enabled by default).
func Enabled(b *bool) bool {
	if b == nil {
		return true
	}
	return *b
}

// MergeConfig merges overlay on top of base, with overlay fields taking precedence.
// Nil/zero fields in overlay are ignored, keeping the base value.
func MergeConfig(base, overlay *Config) *Config {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay
	}

	merged := *base // copy base

	if overlay.LogLevel != "" {
		merged.LogLevel = overlay.LogLevel
	}
	if overlay.MetricsEnabled != nil {
		merged.MetricsEnabled = overlay.MetricsEnabled
	}
	if overlay.DashboardEnabled != nil {
		merged.DashboardEnabled = overlay.DashboardEnabled
	}
	if len(overlay.Providers) > 0 {
		merged.Providers = overlay.Providers
	}
	if overlay.RateLimit != nil {
		merged.RateLimit = overlay.RateLimit
	}
	if overlay.CircuitBreaker != nil {
		merged.CircuitBreaker = overlay.CircuitBreaker
	}
	if overlay.Cache != nil {
		merged.Cache = overlay.Cache
	}
	if overlay.Budget != nil {
		merged.Budget = overlay.Budget
	}

	return &merged
}
