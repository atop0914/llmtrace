// Package llmtrace provides OpenTelemetry-native observability for LLM calls.
//
// This file implements a Provider Fallback Router that automatically fails over
// across multiple providers when one becomes unavailable. It implements the
// Provider interface, so it can be used anywhere a single Provider is expected.

package llmtrace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// FallbackConfig configures the FallbackRouter behavior.
type FallbackConfig struct {
	// Strategy determines the order in which providers are tried.
	// Default: StrategyPriority (try in registration order).
	Strategy FallbackStrategy

	// HealthCheckInterval is how often to probe unhealthy providers.
	// Set to 0 to disable background health checks. Default: 30s.
	HealthCheckInterval time.Duration

	// Cooldown is how long a provider is marked unhealthy after a failure.
	// Default: 60s.
	Cooldown time.Duration

	// MaxAttempts is the maximum number of providers to try per request.
	// 0 means try all providers. Default: 0 (all).
	MaxAttempts int

	// OnFallback is called when a request fails on one provider and falls
	// back to the next. Useful for logging/alerting.
	OnFallback func(from, to string, err error)

	// OnProviderHealthChange is called when a provider's health status changes.
	OnProviderHealthChange func(name string, healthy bool)
}

// FallbackStrategy determines provider selection order.
type FallbackStrategy int

const (
	// StrategyPriority tries providers in registration order (first registered = highest priority).
	StrategyPriority FallbackStrategy = iota

	// StrategyRoundRobin distributes requests across healthy providers in round-robin order.
	StrategyRoundRobin
)

// DefaultFallbackConfig returns a FallbackConfig with sensible defaults.
func DefaultFallbackConfig() FallbackConfig {
	return FallbackConfig{
		Strategy:            StrategyPriority,
		HealthCheckInterval: 30 * time.Second,
		Cooldown:            60 * time.Second,
	}
}

// providerEntry holds a provider and its health state.
type providerEntry struct {
	provider    Provider
	name        string
	healthy     atomic.Bool
	lastFailure time.Time
	mu          sync.RWMutex
}

// FallbackRouter manages multiple providers with automatic failover.
// It implements the Provider interface, so it can be used anywhere a single
// Provider is expected.
//
// Usage:
//
//	router := llmtrace.NewFallbackRouter(
//	    llmtrace.FallbackConfig{Strategy: llmtrace.StrategyPriority},
//	    openaiProvider,
//	    anthropicProvider,
//	    geminiProvider,
//	)
//	resp, err := tracer.Chat(ctx, req, router)
type FallbackRouter struct {
	config   FallbackConfig
	entries  []*providerEntry
	rrIndex  atomic.Uint64
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewFallbackRouter creates a new FallbackRouter with the given config and providers.
// Providers are tried in the order they are passed (highest priority first).
func NewFallbackRouter(config FallbackConfig, providers ...Provider) *FallbackRouter {
	if config.Cooldown == 0 {
		config.Cooldown = 60 * time.Second
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}

	r := &FallbackRouter{
		config:  config,
		entries: make([]*providerEntry, 0, len(providers)),
		stopCh:  make(chan struct{}),
	}

	for _, p := range providers {
		entry := &providerEntry{
			provider: p,
			name:     p.Name(),
		}
		entry.healthy.Store(true)
		r.entries = append(r.entries, entry)
	}

	if config.HealthCheckInterval > 0 && len(providers) > 0 {
		go r.healthCheckLoop()
	}

	return r
}

// Name returns a combined name for logging/tracing.
func (r *FallbackRouter) Name() string {
	if len(r.entries) == 0 {
		return "fallback(empty)"
	}
	if len(r.entries) == 1 {
		return r.entries[0].name
	}
	return r.entries[0].name + "+fallback"
}

// DefaultModel returns the default model of the first (highest priority) provider.
func (r *FallbackRouter) DefaultModel() string {
	if len(r.entries) == 0 {
		return ""
	}
	return r.entries[0].provider.DefaultModel()
}

// SupportsStreaming returns true if at least one provider supports streaming.
func (r *FallbackRouter) SupportsStreaming() bool {
	for _, e := range r.entries {
		if e.provider.SupportsStreaming() {
			return true
		}
	}
	return false
}

// Complete makes a completion request with automatic failover.
func (r *FallbackRouter) Complete(ctx context.Context, req *Request) (*Response, error) {
	order := r.providerOrder()
	attempts := r.maxAttempts(len(order))

	var lastErr error
	tried := 0
	for _, idx := range order {
		if tried >= attempts {
			break
		}

		entry := r.entries[idx]
		if !entry.IsHealthy(r.config.Cooldown) {
			continue
		}

		tried++
		resp, err := entry.provider.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		r.markFailure(entry)

		// Find next healthy provider for fallback notification
		if r.config.OnFallback != nil {
			if next := r.nextHealthy(idx, order); next != nil {
				r.config.OnFallback(entry.name, next.name, err)
			}
		}
	}

	if lastErr == nil {
		lastErr = errors.New("llmtrace: no healthy providers available")
	}
	return nil, lastErr
}

// Stream makes a streaming request with automatic failover.
func (r *FallbackRouter) Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
	order := r.providerOrder()
	attempts := r.maxAttempts(len(order))

	var lastErr error
	tried := 0
	for _, idx := range order {
		if tried >= attempts {
			break
		}

		entry := r.entries[idx]
		if !entry.IsHealthy(r.config.Cooldown) {
			continue
		}

		if !entry.provider.SupportsStreaming() {
			continue
		}

		tried++
		ch, err := entry.provider.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}

		lastErr = err
		r.markFailure(entry)

		if r.config.OnFallback != nil {
			if next := r.nextHealthy(idx, order); next != nil {
				r.config.OnFallback(entry.name, next.name, err)
			}
		}
	}

	if lastErr == nil {
		lastErr = errors.New("llmtrace: no healthy streaming providers available")
	}
	return nil, lastErr
}

// Providers returns the names of all registered providers.
func (r *FallbackRouter) Providers() []string {
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.name
	}
	return names
}

// HealthStatus returns the health status of each provider.
func (r *FallbackRouter) HealthStatus() map[string]bool {
	status := make(map[string]bool, len(r.entries))
	for _, e := range r.entries {
		status[e.name] = e.healthy.Load()
	}
	return status
}

// Close stops background health checks and releases resources.
func (r *FallbackRouter) Close() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
}

// ResetHealth manually marks all providers as healthy.
func (r *FallbackRouter) ResetHealth() {
	for _, e := range r.entries {
		wasHealthy := e.healthy.Swap(true)
		if !wasHealthy && r.config.OnProviderHealthChange != nil {
			r.config.OnProviderHealthChange(e.name, true)
		}
	}
}

// --- internal methods ---

// providerOrder returns the indices of providers in the order they should be tried.
func (r *FallbackRouter) providerOrder() []int {
	n := len(r.entries)
	if n == 0 {
		return nil
	}

	switch r.config.Strategy {
	case StrategyRoundRobin:
		start := int(r.rrIndex.Add(1)-1) % n
		order := make([]int, n)
		for i := range n {
			order[i] = (start + i) % n
		}
		return order
	default: // StrategyPriority
		order := make([]int, n)
		for i := range n {
			order[i] = i
		}
		return order
	}
}

// maxAttempts returns the effective number of providers to try.
func (r *FallbackRouter) maxAttempts(available int) int {
	if r.config.MaxAttempts <= 0 {
		return available
	}
	if r.config.MaxAttempts > available {
		return available
	}
	return r.config.MaxAttempts
}

// markFailure marks a provider as unhealthy and records the failure time.
func (r *FallbackRouter) markFailure(entry *providerEntry) {
	entry.mu.Lock()
	entry.lastFailure = time.Now()
	entry.mu.Unlock()

	wasHealthy := entry.healthy.Swap(false)
	if wasHealthy && r.config.OnProviderHealthChange != nil {
		r.config.OnProviderHealthChange(entry.name, false)
	}
}

// nextHealthy finds the next healthy provider after the current index.
func (r *FallbackRouter) nextHealthy(currentIdx int, order []int) *providerEntry {
	foundCurrent := false
	for _, idx := range order {
		if idx == currentIdx {
			foundCurrent = true
			continue
		}
		if foundCurrent && r.entries[idx].IsHealthy(r.config.Cooldown) {
			return r.entries[idx]
		}
	}
	return nil
}

// healthCheckLoop periodically checks unhealthy providers.
func (r *FallbackRouter) healthCheckLoop() {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkUnhealthy()
		}
	}
}

// checkUnhealthy attempts to recover providers that have been down longer than Cooldown.
func (r *FallbackRouter) checkUnhealthy() {
	now := time.Now()
	for _, entry := range r.entries {
		if entry.healthy.Load() {
			continue
		}

		entry.mu.RLock()
		cooldownExpired := now.Sub(entry.lastFailure) >= r.config.Cooldown
		entry.mu.RUnlock()

		if cooldownExpired {
			// Mark as healthy to allow the next request to try it
			entry.healthy.Store(true)
			if r.config.OnProviderHealthChange != nil {
				r.config.OnProviderHealthChange(entry.name, true)
			}
		}
	}
}

// IsHealthy returns whether the provider entry is currently healthy.
func (e *providerEntry) IsHealthy(cooldown time.Duration) bool {
	if e.healthy.Load() {
		return true
	}

	// Check if cooldown has expired
	e.mu.RLock()
	elapsed := time.Since(e.lastFailure)
	e.mu.RUnlock()

	if elapsed >= cooldown {
		e.healthy.Store(true)
		return true
	}
	return false
}