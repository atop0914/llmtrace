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
	case "/models/health":
		h.handleModelHealth(w, r)
	case "/models/compare":
		h.handleModelCompare(w, r)
	case "/models/rankings":
		h.handleModelRankings(w, r)
	case "/latency":
		h.handleLatency(w, r)
	case "/costs":
		h.handleCosts(w, r)
	case "/costs/trend":
		h.handleCostTrend(w, r)
	case "/costs/breakdown":
		h.handleCostBreakdown(w, r)
	case "/errors":
		h.handleErrors(w, r)
	case "/errors/trend":
		h.handleErrorTrend(w, r)
	case "/errors/recent":
		h.handleErrorRecent(w, r)
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

// buildModelHealthDetail computes detailed health metrics for a model.
func (h *apiHandler) buildModelHealthDetail(provider, model string, m *modelMetricsAccum, traceLatencies []float64) ModelHealthDetail {
	// Error rate
	var errorRate float64
	if m.requests > 0 {
		errorRate = float64(m.errors) / float64(m.requests)
	}

	// Cost per 1K tokens
	var costPer1K float64
	if m.tokens > 0 {
		costPer1K = (m.costUSD / float64(m.tokens)) * 1000
	}

	// Throughput (tokens per second)
	var tokensPerSec float64
	if m.latencySum > 0 {
		tokensPerSec = float64(m.tokens) / m.latencySum
	}

	// Latency percentiles
	var p50, p95, p99 float64
	if len(traceLatencies) > 0 {
		sorted := make([]float64, len(traceLatencies))
		copy(sorted, traceLatencies)
		sort.Float64s(sorted)
		p50 = percentile(sorted, 50)
		p95 = percentile(sorted, 95)
		p99 = percentile(sorted, 99)
	}
	// Fallback: estimate from histogram average
	if p50 == 0 && m.latencyCount > 0 {
		avgMS := (m.latencySum / float64(m.latencyCount)) * 1000
		p50 = avgMS * 0.8
		p95 = avgMS * 1.8
		p99 = avgMS * 2.5
	}

	// Average latency
	var avgLatencyMS float64
	if m.latencyCount > 0 {
		avgLatencyMS = (m.latencySum / float64(m.latencyCount)) * 1000
	}

	// Health score: weighted composite
	healthScore := 100.0
	healthScore -= errorRate * 40 // error rate penalty (up to -40)
	if p95 > 5000 {               // >5s is bad
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

	return ModelHealthDetail{
		Provider:        provider,
		Model:           model,
		Requests:        m.requests,
		Errors:          m.errors,
		ErrorRate:       errorRate,
		HealthScore:     healthScore,
		Tokens:          m.tokens,
		InputTokens:     m.inputTokens,
		OutputTokens:    m.outputTokens,
		CostUSD:         m.costUSD,
		CostPer1KTokens: costPer1K,
		TokensPerSecond: tokensPerSec,
		LatencyP50:      p50,
		LatencyP95:      p95,
		LatencyP99:      p99,
		AvgLatencyMS:    avgLatencyMS,
		Status:          status,
	}
}

// modelKey represents a unique model identifier.
type modelKey struct{ provider, model string }

// modelMetricsAccum holds accumulated metrics for a single model.
type modelMetricsAccum struct {
	requests     int64
	tokens       int64
	inputTokens  int64
	outputTokens int64
	costUSD      float64
	errors       int64
	latencySum   float64
	latencyCount uint64
}

// collectModelMetrics gathers per-model metrics from the registry and trace store.
func (h *apiHandler) collectModelMetrics() (map[modelKey]*modelMetricsAccum, map[modelKey][]float64) {
	snap := h.registry.Collect()

	modelMap := make(map[modelKey]*modelMetricsAccum)

	getModel := func(provider, model string) *modelMetricsAccum {
		k := modelKey{provider, model}
		if m, ok := modelMap[k]; ok {
			return m
		}
		m := &modelMetricsAccum{}
		modelMap[k] = m
		return m
	}

	// Parse counters - only those with "model" as second label
	for _, cs := range snap.Counters {
		// Skip counters that don't have model as second label
		if len(cs.Labels) < 2 || cs.Labels[1] != "model" {
			continue
		}
		for _, s := range cs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			prov, modelName := s.LabelValues[0], s.LabelValues[1]
			m := getModel(prov, modelName)
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

	// Parse errors counter separately (has error_type as second label, not model)
	for _, cs := range snap.Counters {
		if !strings.HasSuffix(cs.Name, "_errors_total") {
			continue
		}
		for _, s := range cs.Samples {
			if len(s.LabelValues) < 1 {
				continue
			}
			// Errors are aggregated per provider, not per model
			// We'll distribute them evenly across models for now
			prov := s.LabelValues[0]
			// Find all models for this provider and add error count
			errorCount := int64(s.Value)
			var provModels []modelKey
			for k := range modelMap {
				if k.provider == prov {
					provModels = append(provModels, k)
				}
			}
			if len(provModels) > 0 {
				perModel := errorCount / int64(len(provModels))
				remainder := errorCount % int64(len(provModels))
				for i, k := range provModels {
					add := perModel
					if int64(i) < remainder {
						add++
					}
					modelMap[k].errors += add
				}
			}
		}
	}

	// Parse histograms
	for _, hs := range snap.Histograms {
		if !strings.HasSuffix(hs.Name, "_request_duration_seconds") {
			continue
		}
		for _, s := range hs.Samples {
			if len(s.LabelValues) < 2 {
				continue
			}
			m := getModel(s.LabelValues[0], s.LabelValues[1])
			m.latencySum += s.Sum
			m.latencyCount += s.Count
		}
	}

	// Collect trace latencies per model
	traceLatencies := make(map[modelKey][]float64)
	if h.traceStore != nil {
		allTraces := h.traceStore.Query(TraceQuery{Limit: 10000})
		for _, t := range allTraces {
			if t.LatencyMS > 0 {
				k := modelKey{t.Provider, t.Model}
				traceLatencies[k] = append(traceLatencies[k], t.LatencyMS)
			}
		}
	}

	return modelMap, traceLatencies
}

// handleModelHealth returns detailed health metrics for all models.
func (h *apiHandler) handleModelHealth(w http.ResponseWriter, _ *http.Request) {
	modelMap, traceLatencies := h.collectModelMetrics()

	resp := ModelHealthResponse{}
	for k, m := range modelMap {
		lats := traceLatencies[k]
		detail := h.buildModelHealthDetail(k.provider, k.model, m, lats)
		resp.Models = append(resp.Models, detail)
	}

	sort.Slice(resp.Models, func(i, j int) bool {
		if resp.Models[i].Provider != resp.Models[j].Provider {
			return resp.Models[i].Provider < resp.Models[j].Provider
		}
		return resp.Models[i].Model < resp.Models[j].Model
	})

	h.writeJSON(w, resp)
}

// handleModelCompare compares specific models side by side.
// Query param: models=provider1:model1,provider2:model2
func (h *apiHandler) handleModelCompare(w http.ResponseWriter, r *http.Request) {
	modelsParam := r.URL.Query().Get("models")
	if modelsParam == "" {
		h.writeError(w, http.StatusBadRequest, "models query parameter required (format: provider:model,provider:model)")
		return
	}

	// Parse requested models
	type modelRef struct{ provider, model string }
	var requested []modelRef
	for _, part := range strings.Split(modelsParam, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pieces := strings.SplitN(part, ":", 2)
		if len(pieces) != 2 {
			continue
		}
		requested = append(requested, modelRef{pieces[0], pieces[1]})
	}

	if len(requested) == 0 {
		h.writeError(w, http.StatusBadRequest, "no valid models specified")
		return
	}

	modelMap, traceLatencies := h.collectModelMetrics()

	resp := ModelCompareResponse{}
	for _, ref := range requested {
		k := modelKey(ref)
		m, ok := modelMap[k]
		if !ok {
			// Model not found, return empty metrics
			m = &modelMetricsAccum{}
		}
		lats := traceLatencies[k]
		detail := h.buildModelHealthDetail(ref.provider, ref.model, m, lats)
		resp.Models = append(resp.Models, detail)
	}

	h.writeJSON(w, resp)
}

// handleModelRankings returns model rankings by different metrics.
func (h *apiHandler) handleModelRankings(w http.ResponseWriter, _ *http.Request) {
	modelMap, traceLatencies := h.collectModelMetrics()

	type rankedModel struct {
		provider, model string
		value           float64
	}

	// Build list of models with health details
	var models []ModelHealthDetail
	for k, m := range modelMap {
		lats := traceLatencies[k]
		detail := h.buildModelHealthDetail(k.provider, k.model, m, lats)
		models = append(models, detail)
	}

	resp := ModelRankingResponse{}

	// By cost efficiency (lowest cost per 1K tokens)
	var costRanked []rankedModel
	for _, m := range models {
		if m.CostPer1KTokens > 0 {
			costRanked = append(costRanked, rankedModel{m.Provider, m.Model, m.CostPer1KTokens})
		}
	}
	sort.Slice(costRanked, func(i, j int) bool { return costRanked[i].value < costRanked[j].value })
	for i, r := range costRanked {
		resp.ByCostEfficiency = append(resp.ByCostEfficiency, ModelRankingEntry{i + 1, r.provider, r.model, r.value})
	}

	// By latency (lowest p50)
	var latencyRanked []rankedModel
	for _, m := range models {
		if m.LatencyP50 > 0 {
			latencyRanked = append(latencyRanked, rankedModel{m.Provider, m.Model, m.LatencyP50})
		}
	}
	sort.Slice(latencyRanked, func(i, j int) bool { return latencyRanked[i].value < latencyRanked[j].value })
	for i, r := range latencyRanked {
		resp.ByLatency = append(resp.ByLatency, ModelRankingEntry{i + 1, r.provider, r.model, r.value})
	}

	// By throughput (highest tokens per second)
	var throughputRanked []rankedModel
	for _, m := range models {
		if m.TokensPerSecond > 0 {
			throughputRanked = append(throughputRanked, rankedModel{m.Provider, m.Model, m.TokensPerSecond})
		}
	}
	sort.Slice(throughputRanked, func(i, j int) bool { return throughputRanked[i].value > throughputRanked[j].value })
	for i, r := range throughputRanked {
		resp.ByThroughput = append(resp.ByThroughput, ModelRankingEntry{i + 1, r.provider, r.model, r.value})
	}

	// By reliability (lowest error rate)
	var reliabilityRanked []rankedModel
	for _, m := range models {
		reliabilityRanked = append(reliabilityRanked, rankedModel{m.Provider, m.Model, m.ErrorRate})
	}
	sort.Slice(reliabilityRanked, func(i, j int) bool { return reliabilityRanked[i].value < reliabilityRanked[j].value })
	for i, r := range reliabilityRanked {
		resp.ByReliability = append(resp.ByReliability, ModelRankingEntry{i + 1, r.provider, r.model, r.value})
	}

	h.writeJSON(w, resp)
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
			p50 = avgMS * 0.8 // approximate
			p95 = avgMS * 1.8
			p99 = avgMS * 2.5
		}

		// Health score: weighted composite
		healthScore := 100.0
		healthScore -= errorRate * 40 // error rate penalty (up to -40)
		if p95 > 5000 {               // >5s is bad
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

// handleCostTrend returns daily cost breakdown from trace data.
func (h *apiHandler) handleCostTrend(w http.ResponseWriter, _ *http.Request) {
	resp := CostTrendResponse{}

	if h.traceStore == nil {
		h.writeJSON(w, resp)
		return
	}

	traces := h.traceStore.Query(TraceQuery{Limit: 10000})

	// Group by date
	type dayData struct {
		cost     float64
		requests int64
		tokens   int64
	}
	dayMap := make(map[string]*dayData)

	for _, t := range traces {
		date := t.StartedAt.Format("2006-01-02")
		if _, ok := dayMap[date]; !ok {
			dayMap[date] = &dayData{}
		}
		d := dayMap[date]
		d.cost += t.CostUSD
		d.requests++
		d.tokens += int64(t.TotalTokens)
	}

	// Convert to sorted slice
	for date, d := range dayMap {
		resp.Daily = append(resp.Daily, CostTrendPoint{
			Date:     date,
			CostUSD:  d.cost,
			Requests: d.requests,
			Tokens:   d.tokens,
		})
		resp.TotalUSD += d.cost
	}

	sort.Slice(resp.Daily, func(i, j int) bool {
		return resp.Daily[i].Date < resp.Daily[j].Date
	})

	resp.Days = len(resp.Daily)
	if resp.Days > 0 {
		resp.AvgPerDay = resp.TotalUSD / float64(resp.Days)
	}

	h.writeJSON(w, resp)
}

// handleCostBreakdown returns cost breakdown by provider.
func (h *apiHandler) handleCostBreakdown(w http.ResponseWriter, _ *http.Request) {
	snap := h.registry.Collect()

	type provAccum struct {
		cost     float64
		requests int64
		tokens   int64
	}

	provMap := make(map[string]*provAccum)

	for _, cs := range snap.Counters {
		for _, s := range cs.Samples {
			if len(s.LabelValues) == 0 {
				continue
			}
			prov := s.LabelValues[0]
			if _, ok := provMap[prov]; !ok {
				provMap[prov] = &provAccum{}
			}
			p := provMap[prov]
			switch {
			case strings.HasSuffix(cs.Name, "_cost_usd_total"):
				p.cost += s.Value
			case strings.HasSuffix(cs.Name, "_requests_total"):
				p.requests += int64(s.Value)
			case strings.HasSuffix(cs.Name, "_tokens_total") && !strings.Contains(cs.Name, "input") && !strings.Contains(cs.Name, "output"):
				p.tokens += int64(s.Value)
			}
		}
	}

	resp := CostBreakdownResponse{}
	for name, p := range provMap {
		resp.TotalUSD += p.cost
		resp.Providers = append(resp.Providers, CostByProvider{
			Provider: name,
			CostUSD:  p.cost,
			Requests: p.requests,
			Tokens:   p.tokens,
		})
	}

	// Calculate percentages and cost per 1K
	for i := range resp.Providers {
		if resp.TotalUSD > 0 {
			resp.Providers[i].Percentage = resp.Providers[i].CostUSD / resp.TotalUSD * 100
		}
		if resp.Providers[i].Tokens > 0 {
			resp.Providers[i].CostPer1K = (resp.Providers[i].CostUSD / float64(resp.Providers[i].Tokens)) * 1000
		}
	}

	sort.Slice(resp.Providers, func(i, j int) bool {
		return resp.Providers[i].CostUSD > resp.Providers[j].CostUSD
	})

	h.writeJSON(w, resp)
}

// handleErrorTrend returns daily error rate from trace data.
func (h *apiHandler) handleErrorTrend(w http.ResponseWriter, _ *http.Request) {
	resp := ErrorTrendResponse{}

	if h.traceStore == nil {
		h.writeJSON(w, resp)
		return
	}

	traces := h.traceStore.Query(TraceQuery{Limit: 10000})

	type dayData struct {
		errors   int64
		requests int64
	}
	dayMap := make(map[string]*dayData)

	for _, t := range traces {
		date := t.StartedAt.Format("2006-01-02")
		if _, ok := dayMap[date]; !ok {
			dayMap[date] = &dayData{}
		}
		d := dayMap[date]
		d.requests++
		if t.Status == "error" {
			d.errors++
		}
	}

	for date, d := range dayMap {
		errorRate := 0.0
		if d.requests > 0 {
			errorRate = float64(d.errors) / float64(d.requests)
		}
		resp.Daily = append(resp.Daily, ErrorTrendPoint{
			Date:      date,
			Errors:    d.errors,
			Requests:  d.requests,
			ErrorRate: errorRate,
		})
		resp.TotalErrors += d.errors
		resp.TotalRequests += d.requests
	}

	sort.Slice(resp.Daily, func(i, j int) bool {
		return resp.Daily[i].Date < resp.Daily[j].Date
	})

	resp.Days = len(resp.Daily)
	if resp.TotalRequests > 0 {
		resp.AvgErrorRate = float64(resp.TotalErrors) / float64(resp.TotalRequests)
	}

	h.writeJSON(w, resp)
}

// handleErrorRecent returns recent error traces.
func (h *apiHandler) handleErrorRecent(w http.ResponseWriter, _ *http.Request) {
	resp := ErrorRecentResponse{}

	if h.traceStore == nil {
		h.writeJSON(w, resp)
		return
	}

	// Query recent error traces
	traces := h.traceStore.Query(TraceQuery{
		Status:   "error",
		Limit:    20,
		SortDesc: true,
	})

	for _, t := range traces {
		// Derive error type from error message
		errType := "unknown"
		if strings.Contains(t.Error, "rate_limit") || strings.Contains(t.Error, "429") {
			errType = "rate_limit"
		} else if strings.Contains(t.Error, "timeout") || strings.Contains(t.Error, "deadline") {
			errType = "timeout"
		} else if strings.Contains(t.Error, "context_length") || strings.Contains(t.Error, "too long") {
			errType = "context_length"
		} else if strings.Contains(t.Error, "auth") || strings.Contains(t.Error, "401") || strings.Contains(t.Error, "403") {
			errType = "auth_error"
		} else if strings.Contains(t.Error, "500") || strings.Contains(t.Error, "502") || strings.Contains(t.Error, "503") {
			errType = "server_error"
		} else if t.Error != "" {
			errType = "api_error"
		}

		resp.Errors = append(resp.Errors, ErrorRecentEntry{
			ID:        t.ID,
			Timestamp: t.StartedAt.Format(time.RFC3339),
			Provider:  t.Provider,
			Model:     t.Model,
			ErrorType: errType,
			ErrorMsg:  t.Error,
			LatencyMS: t.LatencyMS,
		})
	}

	resp.Total = len(resp.Errors)

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
