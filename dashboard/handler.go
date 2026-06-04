package dashboard

import (
	"net/http"
	"time"

	"github.com/atop0914/llmtrace/metrics"
)

// Config holds dashboard configuration.
type Config struct {
	// SSEInterval is how often SSE pushes updates. Default: 2s.
	SSEInterval time.Duration

	// StaticFS is the embedded filesystem for static assets.
	// If nil, only API endpoints are served.
	StaticFS http.FileSystem
}

// Handler returns an http.Handler that serves the dashboard.
// It provides:
//
//   - GET /api/overview   — overview metrics
//   - GET /api/providers  — per-provider breakdown
//   - GET /api/models     — per-model breakdown
//   - GET /api/latency    — latency distribution
//   - GET /api/costs      — cost analysis
//   - GET /api/errors     — error statistics
//   - GET /api/events     — SSE real-time stream
//   - GET /               — dashboard UI (if StaticFS provided)
//
// Usage:
//
//   reg := metrics.NewRegistry("llmtrace")
//   collector := metrics.NewLLMCollector(reg)
//
//   // ... use collector as middleware ...
//
//   dash := dashboard.Handler(reg, dashboard.Config{
//       SSEInterval: 2 * time.Second,
//   })
//   log.Fatal(http.ListenAndServe(":8080", dash))
func Handler(reg *metrics.Registry, cfg Config) http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	api := newAPIHandler(reg)
	mux.Handle("/api/overview", api)
	mux.Handle("/api/providers", api)
	mux.Handle("/api/models", api)
	mux.Handle("/api/latency", api)
	mux.Handle("/api/costs", api)
	mux.Handle("/api/errors", api)

	// SSE endpoint
	sse := newSSEHandler(reg, cfg.SSEInterval)
	mux.Handle("/api/events", sse)

	// Static files (dashboard UI)
	if cfg.StaticFS != nil {
		mux.Handle("/", http.FileServer(cfg.StaticFS))
	} else {
		// Serve a minimal landing page when no static FS is provided
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(landingHTML))
		})
	}

	return mux
}

// HandlerFromCollector creates a dashboard handler directly from an LLMCollector.
// This is a convenience function that extracts the registry from the collector.
func HandlerFromCollector(reg *metrics.Registry, cfg Config) http.Handler {
	return Handler(reg, cfg)
}

const landingHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLMTrace Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: system-ui, -apple-system, sans-serif; background: #0f172a; color: #e2e8f0; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
        .container { text-align: center; max-width: 600px; padding: 2rem; }
        h1 { font-size: 2rem; margin-bottom: 1rem; color: #38bdf8; }
        p { color: #94a3b8; line-height: 1.6; margin-bottom: 1.5rem; }
        .api-list { text-align: left; background: #1e293b; border-radius: 8px; padding: 1.5rem; }
        .api-list h3 { color: #f8fafc; margin-bottom: 0.75rem; }
        .api-list a { color: #38bdf8; text-decoration: none; display: block; padding: 0.25rem 0; font-family: monospace; }
        .api-list a:hover { text-decoration: underline; }
        .badge { display: inline-block; background: #10b981; color: white; padding: 0.2rem 0.6rem; border-radius: 4px; font-size: 0.75rem; margin-bottom: 1rem; }
    </style>
</head>
<body>
    <div class="container">
        <span class="badge">Live</span>
        <h1>🔍 LLMTrace Dashboard</h1>
        <p>OpenTelemetry-native LLM Observability SDK for Go.<br>
        Full dashboard UI coming soon — API endpoints are live.</p>
        <div class="api-list">
            <h3>Available API Endpoints</h3>
            <a href="/api/overview">GET /api/overview</a>
            <a href="/api/providers">GET /api/providers</a>
            <a href="/api/models">GET /api/models</a>
            <a href="/api/latency">GET /api/latency</a>
            <a href="/api/costs">GET /api/costs</a>
            <a href="/api/errors">GET /api/errors</a>
            <a href="/api/events">GET /api/events (SSE)</a>
        </div>
    </div>
</body>
</html>`
