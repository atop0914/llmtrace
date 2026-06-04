package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/atop0914/llmtrace/metrics"
)

// sseHandler serves Server-Sent Events for real-time dashboard updates.
type sseHandler struct {
	registry *metrics.Registry
	interval time.Duration
}

// newSSEHandler creates a new SSE handler with the given push interval.
func newSSEHandler(reg *metrics.Registry, interval time.Duration) *sseHandler {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &sseHandler{registry: reg, interval: interval}
}

// ServeHTTP handles SSE connections.
// It streams overview + provider data every interval seconds.
func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctx := r.Context()
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Send initial data immediately
	h.sendEvent(w, flusher, "overview", h.buildOverview())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sendEvent(w, flusher, "overview", h.buildOverview())
		}
	}
}

func (h *sseHandler) sendEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// buildOverview constructs an OverviewResponse from current metrics.
func (h *sseHandler) buildOverview() OverviewResponse {
	snap := h.registry.Collect()

	resp := OverviewResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	providers := make(map[string]bool)
	models := make(map[string]bool)

	for _, cs := range snap.Counters {
		switch {
		case strings.HasSuffix(cs.Name, "_requests_total"):
			for _, s := range cs.Samples {
				resp.TotalRequests += int64(s.Value)
				if len(s.LabelValues) > 0 {
					providers[s.LabelValues[0]] = true
				}
				if len(s.LabelValues) > 1 {
					models[s.LabelValues[1]] = true
				}
			}
		case strings.HasSuffix(cs.Name, "_tokens_total") && !strings.Contains(cs.Name, "input") && !strings.Contains(cs.Name, "output"):
			for _, s := range cs.Samples {
				resp.TotalTokens += int64(s.Value)
			}
		case strings.HasSuffix(cs.Name, "_input_tokens_total"):
			for _, s := range cs.Samples {
				resp.InputTokens += int64(s.Value)
			}
		case strings.HasSuffix(cs.Name, "_output_tokens_total"):
			for _, s := range cs.Samples {
				resp.OutputTokens += int64(s.Value)
			}
		case strings.HasSuffix(cs.Name, "_cost_usd_total"):
			for _, s := range cs.Samples {
				resp.TotalCostUSD += s.Value
			}
		case strings.HasSuffix(cs.Name, "_errors_total"):
			for _, s := range cs.Samples {
				resp.TotalErrors += int64(s.Value)
			}
		}
	}

	for _, gs := range snap.Gauges {
		if strings.HasSuffix(gs.Name, "_active_requests") {
			for _, s := range gs.Samples {
				resp.ActiveRequests += int64(s.Value)
			}
		}
	}

	for _, hs := range snap.Histograms {
		if strings.HasSuffix(hs.Name, "_request_duration_seconds") {
			var totalCount uint64
			var totalSum float64
			for _, s := range hs.Samples {
				totalCount += s.Count
				totalSum += s.Sum
			}
			if totalCount > 0 {
				resp.AvgLatencyMS = (totalSum / float64(totalCount)) * 1000
			}
		}
	}

	resp.ProviderCount = len(providers)
	resp.ModelCount = len(models)

	return resp
}
