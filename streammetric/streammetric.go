// Package streammetric provides real-time metrics collection for streaming LLM responses.
//
// It tracks key performance indicators for streaming:
//   - Time to First Token (TTFT): latency from request start to first chunk
//   - Inter-Chunk Latency (ICL): time between consecutive chunks
//   - Tokens Per Second (TPS): throughput of the stream
//   - Total stream duration and chunk count
//
// Usage:
//
//	collector := streammetric.NewCollector()
//	wrappedCh := collector.Wrap(ch)
//	for chunk := range wrappedCh {
//	    // process chunk normally
//	}
//	metrics := collector.Metrics()
//	fmt.Printf("TTFT: %v, TPS: %.1f\n", metrics.TTFT, metrics.TokensPerSecond)
package streammetric

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/atop0914/llmtrace"
)

// Metrics holds streaming performance metrics collected from a response stream.
type Metrics struct {
	// TTFT is the Time to First Token — duration from stream start to first chunk.
	TTFT time.Duration

	// TotalDuration is the total time from stream start to last chunk.
	TotalDuration time.Duration

	// ChunkCount is the number of chunks received.
	ChunkCount int

	// TokensPerSecond is the output token throughput (tokens / total duration).
	TokensPerSecond float64

	// AvgInterChunkLatency is the average time between consecutive chunks.
	AvgInterChunkLatency time.Duration

	// MaxInterChunkLatency is the maximum time between consecutive chunks.
	MaxInterChunkLatency time.Duration

	// MinInterChunkLatency is the minimum time between consecutive chunks.
	MinInterChunkLatency time.Duration

	// P50InterChunkLatency is the median inter-chunk latency.
	P50InterChunkLatency time.Duration

	// P99InterChunkLatency is the 99th percentile inter-chunk latency.
	P99InterChunkLatency time.Duration

	// TotalTokens is the total output tokens reported by the provider.
	TotalTokens int

	// InputTokens is the total input tokens reported by the provider.
	InputTokens int
}

// Collector accumulates streaming metrics from a channel of StreamChunks.
//
// It is safe to use from a single goroutine (the stream consumer).
// Create one Collector per stream.
type Collector struct {
	start      time.Time
	firstChunk time.Time
	lastChunk  time.Time
	chunks     int
	iclValues  []time.Duration // inter-chunk latency samples
	usage      *llmtrace.Usage
	mu         sync.Mutex
	done       int32 // atomic
}

// NewCollector creates a new streaming metrics collector.
// Call Wrap() to instrument a stream channel, then Metrics() after the channel is drained.
func NewCollector() *Collector {
	return &Collector{
		start:     time.Now(),
		iclValues: make([]time.Duration, 0, 64),
	}
}

// Wrap returns a new channel that forwards all chunks from ch while
// collecting timing metrics. The caller must drain the returned channel
// completely (range over it) before calling Metrics().
//
// The original ch is consumed — do not read from ch after calling Wrap.
func (c *Collector) Wrap(ch <-chan llmtrace.StreamChunk) <-chan llmtrace.StreamChunk {
	out := make(chan llmtrace.StreamChunk)
	go func() {
		defer close(out)
		for chunk := range ch {
			c.record(chunk)
			out <- chunk
		}
		atomic.StoreInt32(&c.done, 1)
	}()
	return out
}

// record captures timing data for a single chunk.
func (c *Collector) record(chunk llmtrace.StreamChunk) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.chunks++

	if c.chunks == 1 {
		c.firstChunk = now
		c.lastChunk = now
	} else {
		icl := now.Sub(c.lastChunk)
		c.iclValues = append(c.iclValues, icl)
		c.lastChunk = now
	}

	if chunk.Usage != nil {
		c.usage = chunk.Usage
	}
}

// Metrics computes the final streaming metrics.
// Call this after the wrapped channel has been fully drained.
func (c *Collector) Metrics() Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := Metrics{
		ChunkCount: c.chunks,
	}

	if c.chunks == 0 {
		return m
	}

	m.TTFT = c.firstChunk.Sub(c.start)
	m.TotalDuration = c.lastChunk.Sub(c.start)

	if c.usage != nil {
		m.TotalTokens = c.usage.OutputTokens
		m.InputTokens = c.usage.InputTokens
	}

	// Tokens per second
	if m.TotalDuration > 0 && m.TotalTokens > 0 {
		m.TokensPerSecond = float64(m.TotalTokens) / m.TotalDuration.Seconds()
	}

	// Inter-chunk latency stats
	if len(c.iclValues) > 0 {
		m.AvgInterChunkLatency = avgDuration(c.iclValues)
		m.MinInterChunkLatency = minDuration(c.iclValues)
		m.MaxInterChunkLatency = maxDuration(c.iclValues)
		m.P50InterChunkLatency = percentileDuration(c.iclValues, 50)
		m.P99InterChunkLatency = percentileDuration(c.iclValues, 99)
	}

	return m
}

// TTFT returns the time to first token observed so far.
// Useful for live monitoring before the stream completes.
func (c *Collector) TTFT() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chunks == 0 {
		return time.Since(c.start)
	}
	return c.firstChunk.Sub(c.start)
}

// ChunkCount returns the number of chunks received so far.
func (c *Collector) ChunkCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chunks
}

// Done reports whether the wrapped channel has been fully drained.
func (c *Collector) Done() bool {
	return atomic.LoadInt32(&c.done) == 1
}

// avgDuration calculates the average of a duration slice.
func avgDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return sum / time.Duration(len(ds))
}

// minDuration returns the minimum duration in a slice.
func minDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	m := ds[0]
	for _, d := range ds[1:] {
		if d < m {
			m = d
		}
	}
	return m
}

// maxDuration returns the maximum duration in a slice.
func maxDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	m := ds[0]
	for _, d := range ds[1:] {
		if d > m {
			m = d
		}
	}
	return m
}

// percentileDuration computes the p-th percentile (0-100) of a duration slice.
func percentileDuration(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sortDurations(sorted)
	idx := int(float64(p) / 100.0 * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// sortDurations sorts a duration slice in-place using insertion sort.
// Suitable for the expected small sizes (hundreds of chunks).
func sortDurations(ds []time.Duration) {
	for i := 1; i < len(ds); i++ {
		key := ds[i]
		j := i - 1
		for j >= 0 && ds[j] > key {
			ds[j+1] = ds[j]
			j--
		}
		ds[j+1] = key
	}
}
