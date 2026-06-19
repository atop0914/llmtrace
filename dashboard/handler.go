package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/metrics"
)

//go:embed static/*
var staticFiles embed.FS

// Config holds dashboard configuration.
type Config struct {
	// SSEInterval is how often SSE pushes updates. Default: 2s.
	SSEInterval time.Duration

	// TraceStore is an optional TraceStore for the /api/traces endpoint.
	// If nil, trace endpoints return empty results.
	TraceStore TraceStorer
}

// TraceStorer is the interface for querying traces.
// *llmtrace.TraceStore satisfies this interface.
type TraceStorer interface {
	Query(q TraceQuery) []TraceRecord
	TraceSummary() TraceSummaryResult
	Len() int
}

// TraceQuery re-exports llmtrace.TraceQuery for dashboard use.
type TraceQuery = llmtrace.TraceQuery

// TraceRecord re-exports llmtrace.TraceRecord for dashboard use.
type TraceRecord = llmtrace.TraceRecord

// TraceSummaryResult re-exports llmtrace.TraceSummaryResult for dashboard use.
type TraceSummaryResult = llmtrace.TraceSummaryResult

// Handler returns an http.Handler that serves the dashboard.
// It provides:
//
//   - GET /api/overview   — overview metrics
//   - GET /api/providers        — per-provider breakdown
//   - GET /api/providers/health — provider health and efficiency metrics
//   - GET /api/models     — per-model breakdown
//   - GET /api/latency    — latency distribution
//   - GET /api/costs      — cost analysis
//   - GET /api/errors     — error statistics
//   - GET /api/traces     — trace explorer
//   - GET /api/events     — SSE real-time stream
//   - GET /               — dashboard UI
//
// Usage:
//
//	reg := metrics.NewRegistry("llmtrace")
//	collector := metrics.NewLLMCollector(reg)
//
//	// ... use collector as middleware ...
//
//	dash := dashboard.Handler(reg, dashboard.Config{
//	    SSEInterval: 2 * time.Second,
//	})
//	log.Fatal(http.ListenAndServe(":8080", dash))
func Handler(reg *metrics.Registry, cfg Config) http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	api := newAPIHandler(reg, cfg.TraceStore)
	mux.Handle("/api/overview", api)
	mux.Handle("/api/providers", api)
	mux.Handle("/api/providers/health", api)
	mux.Handle("/api/models", api)
	mux.Handle("/api/latency", api)
	mux.Handle("/api/costs", api)
	mux.Handle("/api/errors", api)
	mux.Handle("/api/traces", api)
	mux.Handle("/api/traces/summary", api)

	// SSE endpoint
	sse := newSSEHandler(reg, cfg.SSEInterval)
	mux.Handle("/api/events", sse)

	// Embedded static files (dashboard UI)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Should never happen with go:embed
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failed to load embedded files", http.StatusInternalServerError)
		})
	} else {
		mux.Handle("/", http.FileServer(http.FS(staticFS)))
	}

	return mux
}

// HandlerFromCollector creates a dashboard handler directly from an LLMCollector.
// This is a convenience function that extracts the registry from the collector.
func HandlerFromCollector(reg *metrics.Registry, cfg Config) http.Handler {
	return Handler(reg, cfg)
}
