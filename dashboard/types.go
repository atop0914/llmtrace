package dashboard

import (
	"time"
)

// --- API Response Types ---

// OverviewResponse is the /api/overview response.
type OverviewResponse struct {
	TotalRequests  int64   `json:"total_requests"`
	TotalTokens    int64   `json:"total_tokens"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	ActiveRequests int64   `json:"active_requests"`
	TotalErrors    int64   `json:"total_errors"`
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
	ProviderCount  int     `json:"provider_count"`
	ModelCount     int     `json:"model_count"`
	Timestamp      string  `json:"timestamp"`
}

// ProviderResponse is the /api/providers response.
type ProviderResponse struct {
	Providers []ProviderData `json:"providers"`
}

// ProviderData holds metrics for a single provider.
type ProviderData struct {
	Name           string  `json:"name"`
	Requests       int64   `json:"requests"`
	Tokens         int64   `json:"tokens"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CostUSD        float64 `json:"cost_usd"`
	ActiveRequests int64   `json:"active_requests"`
	Errors         int64   `json:"errors"`
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
}

// ModelResponse is the /api/models response.
type ModelResponse struct {
	Models []ModelData `json:"models"`
}

// ModelData holds metrics for a single model.
type ModelData struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
}

// LatencyResponse is the /api/latency response.
type LatencyResponse struct {
	Providers []LatencyDistribution `json:"providers"`
}

// LatencyDistribution holds latency histogram for a provider+model.
type LatencyDistribution struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Buckets  []BucketPoint `json:"buckets"`
	Count    uint64        `json:"count"`
	Sum      float64       `json:"sum"`
	AvgMS    float64       `json:"avg_ms"`
}

// BucketPoint is a histogram bucket for JSON output.
type BucketPoint struct {
	UpperMS float64 `json:"upper_ms"`
	Count   uint64  `json:"count"`
}

// CostResponse is the /api/costs response.
type CostResponse struct {
	TotalUSD float64       `json:"total_usd"`
	ByModel  []CostByModel `json:"by_model"`
}

// CostByModel holds cost data for a single model.
type CostByModel struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	CostUSD  float64 `json:"cost_usd"`
	Requests int64   `json:"requests"`
	AvgCost  float64 `json:"avg_cost"`
}

// ErrorResponse is the /api/errors response.
type ErrorResponse struct {
	TotalErrors int64         `json:"total_errors"`
	ByType      []ErrorByType `json:"by_type"`
	ByProvider  []ErrorByProv `json:"by_provider"`
}

// ErrorByType holds error count by error category.
type ErrorByType struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// ErrorByProv holds error count by provider.
type ErrorByProv struct {
	Provider string `json:"provider"`
	Count    int64  `json:"count"`
}

// SSEEvent is a Server-Sent Event.
type SSEEvent struct {
	Type      string      `json:"type"` // "overview", "providers", etc.
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// TracesResponse is the /api/traces response.
type TracesResponse struct {
	Traces []TraceRecord `json:"traces"`
	Total  int           `json:"total"`
}

// ProviderHealthResponse is the /api/providers/health response.
type ProviderHealthResponse struct {
	Providers []ProviderHealth `json:"providers"`
}

// ProviderHealth holds health and efficiency metrics for a single provider.
type ProviderHealth struct {
	Name            string        `json:"name"`
	ErrorRate       float64       `json:"error_rate"`         // 0.0 - 1.0
	HealthScore     float64       `json:"health_score"`       // 0 - 100
	CostPer1KTokens float64       `json:"cost_per_1k_tokens"` // USD
	TokensPerSecond float64       `json:"tokens_per_second"`  // throughput
	LatencyP50      float64       `json:"latency_p50_ms"`
	LatencyP95      float64       `json:"latency_p95_ms"`
	LatencyP99      float64       `json:"latency_p99_ms"`
	TotalRequests   int64         `json:"total_requests"`
	TotalTokens     int64         `json:"total_tokens"`
	TotalCostUSD    float64       `json:"total_cost_usd"`
	Models          []ModelHealth `json:"models"`
	Status          string        `json:"status"` // "healthy", "degraded", "unhealthy"
}

// ModelHealth holds per-model health within a provider.
type ModelHealth struct {
	Model           string  `json:"model"`
	Requests        int64   `json:"requests"`
	ErrorRate       float64 `json:"error_rate"`
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	CostPer1KTokens float64 `json:"cost_per_1k_tokens"`
}

// ModelHealthResponse is the /api/models/health response.
type ModelHealthResponse struct {
	Models []ModelHealthDetail `json:"models"`
}

// ModelHealthDetail holds detailed health metrics for a single model.
type ModelHealthDetail struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Requests        int64   `json:"requests"`
	Errors          int64   `json:"errors"`
	ErrorRate       float64 `json:"error_rate"`   // 0.0 - 1.0
	HealthScore     float64 `json:"health_score"` // 0 - 100
	Tokens          int64   `json:"tokens"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	CostPer1KTokens float64 `json:"cost_per_1k_tokens"` // USD
	TokensPerSecond float64 `json:"tokens_per_second"`  // throughput
	LatencyP50      float64 `json:"latency_p50_ms"`
	LatencyP95      float64 `json:"latency_p95_ms"`
	LatencyP99      float64 `json:"latency_p99_ms"`
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	Status          string  `json:"status"` // "healthy", "degraded", "unhealthy"
}

// ModelCompareResponse is the /api/models/compare response.
type ModelCompareResponse struct {
	Models []ModelHealthDetail `json:"models"`
}

// ModelRankingResponse is the /api/models/rankings response.
type ModelRankingResponse struct {
	ByCostEfficiency []ModelRankingEntry `json:"by_cost_efficiency"`
	ByLatency        []ModelRankingEntry `json:"by_latency"`
	ByThroughput     []ModelRankingEntry `json:"by_throughput"`
	ByReliability    []ModelRankingEntry `json:"by_reliability"`
}

// ModelRankingEntry holds a model's ranking position.
type ModelRankingEntry struct {
	Rank     int     `json:"rank"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Value    float64 `json:"value"`
}
