package streammetric

import (
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func TestCollector_EmptyStream(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk)
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	if m.ChunkCount != 0 {
		t.Errorf("expected 0 chunks, got %d", m.ChunkCount)
	}
	if m.TTFT != 0 {
		t.Errorf("expected 0 TTFT, got %v", m.TTFT)
	}
	if m.TotalDuration != 0 {
		t.Errorf("expected 0 total duration, got %v", m.TotalDuration)
	}
}

func TestCollector_SingleChunk(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 1)
	ch <- llmtrace.StreamChunk{
		Content: "Hello",
	}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	if m.ChunkCount != 1 {
		t.Errorf("expected 1 chunk, got %d", m.ChunkCount)
	}
	if m.TTFT <= 0 {
		t.Errorf("expected positive TTFT, got %v", m.TTFT)
	}
	if m.TotalTokens != 0 {
		t.Errorf("expected 0 tokens (no usage), got %d", m.TotalTokens)
	}
}

func TestCollector_MultipleChunks(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 3)

	ch <- llmtrace.StreamChunk{Content: "Hello"}
	time.Sleep(10 * time.Millisecond)
	ch <- llmtrace.StreamChunk{Content: "Hello world"}
	time.Sleep(10 * time.Millisecond)
	ch <- llmtrace.StreamChunk{
		Content: "Hello world!",
		Usage: &llmtrace.Usage{
			InputTokens:  10,
			OutputTokens: 3,
			TotalTokens:  13,
		},
	}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	if m.ChunkCount != 3 {
		t.Errorf("expected 3 chunks, got %d", m.ChunkCount)
	}
	if m.TTFT <= 0 {
		t.Errorf("expected positive TTFT, got %v", m.TTFT)
	}
	if m.TotalDuration < m.TTFT {
		t.Errorf("total duration %v should be >= TTFT %v", m.TotalDuration, m.TTFT)
	}
	if m.TotalTokens != 3 {
		t.Errorf("expected 3 total tokens, got %d", m.TotalTokens)
	}
	if m.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", m.InputTokens)
	}
	if m.TokensPerSecond <= 0 {
		t.Errorf("expected positive TPS, got %f", m.TokensPerSecond)
	}
	if m.AvgInterChunkLatency <= 0 {
		t.Errorf("expected positive avg ICL, got %v", m.AvgInterChunkLatency)
	}
	if m.MinInterChunkLatency <= 0 {
		t.Errorf("expected positive min ICL, got %v", m.MinInterChunkLatency)
	}
	if m.MaxInterChunkLatency <= 0 {
		t.Errorf("expected positive max ICL, got %v", m.MaxInterChunkLatency)
	}
	if m.P50InterChunkLatency <= 0 {
		t.Errorf("expected positive P50 ICL, got %v", m.P50InterChunkLatency)
	}
	if m.P99InterChunkLatency <= 0 {
		t.Errorf("expected positive P99 ICL, got %v", m.P99InterChunkLatency)
	}
}

func TestCollector_TTFTLiveMonitoring(t *testing.T) {
	collector := NewCollector()

	// Before any chunks, TTFT should reflect elapsed time
	ttft := collector.TTFT()
	if ttft < 0 {
		t.Errorf("expected non-negative TTFT before chunks, got %v", ttft)
	}

	// Chunk count before any chunks
	if count := collector.ChunkCount(); count != 0 {
		t.Errorf("expected 0 chunks initially, got %d", count)
	}

	// Feed one chunk
	ch := make(chan llmtrace.StreamChunk, 1)
	ch <- llmtrace.StreamChunk{Content: "hi"}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	if count := collector.ChunkCount(); count != 1 {
		t.Errorf("expected 1 chunk, got %d", count)
	}
	if !collector.Done() {
		t.Error("expected Done() to be true after draining")
	}
}

func TestCollector_UsageFromLastChunk(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 3)

	// Early chunks have no usage
	ch <- llmtrace.StreamChunk{Content: "a"}
	ch <- llmtrace.StreamChunk{Content: "ab"}
	// Last chunk has usage (common pattern with OpenAI/Anthropic)
	ch <- llmtrace.StreamChunk{
		Content: "abc",
		Usage: &llmtrace.Usage{
			InputTokens:  5,
			OutputTokens: 3,
			TotalTokens:  8,
		},
	}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	if m.TotalTokens != 3 {
		t.Errorf("expected 3 output tokens from last chunk usage, got %d", m.TotalTokens)
	}
	if m.InputTokens != 5 {
		t.Errorf("expected 5 input tokens, got %d", m.InputTokens)
	}
}

func TestCollector_UsageOverwrite(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 3)

	// First chunk has usage (e.g., Anthropic sends usage early)
	ch <- llmtrace.StreamChunk{
		Usage: &llmtrace.Usage{OutputTokens: 1, TotalTokens: 5},
	}
	// Last chunk has updated usage
	ch <- llmtrace.StreamChunk{
		Usage: &llmtrace.Usage{OutputTokens: 10, TotalTokens: 15},
	}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	// Should use the LAST chunk's usage
	if m.TotalTokens != 10 {
		t.Errorf("expected 10 output tokens (last chunk), got %d", m.TotalTokens)
	}
}

func TestMetrics_NoTokens(t *testing.T) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 1)
	ch <- llmtrace.StreamChunk{Content: "x"}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	m := collector.Metrics()
	if m.TokensPerSecond != 0 {
		t.Errorf("expected 0 TPS with no token usage, got %f", m.TokensPerSecond)
	}
}

func TestSortDurations(t *testing.T) {
	ds := []time.Duration{
		30 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
	}
	sortDurations(ds)

	for i := 1; i < len(ds); i++ {
		if ds[i] < ds[i-1] {
			t.Errorf("not sorted at index %d: %v < %v", i, ds[i], ds[i-1])
		}
	}
}

func TestPercentileDuration(t *testing.T) {
	ds := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	}

	p50 := percentileDuration(ds, 50)
	if p50 != 3*time.Millisecond {
		t.Errorf("expected P50=3ms, got %v", p50)
	}

	p0 := percentileDuration(ds, 0)
	if p0 != 1*time.Millisecond {
		t.Errorf("expected P0=1ms, got %v", p0)
	}

	p100 := percentileDuration(ds, 100)
	if p100 != 5*time.Millisecond {
		t.Errorf("expected P100=5ms, got %v", p100)
	}

	// Empty slice
	if p := percentileDuration(nil, 50); p != 0 {
		t.Errorf("expected 0 for nil slice, got %v", p)
	}
}

func TestAvgDuration(t *testing.T) {
	ds := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}
	avg := avgDuration(ds)
	if avg != 20*time.Millisecond {
		t.Errorf("expected 20ms avg, got %v", avg)
	}

	if avgDuration(nil) != 0 {
		t.Error("expected 0 for nil slice")
	}
}

func TestMinMaxDuration(t *testing.T) {
	ds := []time.Duration{
		5 * time.Millisecond,
		1 * time.Millisecond,
		10 * time.Millisecond,
	}

	if min := minDuration(ds); min != 1*time.Millisecond {
		t.Errorf("expected min=1ms, got %v", min)
	}
	if max := maxDuration(ds); max != 10*time.Millisecond {
		t.Errorf("expected max=10ms, got %v", max)
	}

	if minDuration(nil) != 0 {
		t.Error("expected 0 for nil slice")
	}
	if maxDuration(nil) != 0 {
		t.Error("expected 0 for nil slice")
	}
}

func TestCollector_ConcurrentSafety(t *testing.T) {
	// Verify no race conditions with the race detector
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 100)

	for i := 0; i < 100; i++ {
		ch <- llmtrace.StreamChunk{Content: "x"}
	}
	close(ch)

	wrapped := collector.Wrap(ch)

	done := make(chan struct{})
	go func() {
		for range wrapped {
		}
		close(done)
	}()

	// Access live methods concurrently
	for i := 0; i < 10; i++ {
		_ = collector.TTFT()
		_ = collector.ChunkCount()
	}

	<-done
	_ = collector.Metrics()
}
