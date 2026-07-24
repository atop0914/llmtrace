// Package healthcheck provides HTTP handlers for liveness and readiness probes,
// following Kubernetes health check conventions.
//
// Liveness probes indicate whether the process is running. A failed liveness
// probe causes the container to be restarted.
//
// Readiness probes indicate whether the service is ready to accept traffic.
// A failed readiness probe removes the pod from service endpoints.
//
// Usage:
//
//	hc := healthcheck.New()
//	hc.AddReadinessCheck("database", func(ctx context.Context) error {
//	    return db.PingContext(ctx)
//	})
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/healthz", hc.LiveHandler)
//	mux.HandleFunc("/readyz", hc.ReadyHandler)
package healthcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status represents the health check result status.
type Status string

const (
	// StatusUp indicates the check passed.
	StatusUp Status = "up"
	// StatusDown indicates the check failed.
	StatusDown Status = "down"
)

// CheckFunc is a function that performs a health check.
// It should return nil if the component is healthy, or an error describing
// the failure. The context carries the check deadline.
type CheckFunc func(ctx context.Context) error

// CheckResult holds the result of a single health check.
type CheckResult struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Response is the JSON response body for health check endpoints.
type Response struct {
	Status   Status                 `json:"status"`
	Checks   map[string]CheckResult `json:"checks,omitempty"`
	Duration string                 `json:"duration,omitempty"`
}

// Config configures the health checker behavior.
type Config struct {
	// Timeout is the maximum duration for each readiness check.
	// Default: 5 seconds.
	Timeout time.Duration

	// AggregateStatus controls whether the top-level status reflects
	// the worst individual check result. When false, the top-level
	// status is always "up" and individual checks carry their own status.
	// Default: true.
	AggregateStatus bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:         5 * time.Second,
		AggregateStatus: true,
	}
}

// HealthChecker provides liveness and readiness HTTP handlers.
type HealthChecker struct {
	cfg     Config
	mu      sync.RWMutex
	checks  map[string]CheckFunc
	started time.Time
}

// New creates a new HealthChecker with the given config.
// If cfg is zero-valued, DefaultConfig() is used.
func New(cfg Config) *HealthChecker {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &HealthChecker{
		cfg:     cfg,
		checks:  make(map[string]CheckFunc),
		started: time.Now(),
	}
}

// AddReadinessCheck registers a named readiness check.
// If a check with the same name already exists, it is replaced.
func (hc *HealthChecker) AddReadinessCheck(name string, fn CheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = fn
}

// RemoveCheck removes a named readiness check.
func (hc *HealthChecker) RemoveCheck(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	delete(hc.checks, name)
}

// CheckNames returns the names of all registered readiness checks.
func (hc *HealthChecker) CheckNames() []string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	names := make([]string, 0, len(hc.checks))
	for name := range hc.checks {
		names = append(names, name)
	}
	return names
}

// LiveHandler handles liveness probe requests (GET /healthz).
// Returns 200 OK if the process is running.
// This never fails unless the process itself is unresponsive.
func (hc *HealthChecker) LiveHandler(w http.ResponseWriter, r *http.Request) {
	resp := Response{
		Status: StatusUp,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ReadyHandler handles readiness probe requests (GET /readyz).
// Runs all registered checks and returns 200 if all pass, 503 otherwise.
func (hc *HealthChecker) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	hc.mu.RLock()
	checks := make(map[string]CheckFunc, len(hc.checks))
	for k, v := range hc.checks {
		checks[k] = v
	}
	hc.mu.RUnlock()

	results := make(map[string]CheckResult, len(checks))
	overallUp := true

	for name, fn := range checks {
		ctx, cancel := context.WithTimeout(r.Context(), hc.cfg.Timeout)
		err := fn(ctx)
		cancel()

		if err != nil {
			results[name] = CheckResult{
				Status:  StatusDown,
				Message: err.Error(),
			}
			overallUp = false
		} else {
			results[name] = CheckResult{
				Status: StatusUp,
			}
		}
	}

	resp := Response{
		Checks:   results,
		Duration: time.Since(start).String(),
	}

	if hc.cfg.AggregateStatus {
		if overallUp {
			resp.Status = StatusUp
		} else {
			resp.Status = StatusDown
		}
	} else {
		resp.Status = StatusUp
	}

	code := http.StatusOK
	if resp.Status == StatusDown {
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, resp)
}

// Handler returns an http.Handler that routes /healthz to liveness
// and /readyz to readiness probes. All other paths get 404.
func (hc *HealthChecker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hc.LiveHandler)
	mux.HandleFunc("/readyz", hc.ReadyHandler)
	return mux
}

// Uptime returns the duration since the HealthChecker was created.
func (hc *HealthChecker) Uptime() time.Duration {
	return time.Since(hc.started)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
