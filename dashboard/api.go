package dashboard

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/atop0914/llmtrace/metrics"
)

// apiHandler serves the dashboard JSON API.
type apiHandler struct {
	registry *metrics.Registry
}

// newAPIHandler creates a new API handler.
func newAPIHandler(reg *metrics.Registry) *apiHandler {
	return &apiHandler{registry: reg}
}

// ServeHTTP routes API requests.
func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	path := strings.TrimPrefix(r.URL.Path, "/api")
	switch path {
	case "/overview":
		h.handleOverview(w, r)
	case "/providers":
		h.handleProviders(w, r)
	case "/models":
		h.handleModels(w, r)
	case "/latency":
		h.handleLatency(w, r)
	case "/costs":
		h.handleCosts(w, r)
	case "/errors":
		h.handleErrors(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "unknown endpoint")
	}
}

func (h *apiHandler) handleOverview(w http.ResponseWriter, _ *http.Request) {
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

	// Calculate average latency from histogram
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

	h.writeJSON(w, resp)
}

func (h *apiHandler) handleProviders(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	type providerAccum struct {
		requests     int64
		tokens       int64
		inputTokens  int64
		outputTokens int64
		costUSD      float64
		activeReqs   int64
		errors       int64
		latencySum   float64
		latencyCount uint64
	}

	provMap := make(map[string]*providerAccum)

	getProv := func(name string) *providerAccum {
		if p, ok := provMap[name]; ok {
			return p
		}
		p := &providerAccum{}
		provMap[name] = p
		return p
	}

	for _, cs := range snap.Counters {
		for _, s := range cs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			prov := s.LabelValues[0]
			p := getProv(prov)
			switch {
			case strings.HasSuffix(cs.Name, "_requests_total"):
				p.requests += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_tokens_total") && !strings.Contains(cs.Name, "input") && !strings.Contains(cs.Name, "output"):
				p.tokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_input_tokens_total"):
				p.inputTokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_output_tokens_total"):
				p.outputTokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_cost_usd_total"):
				p.costUSD += s.Value
			case strings.HasSuffix(cs.Name, "_errors_total"):
				p.errors += int64(s.Value)
			}
		}
	}

	for _, gs := range snap.Gauges {
		for _, s := range gs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			if strings.HasSuffix(gs.Name, "_active_requests") {
				getProv(s.LabelValues[0]).activeReqs += int64(s.Value)
			}
		}
	}

	for _, hs := range snap.Histograms {
		for _, s := range hs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			if strings.HasSuffix(hs.Name, "_request_duration_seconds") {
				p := getProv(s.LabelValues[0])
				p.latencySum += s.Sum
				p.latencyCount += s.Count
			}
		}
	}

	resp := ProviderResponse{}
	for name, p := range provMap {
		avgLatency := 0.0
		if p.latencyCount > 0 {
			avgLatency = (p.latencySum / float64(p.latencyCount)) * 1000
		}
		resp.Providers = append(resp.Providers, ProviderData{
			Name:           name,
			Requests:       p.requests,
			Tokens:         p.tokens,
			InputTokens:    p.inputTokens,
			OutputTokens:   p.outputTokens,
			CostUSD:        p.costUSD,
			ActiveRequests: p.activeReqs,
			Errors:         p.errors,
			AvgLatencyMS:   avgLatency,
		})
	}

	sort.Slice(resp.Providers, func(i, j int) bool {
		return resp.Providers[i].Name < resp.Providers[j].Name
	})

	h.writeJSON(w, resp)
}

func (h *apiHandler) handleModels(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	type modelKey struct {
		provider, model string
	}
	type modelAccum struct {
		requests     int64
		tokens       int64
		inputTokens  int64
		outputTokens int64
		costUSD      float64
		latencySum   float64
		latencyCount uint64
	}

	modelMap := make(map[modelKey]*modelAccum)

	getKey := func(provider, model string) modelKey {
		return modelKey{provider, model}
	}
	getModel := func(k modelKey) *modelAccum {
		if m, ok := modelMap[k]; ok {
			return m
		}
		m := &modelAccum{}
		modelMap[k] = m
		return m
	}

	for _, cs := range snap.Counters {
		// Only process counters with model as the second label
		if len(cs.Labels) < 2 || cs.Labels[1] != "model" {
			continue
		}
		for _, s := range cs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			k := getKey(s.LabelValues[0], s.LabelValues[1])
			m := getModel(k)
			switch {
			case strings.HasSuffix(cs.Name, "_requests_total"):
				m.requests += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_tokens_total") && !strings.Contains(cs.Name, "input") && !strings.Contains(cs.Name, "output"):
				m.tokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_input_tokens_total"):
				m.inputTokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_output_tokens_total"):
				m.outputTokens += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_cost_usd_total"):
				m.costUSD += s.Value
			}
		}
	}

	for _, hs := range snap.Histograms {
		for _, s := range hs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			if strings.HasSuffix(hs.Name, "_request_duration_seconds") {
				m := getModel(getKey(s.LabelValues[0], s.LabelValues[1]))
				m.latencySum += s.Sum
				m.latencyCount += s.Count
			}
		}
	}

	resp := ModelResponse{}
	for k, m := range modelMap {
		avgLatency := 0.0
		if m.latencyCount > 0 {
			avgLatency = (m.latencySum / float64(m.latencyCount)) * 1000
		}
		resp.Models = append(resp.Models, ModelData{
			Provider:     k.provider,
			Model:        k.model,
			Requests:     m.requests,
			Tokens:       m.tokens,
			InputTokens:  m.inputTokens,
			OutputTokens: m.outputTokens,
			CostUSD:      m.costUSD,
			AvgLatencyMS: avgLatency,
		})
	}

	sort.Slice(resp.Models, func(i, j int) bool {
		if resp.Models[i].Provider != resp.Models[j].Provider {
			return resp.Models[i].Provider < resp.Models[j].Provider
		}
		return resp.Models[i].Model < resp.Models[j].Model
	})

	h.writeJSON(w, resp)
}

func (h *apiHandler) handleLatency(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	resp := LatencyResponse{}
	for _, hs := range snap.Histograms {
		if !strings.HasSuffix(hs.Name, "_request_duration_seconds") {
			continue
		}
		for _, s := range hs.Samples {
			provider, model := "", ""
			if len(s.LabelValues) > 0 {
				provider = s.LabelValues[0]
			}
			if len(s.LabelValues) > 1 {
				model = s.LabelValues[1]
			}
			avgMS := 0.0
			if s.Count > 0 {
				avgMS = (s.Sum / float64(s.Count)) * 1000
			}
			buckets := make([]BucketPoint, 0, len(s.Buckets))
			for _, b := range s.Buckets {
				if math.IsInf(b.Upper, 1) {
					continue // skip +Inf bucket, not serializable to JSON
				}
				buckets = append(buckets, BucketPoint{
					UpperMS: b.Upper * 1000, // convert seconds to ms
					Count:   b.Count,
				})
			}
			resp.Providers = append(resp.Providers, LatencyDistribution{
				Provider: provider,
				Model:    model,
				Buckets:  buckets,
				Count:    s.Count,
				Sum:      s.Sum,
				AvgMS:    avgMS,
			})
		}
	}

	h.writeJSON(w, resp)
}

func (h *apiHandler) handleCosts(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	type costKey struct {
		provider, model string
	}

	costMap := make(map[costKey]struct {
		cost     float64
		requests int64
	})

	for _, cs := range snap.Counters {
		if !strings.HasSuffix(cs.Name, "_cost_usd_total") {
			continue
		}
		for _, s := range cs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			k := costKey{s.LabelValues[0], s.LabelValues[1]}
			v := costMap[k]
			v.cost += s.Value
			costMap[k] = v
		}
	}

	// Get request counts for avg cost calculation
	for _, cs := range snap.Counters {
		if !strings.HasSuffix(cs.Name, "_requests_total") {
			continue
		}
		for _, s := range cs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			k := costKey{s.LabelValues[0], s.LabelValues[1]}
			v := costMap[k]
			v.requests += int64(s.Value)
			costMap[k] = v
		}
	}

	resp := CostResponse{}
	for k, v := range costMap {
		avgCost := 0.0
		if v.requests > 0 {
			avgCost = v.cost / float64(v.requests)
		}
		resp.ByModel = append(resp.ByModel, CostByModel{
			Provider: k.provider,
			Model:    k.model,
			CostUSD:  v.cost,
			Requests: v.requests,
			AvgCost:  avgCost,
		})
		resp.TotalUSD += v.cost
	}

	sort.Slice(resp.ByModel, func(i, j int) bool {
		return resp.ByModel[i].CostUSD > resp.ByModel[j].CostUSD
	})

	h.writeJSON(w, resp)
}

func (h *apiHandler) handleErrors(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	resp := ErrorResponse{}
	typeMap := make(map[string]int64)
	provMap := make(map[string]int64)

	for _, cs := range snap.Counters {
		if !strings.HasSuffix(cs.Name, "_errors_total") {
			continue
		}
		for _, s := range cs.Samples {
			resp.TotalErrors += int64(s.Value)
			// Labels: [provider, error_type]
			if len(s.LabelValues) > 1 {
				typeMap[s.LabelValues[1]] += int64(s.Value)
			}
			if len(s.LabelValues) > 0 {
				provMap[s.LabelValues[0]] += int64(s.Value)
			}
		}
	}

	for t, c := range typeMap {
		resp.ByType = append(resp.ByType, ErrorByType{Type: t, Count: c})
	}
	sort.Slice(resp.ByType, func(i, j int) bool {
		return resp.ByType[i].Count > resp.ByType[j].Count
	})

	for p, c := range provMap {
		resp.ByProvider = append(resp.ByProvider, ErrorByProv{Provider: p, Count: c})
	}
	sort.Slice(resp.ByProvider, func(i, j int) bool {
		return resp.ByProvider[i].Count > resp.ByProvider[j].Count
	})

	h.writeJSON(w, resp)
}

func (h *apiHandler) writeJSON(w http.ResponseWriter, v interface{}) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		h.writeError(w, http.StatusInternalServerError, "encoding error")
	}
}

func (h *apiHandler) writeError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
