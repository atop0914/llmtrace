package dashboard

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atop0914/llmtrace/metrics"
)

// apiHandler serves the dashboard JSON API.
type apiHandler struct {
	registry   *metrics.Registry
	traceStore TraceStorer
}

// newAPIHandler creates a new API handler.
func newAPIHandler(reg *metrics.Registry, ts TraceStorer) *apiHandler {
	return &apiHandler{registry: reg, traceStore: ts}
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
	case "/providers/health":
		h.handleProviderHealth(w, r)
	case "/models":
		h.handleModels(w, r)
	case "/latency":
		h.handleLatency(w, r)
	case "/costs":
		h.handleCosts(w, r)
	case "/errors":
		h.handleErrors(w, r)
	case "/traces":
		h.handleTraces(w, r)
	case "/traces/summary":
		h.handleTraceSummary(w, r)
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

func (h *apiHandler) handleTraces(w http.ResponseWriter, r *http.Request) {
	if h.traceStore == nil {
		h.writeJSON(w, TracesResponse{Traces: []TraceRecord{}})
		return
	}

	q := TraceQuery{
		SortDesc: true, // newest first by default
	}

	params := r.URL.Query()
	if v := params.Get("provider"); v != "" {
		q.Provider = v
	}
	if v := params.Get("model"); v != "" {
		q.Model = v
	}
	if v := params.Get("status"); v != "" {
		q.Status = v
	}
	if v := params.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if v := params.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.Since = t
		}
	}
	if v := params.Get("sort"); v == "asc" {
		q.SortDesc = false
	}

	traces := h.traceStore.Query(q)
	if traces == nil {
		traces = []TraceRecord{}
	}

	h.writeJSON(w, TracesResponse{
		Traces: traces,
		Total:  h.traceStore.Len(),
	})
}

func (h *apiHandler) handleTraceSummary(w http.ResponseWriter, _ *http.Request) {
	if h.traceStore == nil {
		h.writeJSON(w, TraceSummaryResult{})
		return
	}
	h.writeJSON(w, h.traceStore.TraceSummary())
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

func (h *apiHandler) handleProviderHealth(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	// Build provider-level data from metrics
	type modelHealthAccum struct {
		requests     int64
		tokens       int64
		inputTokens  int64
		outputTokens int64
		costUSD      float64
		errors       int64
		latencySum   float64
		latencyCount uint64
	}

	type provData struct {
		requests       int64
		tokens         int64
		inputTokens    int64
		outputTokens   int64
		costUSD        float64
		errors         int64
		latencySum     float64
		latencyCount   uint64
		latencySamples []float64 // for percentile calc from histogram buckets
		models         map[string]*modelHealthAccum
	}

	provMap := make(map[string]*provData)
	getProv := func(name string) *provData {
		if p, ok := provMap[name]; ok {
			return p
		}
		p := &provData{models: make(map[string]*modelHealthAccum)}
		provMap[name] = p
		return p
	}
	getModel := func(p *provData, name string) *modelHealthAccum {
		if m, ok := p.models[name]; ok {
			return m
		}
		m := &modelHealthAccum{}
		p.models[name] = m
		return m
	}

	// Parse counters
	for _, cs := range snap.Counters {
		for _, s := range cs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			prov := s.LabelValues[0]
			p := getProv(prov)
			var m *modelHealthAccum
			if len(s.LabelValues) > 1 {
				m = getModel(p, s.LabelValues[1])
			}
			switch {
			case strings.HasSuffix(cs.Name, "_requests_total"):
				p.requests += int64(s.Value)
				if m != nil {
					m.requests += int64(s.Value)
				}
			case strings.HasSuffix(cs.Name, "_tokens_total") && !strings.Contains(cs.Name, "input") && !strings.Contains(cs.Name, "output"):
				p.tokens += int64(s.Value)
				if m != nil {
					m.tokens += int64(s.Value)
				}
			case strings.HasSuffix(cs.Name, "_input_tokens_total"):
				p.inputTokens += int64(s.Value)
				if m != nil {
					m.inputTokens += int64(s.Value)
				}
			case strings.HasSuffix(cs.Name, "_output_tokens_total"):
				p.outputTokens += int64(s.Value)
				if m != nil {
					m.outputTokens += int64(s.Value)
				}
			case strings.HasSuffix(cs.Name, "_cost_usd_total"):
				p.costUSD += s.Value
				if m != nil {
					m.costUSD += s.Value
				}
			case strings.HasSuffix(cs.Name, "_errors_total"):
				p.errors += int64(s.Value)
				if m != nil {
					m.errors += int64(s.Value)
				}
			}
		}
	}

	// Parse histograms for latency percentiles
	for _, hs := range snap.Histograms {
		if !strings.HasSuffix(hs.Name, "_request_duration_seconds") {
			continue
		}
		for _, s := range hs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			p := getProv(s.LabelValues[0])
			p.latencySum += s.Sum
			p.latencyCount += s.Count
			// Store cumulative counts for percentile estimation
			for _, b := range s.Buckets {
				if !math.IsInf(b.Upper, 1) {
					p.latencySamples = append(p.latencySamples, b.Upper*1000) // convert to ms
				}
			}
		}
	}

	// Also compute per-provider latency from traces if available
	var traceLatencies map[string][]float64
	if h.traceStore != nil {
		traceLatencies = make(map[string][]float64)
		allTraces := h.traceStore.Query(TraceQuery{Limit: 10000})
		for _, t := range allTraces {
			if t.LatencyMS > 0 {
				traceLatencies[t.Provider] = append(traceLatencies[t.Provider], t.LatencyMS)
			}
		}
	}

	resp := ProviderHealthResponse{}
	for name, p := range provMap {
		// Error rate
		var errorRate float64
		if p.requests > 0 {
			errorRate = float64(p.errors) / float64(p.requests)
		}

		// Cost per 1K tokens
		var costPer1K float64
		if p.tokens > 0 {
			costPer1K = (p.costUSD / float64(p.tokens)) * 1000
		}

		// Throughput (tokens per second) from latency data
		var tokensPerSec float64
		if p.latencySum > 0 {
			tokensPerSec = float64(p.tokens) / p.latencySum
		}

		// Latency percentiles from trace data (more accurate than histogram buckets)
		var p50, p95, p99 float64
		if traceLatencies != nil {
			if lats, ok := traceLatencies[name]; ok && len(lats) > 0 {
				sorted := make([]float64, len(lats))
				copy(sorted, lats)
				sort.Float64s(sorted)
				p50 = percentile(sorted, 50)
				p95 = percentile(sorted, 95)
				p99 = percentile(sorted, 99)
			}
		}
		// Fallback: estimate from histogram average
		if p50 == 0 && p.latencyCount > 0 {
			avgMS := (p.latencySum / float64(p.latencyCount)) * 1000
			p50 = avgMS * 0.8  // approximate
			p95 = avgMS * 1.8
			p99 = avgMS * 2.5
		}

		// Health score: weighted composite
		healthScore := 100.0
		healthScore -= errorRate * 40            // error rate penalty (up to -40)
		if p95 > 5000 {                         // >5s is bad
			healthScore -= math.Min(30, (p95-5000)/100)
		}
		healthScore = math.Max(0, math.Min(100, healthScore))

		// Status label
		status := "healthy"
		if healthScore < 70 {
			status = "degraded"
		}
		if healthScore < 40 {
			status = "unhealthy"
		}

		// Build per-model health
		var models []ModelHealth
		for modelName, m := range p.models {
			var mErrorRate float64
			if m.requests > 0 {
				mErrorRate = float64(m.errors) / float64(m.requests)
			}
			var mCostPer1K float64
			if m.tokens > 0 {
				mCostPer1K = (m.costUSD / float64(m.tokens)) * 1000
			}
			var mAvgLatency float64
			if m.latencyCount > 0 {
				mAvgLatency = (m.latencySum / float64(m.latencyCount)) * 1000
			}
			models = append(models, ModelHealth{
				Model:           modelName,
				Requests:        m.requests,
				ErrorRate:       mErrorRate,
				AvgLatencyMS:    mAvgLatency,
				CostPer1KTokens: mCostPer1K,
			})
		}
		sort.Slice(models, func(i, j int) bool {
			return models[i].Requests > models[j].Requests
		})

		resp.Providers = append(resp.Providers, ProviderHealth{
			Name:            name,
			ErrorRate:       errorRate,
			HealthScore:     healthScore,
			CostPer1KTokens: costPer1K,
			TokensPerSecond: tokensPerSec,
			LatencyP50:      p50,
			LatencyP95:      p95,
			LatencyP99:      p99,
			TotalRequests:   p.requests,
			TotalTokens:     p.tokens,
			TotalCostUSD:    p.costUSD,
			Models:          models,
			Status:          status,
		})
	}

	sort.Slice(resp.Providers, func(i, j int) bool {
		return resp.Providers[i].Name < resp.Providers[j].Name
	})

	h.writeJSON(w, resp)
}

// percentile computes the p-th percentile from a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100 * float64(len(sorted)-1)
	lower := int(idx)
	if lower >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower] + frac*(sorted[lower+1]-sorted[lower])
}