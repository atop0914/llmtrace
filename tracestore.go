// TraceStore provides in-memory storage for individual LLM trace records.
//
// Unlike the aggregated metrics in the metrics package, TraceStore captures
// each individual call with its full context (model, provider, tokens, cost,
// latency, status, timestamps). This enables:
//   - Querying recent traces by provider, model, or status
//   - Debugging individual requests
//   - Building a traces view in the dashboard
//
// The store uses a fixed-capacity ring buffer with automatic eviction of
// oldest records when full. All operations are thread-safe.
//
// Usage:
//
//	store := llmtrace.NewTraceStore(llmtrace.TraceStoreConfig{MaxSize: 10000})
//
//	// Use as middleware
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(store.Middleware()),
//	)
//
//	// Query traces
//	traces := store.Query(llmtrace.TraceQuery{
//	    Provider: "openai",
//	    Since:    time.Now().Add(-1 * time.Hour),
//	})
package llmtrace

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// TraceRecord represents a single LLM call trace.
type TraceRecord struct {
	// ID is a unique identifier for this trace.
	ID string `json:"id"`

	// Provider is the LLM provider name (e.g. "openai", "anthropic").
	Provider string `json:"provider"`

	// Model is the model identifier (e.g. "gpt-4o", "claude-3-opus").
	Model string `json:"model"`

	// Status is "success" or "error".
	Status string `json:"status"`

	// Error is the error message if the call failed.
	Error string `json:"error,omitempty"`

	// InputTokens is the number of input tokens.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the number of output tokens.
	OutputTokens int `json:"output_tokens"`

	// TotalTokens is the total token count.
	TotalTokens int `json:"total_tokens"`

	// CostUSD is the calculated cost in USD.
	CostUSD float64 `json:"cost_usd"`

	// LatencyMS is the request latency in milliseconds.
	LatencyMS float64 `json:"latency_ms"`

	// MessageCount is the number of messages in the request.
	MessageCount int `json:"message_count"`

	// MaxTokens is the configured max_tokens parameter (0 if unset).
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature is the configured temperature parameter (0 if unset).
	Temperature float64 `json:"temperature,omitempty"`

	// ResponseID is the provider's response ID.
	ResponseID string `json:"response_id,omitempty"`

	// FinishReason indicates why generation stopped.
	FinishReason string `json:"finish_reason,omitempty"`

	// Stream is true if this was a streaming request.
	Stream bool `json:"stream"`

	// StartedAt is when the request started.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when the response was received.
	CompletedAt time.Time `json:"completed_at"`
}

// TraceQuery defines filters for querying traces.
type TraceQuery struct {
	// Provider filters by provider name (exact match, empty = all).
	Provider string

	// Model filters by model name (exact match, empty = all).
	Model string

	// Status filters by status ("success" or "error", empty = all).
	Status string

	// Since filters traces started after this time (zero value = no filter).
	Since time.Time

	// Until filters traces started before this time (zero value = no filter).
	Until time.Time

	// Limit is the maximum number of traces to return (0 = no limit).
	Limit int

	// SortDesc reverses the default chronological order (newest first).
	SortDesc bool
}

// TraceStoreConfig configures a TraceStore.
type TraceStoreConfig struct {
	// MaxSize is the maximum number of traces to retain.
	// Older traces are evicted when this limit is reached.
	// Default: 10000.
	MaxSize int

	// CostCalc is an optional CostCalculator for computing trace costs.
	// If nil, cost will be 0.
	CostCalc *CostCalculator
}

// TraceStore is a thread-safe in-memory store for LLM trace records.
type TraceStore struct {
	mu      sync.RWMutex
	records []TraceRecord
	maxSize int
	head    int // next write position
	count   int // number of valid records
	nextID  int64

	costCalc *CostCalculator
}

// NewTraceStore creates a new TraceStore with the given configuration.
func NewTraceStore(cfg TraceStoreConfig) *TraceStore {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10000
	}
	return &TraceStore{
		records:  make([]TraceRecord, cfg.MaxSize),
		maxSize:  cfg.MaxSize,
		costCalc: cfg.CostCalc,
	}
}

// Add inserts a trace record into the store.
// If the store is at capacity, the oldest record is evicted.
func (s *TraceStore) Add(rec TraceRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	if rec.ID == "" {
		rec.ID = formatTraceID(s.nextID)
	}

	s.records[s.head] = rec
	s.head = (s.head + 1) % s.maxSize
	if s.count < s.maxSize {
		s.count++
	}
}

// Len returns the number of records currently in the store.
func (s *TraceStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// Query returns traces matching the given filters.
// Results are returned in chronological order (oldest first) by default.
// Set Query.SortDesc for newest first.
func (s *TraceStore) Query(q TraceQuery) []TraceRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]TraceRecord, 0, s.count)

	// Iterate from oldest to newest
	for i := 0; i < s.count; i++ {
		idx := (s.head - s.count + i + s.maxSize) % s.maxSize
		rec := s.records[idx]

		if !matchTrace(&rec, q) {
			continue
		}

		results = append(results, rec)
	}

	if q.SortDesc {
		sort.Slice(results, func(i, j int) bool {
			return results[i].StartedAt.After(results[j].StartedAt)
		})
	}

	if q.Limit > 0 && len(results) > q.Limit {
		if q.SortDesc {
			results = results[:q.Limit]
		} else {
			results = results[len(results)-q.Limit:]
		}
	}

	return results
}

// All returns all traces in chronological order (oldest first).
func (s *TraceStore) All() []TraceRecord {
	return s.Query(TraceQuery{})
}

// Clear removes all records from the store.
func (s *TraceStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = 0
	s.head = 0
	s.nextID = 0
}

// TraceSummary returns aggregate statistics for the stored traces.
func (s *TraceStore) TraceSummary() TraceSummaryResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := TraceSummaryResult{
		Providers: make(map[string]int),
		Models:    make(map[string]int),
	}

	for i := 0; i < s.count; i++ {
		idx := (s.head - s.count + i + s.maxSize) % s.maxSize
		rec := s.records[idx]

		result.TotalTraces++
		result.TotalTokens += rec.TotalTokens
		result.TotalInputTokens += rec.InputTokens
		result.TotalOutputTokens += rec.OutputTokens
		result.TotalCostUSD += rec.CostUSD
		result.LatencySum += rec.LatencyMS

		if rec.Status == "error" {
			result.TotalErrors++
		}

		result.Providers[rec.Provider]++
		result.Models[rec.Model]++

		if rec.LatencyMS < result.MinLatencyMS || result.MinLatencyMS == 0 {
			result.MinLatencyMS = rec.LatencyMS
		}
		if rec.LatencyMS > result.MaxLatencyMS {
			result.MaxLatencyMS = rec.LatencyMS
		}
	}

	if result.TotalTraces > 0 {
		result.AvgLatencyMS = result.LatencySum / float64(result.TotalTraces)
	}

	return result
}

// TraceSummaryResult holds aggregate statistics for traces.
type TraceSummaryResult struct {
	TotalTraces       int            `json:"total_traces"`
	TotalTokens       int            `json:"total_tokens"`
	TotalInputTokens  int            `json:"total_input_tokens"`
	TotalOutputTokens int            `json:"total_output_tokens"`
	TotalCostUSD      float64        `json:"total_cost_usd"`
	TotalErrors       int            `json:"total_errors"`
	AvgLatencyMS      float64        `json:"avg_latency_ms"`
	MinLatencyMS      float64        `json:"min_latency_ms"`
	MaxLatencyMS      float64        `json:"max_latency_ms"`
	LatencySum        float64        `json:"-"`
	Providers         map[string]int `json:"providers"`
	Models            map[string]int `json:"models"`
}

// Middleware returns a Middleware that captures trace records for each call.
func (s *TraceStore) Middleware() Middleware {
	return func(next CompleteFunc) CompleteFunc {
		return func(ctx context.Context, req *Request) (*Response, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			elapsed := time.Since(start)

			rec := TraceRecord{
				Provider:     "unknown",
				Model:        req.Model,
				MessageCount: len(req.Messages),
				Stream:       false,
				StartedAt:    start,
				CompletedAt:  start.Add(elapsed),
				LatencyMS:    float64(elapsed.Milliseconds()),
			}

			if req.Temperature != nil {
				rec.Temperature = *req.Temperature
			}
			if req.MaxTokens != nil {
				rec.MaxTokens = *req.MaxTokens
			}

			if err != nil {
				rec.Status = "error"
				rec.Error = err.Error()
			} else {
				rec.Status = "success"
				rec.Provider = resp.Provider
				rec.Model = resp.Model
				rec.InputTokens = resp.Usage.InputTokens
				rec.OutputTokens = resp.Usage.OutputTokens
				rec.TotalTokens = resp.Usage.TotalTokens
				rec.ResponseID = resp.ID
				rec.FinishReason = resp.FinishReason

				if s.costCalc != nil {
					rec.CostUSD = s.costCalc.Calculate(resp.Model, resp.Usage)
				}
			}

			s.Add(rec)
			return resp, err
		}
	}
}

// StreamMiddleware returns a StreamMiddleware that captures trace records for streaming calls.
func (s *TraceStore) StreamMiddleware() StreamMiddleware {
	return func(next StreamFunc) StreamFunc {
		return func(ctx context.Context, req *Request) (<-chan StreamChunk, error) {
			start := time.Now()
			ch, err := next(ctx, req)
			if err != nil {
				rec := TraceRecord{
					Provider:     "unknown",
					Model:        req.Model,
					Status:       "error",
					Error:        err.Error(),
					MessageCount: len(req.Messages),
					Stream:       true,
					StartedAt:    start,
					CompletedAt:  time.Now(),
					LatencyMS:    float64(time.Since(start).Milliseconds()),
				}
				s.Add(rec)
				return nil, err
			}

			out := make(chan StreamChunk)
			go func() {
				defer close(out)
				var lastUsage *Usage
				var hasError bool
				var errorMsg string
				var content strings.Builder

				for chunk := range ch {
					if chunk.Error != nil {
						hasError = true
						errorMsg = chunk.Error.Error()
					}
					if chunk.Content != "" {
						content.WriteString(chunk.Content)
					}
					if chunk.Usage != nil {
						lastUsage = chunk.Usage
					}
					out <- chunk
				}

				end := time.Now()
				rec := TraceRecord{
					Provider:     "unknown",
					Model:        req.Model,
					MessageCount: len(req.Messages),
					Stream:       true,
					StartedAt:    start,
					CompletedAt:  end,
					LatencyMS:    float64(end.Sub(start).Milliseconds()),
				}

				if req.Temperature != nil {
					rec.Temperature = *req.Temperature
				}
				if req.MaxTokens != nil {
					rec.MaxTokens = *req.MaxTokens
				}

				if hasError {
					rec.Status = "error"
					rec.Error = errorMsg
				} else {
					rec.Status = "success"
				}

				if lastUsage != nil {
					rec.InputTokens = lastUsage.InputTokens
					rec.OutputTokens = lastUsage.OutputTokens
					rec.TotalTokens = lastUsage.TotalTokens

					if s.costCalc != nil {
						rec.CostUSD = s.costCalc.Calculate(req.Model, *lastUsage)
					}
				}

				s.Add(rec)
			}()

			return out, nil
		}
	}
}

// matchTrace checks if a record matches the query filters.
func matchTrace(rec *TraceRecord, q TraceQuery) bool {
	if q.Provider != "" && rec.Provider != q.Provider {
		return false
	}
	if q.Model != "" && rec.Model != q.Model {
		return false
	}
	if q.Status != "" && rec.Status != q.Status {
		return false
	}
	if !q.Since.IsZero() && rec.StartedAt.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && rec.StartedAt.After(q.Until) {
		return false
	}
	return true
}

// formatTraceID generates a trace ID string from a sequence number.
func formatTraceID(n int64) string {
	// Simple hex-like ID: trace-000001
	const prefix = "trace-"
	buf := make([]byte, len(prefix)+8)
	copy(buf, prefix)
	for i := 7; i >= 0; i-- {
		d := n & 0xf
		if d < 10 {
			buf[len(prefix)+i] = byte('0' + d)
		} else {
			buf[len(prefix)+i] = byte('a' + d - 10)
		}
		n >>= 4
	}
	return string(buf)
}
