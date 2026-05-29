package metrics

import (
	"net/http"
)

// Handler returns an http.Handler that serves metrics in Prometheus text format.
//
// Usage:
//
//	reg := metrics.NewRegistry("llmtrace")
//	http.Handle("/metrics", metrics.Handler(reg))
//	log.Fatal(http.ListenAndServe(":2112", nil))
func Handler(reg *Registry) http.Handler {
	return &metricsHandler{registry: reg}
}

type metricsHandler struct {
	registry *Registry
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.registry.WritePrometheus()))
}
