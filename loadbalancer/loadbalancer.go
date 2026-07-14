// Package loadbalancer provides provider load balancing for LLM API calls.
//
// It distributes requests across multiple provider instances using configurable
// strategies (round-robin, least-latency, random, weighted), with automatic
// health tracking and failover on errors.
//
// Usage:
//
//	openai1 := openai.New(openai.WithAPIKey("sk-key-1"))
//	openai2 := openai.New(openai.WithAPIKey("sk-key-2"))
//
//	lb := loadbalancer.New(
//	    loadbalancer.WithStrategy(loadbalancer.RoundRobin),
//	    loadbalancer.WithEndpoints(
//	        loadbalancer.NewEndpoint("openai-1", openai1),
//	        loadbalancer.NewEndpoint("openai-2", openai2),
//	    ),
//	)
//
//	// Use as a provider
//	resp, err := tracer.Chat(ctx, req, lb)
package loadbalancer

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atop0914/llmtrace"
)

// Strategy defines how requests are distributed across endpoints.
type Strategy int

const (
	// RoundRobin distributes requests sequentially across endpoints.
	RoundRobin Strategy = iota

	// LeastLatency routes to the endpoint with the lowest average latency.
	LeastLatency

	// Random selects a random healthy endpoint.
	Random

	// Weighted distributes requests proportionally based on endpoint weights.
	Weighted
)

var (
	// ErrNoEndpoints is returned when no endpoints are configured.
	ErrNoEndpoints = errors.New("loadbalancer: no endpoints configured")

	// ErrNoHealthyEndpoints is returned when all endpoints are unhealthy.
	ErrNoHealthyEndpoints = errors.New("loadbalancer: no healthy endpoints available")
)

// Endpoint represents a provider instance with health tracking.
type Endpoint struct {
	Name     string
	Provider llmtrace.Provider
	Weight   int // Used by Weighted strategy; 0 means equal weight

	mu               sync.RWMutex
	healthy          bool
	avgLatency       time.Duration
	totalCalls       int64
	totalErrors      int64
	lastError        time.Time
	lastSuccess      time.Time
	consecutiveFails int
}

// NewEndpoint creates a new endpoint with the given name and provider.
// The endpoint starts as healthy with equal weight.
func NewEndpoint(name string, provider llmtrace.Provider) *Endpoint {
	return &Endpoint{
		Name:     name,
		Provider: provider,
		Weight:   1,
		healthy:  true,
	}
}

// IsHealthy returns whether the endpoint is currently healthy.
func (e *Endpoint) IsHealthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.healthy
}

// AvgLatency returns the rolling average latency of this endpoint.
func (e *Endpoint) AvgLatency() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.avgLatency
}

// Stats returns a snapshot of endpoint statistics.
func (e *Endpoint) Stats() EndpointStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return EndpointStats{
		Name:        e.Name,
		Healthy:     e.healthy,
		AvgLatency:  e.avgLatency,
		TotalCalls:  e.totalCalls,
		TotalErrors: e.totalErrors,
		ErrorRate:   e.errorRate(),
		Weight:      e.Weight,
	}
}

// errorRate returns the error rate. Must be called with lock held.
func (e *Endpoint) errorRate() float64 {
	if e.totalCalls == 0 {
		return 0
	}
	return float64(e.totalErrors) / float64(e.totalCalls)
}

// recordSuccess records a successful call with latency.
func (e *Endpoint) recordSuccess(latency time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalCalls++
	e.lastSuccess = time.Now()
	e.consecutiveFails = 0
	e.healthy = true

	// Exponential moving average (alpha=0.2)
	if e.avgLatency == 0 {
		e.avgLatency = latency
	} else {
		e.avgLatency = time.Duration(
			0.8*float64(e.avgLatency) + 0.2*float64(latency),
		)
	}
}

// recordError records a failed call.
func (e *Endpoint) recordError() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalCalls++
	e.totalErrors++
	e.lastError = time.Now()
	e.consecutiveFails++

	// Mark unhealthy after 3 consecutive failures
	if e.consecutiveFails >= 3 {
		e.healthy = false
	}
}

// EndpointStats holds statistics for an endpoint.
type EndpointStats struct {
	Name        string
	Healthy     bool
	AvgLatency  time.Duration
	TotalCalls  int64
	TotalErrors int64
	ErrorRate   float64
	Weight      int
}

// Config holds load balancer configuration.
type Config struct {
	// Strategy for distributing requests.
	Strategy Strategy

	// Endpoints to balance across.
	Endpoints []*Endpoint

	// HealthCheckInterval is how often to probe unhealthy endpoints.
	// 0 disables health checks (default: 30s).
	HealthCheckInterval time.Duration

	// HealthCheckTimeout for probe requests. Default: 5s.
	HealthCheckTimeout time.Duration

	// FailureThreshold is consecutive failures before marking unhealthy. Default: 3.
	FailureThreshold int
}

// LoadBalancer distributes LLM requests across multiple provider endpoints.
// It implements the llmtrace.Provider interface, so it can be used anywhere
// a Provider is expected.
//
// LoadBalancer is safe for concurrent use.
type LoadBalancer struct {
	config    Config
	counter   atomic.Int64
	weightSum int
	stopCh    chan struct{}
}

// Option configures a LoadBalancer.
type Option func(*Config)

// WithStrategy sets the load balancing strategy.
func WithStrategy(s Strategy) Option {
	return func(c *Config) { c.Strategy = s }
}

// WithEndpoints sets the provider endpoints.
func WithEndpoints(endpoints ...*Endpoint) Option {
	return func(c *Config) { c.Endpoints = endpoints }
}

// WithHealthCheckInterval sets the health check interval.
func WithHealthCheckInterval(d time.Duration) Option {
	return func(c *Config) { c.HealthCheckInterval = d }
}

// WithHealthCheckTimeout sets the health check probe timeout.
func WithHealthCheckTimeout(d time.Duration) Option {
	return func(c *Config) { c.HealthCheckTimeout = d }
}

// WithFailureThreshold sets the consecutive failure threshold.
func WithFailureThreshold(n int) Option {
	return func(c *Config) { c.FailureThreshold = n }
}

// New creates a LoadBalancer with the given options.
//
//	lb := loadbalancer.New(
//	    loadbalancer.WithStrategy(loadbalancer.LeastLatency),
//	    loadbalancer.WithEndpoints(
//	        loadbalancer.NewEndpoint("primary", primaryProvider),
//	        loadbalancer.NewEndpoint("secondary", secondaryProvider),
//	    ),
//	)
func New(opts ...Option) *LoadBalancer {
	cfg := Config{
		Strategy:            RoundRobin,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		FailureThreshold:    3,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	lb := &LoadBalancer{
		config: cfg,
		stopCh: make(chan struct{}),
	}

	// Calculate weight sum for weighted strategy
	for _, ep := range cfg.Endpoints {
		w := ep.Weight
		if w <= 0 {
			w = 1
		}
		lb.weightSum += w
	}

	// Start health check goroutine if interval > 0 and there are endpoints
	if cfg.HealthCheckInterval > 0 && len(cfg.Endpoints) > 0 {
		go lb.healthCheckLoop()
	}

	return lb
}

// Name returns the load balancer strategy name.
func (lb *LoadBalancer) Name() string {
	switch lb.config.Strategy {
	case RoundRobin:
		return "loadbalancer-round-robin"
	case LeastLatency:
		return "loadbalancer-least-latency"
	case Random:
		return "loadbalancer-random"
	case Weighted:
		return "loadbalancer-weighted"
	default:
		return "loadbalancer"
	}
}

// Complete implements llmtrace.Provider. It selects an endpoint using the
// configured strategy, calls the provider, and tracks latency/errors.
func (lb *LoadBalancer) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	ep, err := lb.selectEndpoint()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := ep.Provider.Complete(ctx, req)
	latency := time.Since(start)

	if err != nil {
		ep.recordError()
		// Try failover to another endpoint
		if fallback := lb.selectFallback(ep); fallback != nil {
			start2 := time.Now()
			resp2, err2 := fallback.Provider.Complete(ctx, req)
			latency2 := time.Since(start2)
			if err2 != nil {
				fallback.recordError()
				return nil, err
			}
			fallback.recordSuccess(latency2)
			return resp2, nil
		}
		return nil, err
	}

	ep.recordSuccess(latency)
	return resp, nil
}

// Stream implements llmtrace.Provider. It selects an endpoint and streams.
func (lb *LoadBalancer) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	ep, err := lb.selectEndpoint()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	ch, err := ep.Provider.Stream(ctx, req)
	if err != nil {
		ep.recordError()
		// Try failover
		if fallback := lb.selectFallback(ep); fallback != nil {
			ch2, err2 := fallback.Provider.Stream(ctx, req)
			if err2 != nil {
				fallback.recordError()
				return nil, err
			}
			fallback.recordSuccess(time.Since(start))
			return ch2, nil
		}
		return nil, err
	}

	// Track latency after stream completes
	go func() {
		for range ch {
			// Drain channel to measure full latency
		}
		ep.recordSuccess(time.Since(start))
	}()

	return ch, nil
}

// DefaultModel returns the default model from the first healthy endpoint.
func (lb *LoadBalancer) DefaultModel() string {
	for _, ep := range lb.config.Endpoints {
		if ep.IsHealthy() {
			return ep.Provider.DefaultModel()
		}
	}
	if len(lb.config.Endpoints) > 0 {
		return lb.config.Endpoints[0].Provider.DefaultModel()
	}
	return ""
}

// SupportsStreaming returns true if any healthy endpoint supports streaming.
func (lb *LoadBalancer) SupportsStreaming() bool {
	for _, ep := range lb.config.Endpoints {
		if ep.IsHealthy() && ep.Provider.SupportsStreaming() {
			return true
		}
	}
	return false
}

// Endpoints returns a copy of all endpoint stats.
func (lb *LoadBalancer) Endpoints() []EndpointStats {
	stats := make([]EndpointStats, len(lb.config.Endpoints))
	for i, ep := range lb.config.Endpoints {
		stats[i] = ep.Stats()
	}
	return stats
}

// Stop stops the health check goroutine.
func (lb *LoadBalancer) Stop() {
	select {
	case <-lb.stopCh:
		// Already stopped
	default:
		close(lb.stopCh)
	}
}

// selectEndpoint picks an endpoint based on the configured strategy.
func (lb *LoadBalancer) selectEndpoint() (*Endpoint, error) {
	if len(lb.config.Endpoints) == 0 {
		return nil, ErrNoEndpoints
	}

	healthy := lb.healthyEndpoints()
	if len(healthy) == 0 {
		return nil, ErrNoHealthyEndpoints
	}

	switch lb.config.Strategy {
	case RoundRobin:
		return lb.selectRoundRobin(healthy), nil
	case LeastLatency:
		return lb.selectLeastLatency(healthy), nil
	case Random:
		return lb.selectRandom(healthy), nil
	case Weighted:
		return lb.selectWeighted(healthy), nil
	default:
		return lb.selectRoundRobin(healthy), nil
	}
}

// healthyEndpoints returns a slice of healthy endpoints.
func (lb *LoadBalancer) healthyEndpoints() []*Endpoint {
	result := make([]*Endpoint, 0, len(lb.config.Endpoints))
	for _, ep := range lb.config.Endpoints {
		if ep.IsHealthy() {
			result = append(result, ep)
		}
	}
	return result
}

// selectRoundRobin picks the next endpoint in round-robin order.
func (lb *LoadBalancer) selectRoundRobin(healthy []*Endpoint) *Endpoint {
	idx := lb.counter.Add(1) - 1
	return healthy[int(idx)%len(healthy)]
}

// selectLeastLatency picks the endpoint with the lowest average latency.
func (lb *LoadBalancer) selectLeastLatency(healthy []*Endpoint) *Endpoint {
	best := healthy[0]
	bestLat := best.AvgLatency()

	for _, ep := range healthy[1:] {
		lat := ep.AvgLatency()
		// Prefer endpoints with no latency data (haven't been tried yet)
		if lat == 0 && bestLat > 0 {
			return ep
		}
		if lat < bestLat && lat > 0 {
			best = ep
			bestLat = lat
		}
	}
	return best
}

// selectRandom picks a random healthy endpoint.
func (lb *LoadBalancer) selectRandom(healthy []*Endpoint) *Endpoint {
	return healthy[rand.Intn(len(healthy))]
}

// selectWeighted picks an endpoint proportional to its weight.
func (lb *LoadBalancer) selectWeighted(healthy []*Endpoint) *Endpoint {
	// Recalculate weight sum for healthy endpoints
	sum := 0
	for _, ep := range healthy {
		w := ep.Weight
		if w <= 0 {
			w = 1
		}
		sum += w
	}

	r := rand.Intn(sum)
	cumulative := 0
	for _, ep := range healthy {
		w := ep.Weight
		if w <= 0 {
			w = 1
		}
		cumulative += w
		if r < cumulative {
			return ep
		}
	}
	return healthy[len(healthy)-1]
}

// selectFallback tries to find a different healthy endpoint for failover.
func (lb *LoadBalancer) selectFallback(failed *Endpoint) *Endpoint {
	healthy := lb.healthyEndpoints()
	for _, ep := range healthy {
		if ep.Name != failed.Name {
			return ep
		}
	}
	return nil
}

// healthCheckLoop periodically probes unhealthy endpoints.
func (lb *LoadBalancer) healthCheckLoop() bool {
	ticker := time.NewTicker(lb.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lb.stopCh:
			return true
		case <-ticker.C:
			lb.probeUnhealthy()
		}
	}
}

// probeUnhealthy sends a lightweight request to unhealthy endpoints.
func (lb *LoadBalancer) probeUnhealthy() {
	for _, ep := range lb.config.Endpoints {
		if ep.IsHealthy() {
			continue
		}

		// Use a simple request for probing
		ctx, cancel := context.WithTimeout(context.Background(), lb.config.HealthCheckTimeout)
		req := &llmtrace.Request{
			Model: ep.Provider.DefaultModel(),
			Messages: []llmtrace.Message{
				{Role: "user", Content: "ping"},
			},
			MaxTokens: intPtr(1),
		}

		start := time.Now()
		_, err := ep.Provider.Complete(ctx, req)
		latency := time.Since(start)
		cancel()

		if err == nil {
			ep.recordSuccess(latency)
		} else {
			ep.recordError()
		}
	}
}

func intPtr(v int) *int { return &v }
