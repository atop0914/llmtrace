package streammetric

import (
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func BenchmarkCollector_Wrap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		collector := NewCollector()
		ch := make(chan llmtrace.StreamChunk, 100)
		for j := 0; j < 100; j++ {
			ch <- llmtrace.StreamChunk{
				Content: "hello world chunk content here",
			}
		}
		close(ch)

		wrapped := collector.Wrap(ch)
		for range wrapped {
		}
		_ = collector.Metrics()
	}
}

func BenchmarkCollector_Record(b *testing.B) {
	collector := NewCollector()
	chunk := llmtrace.StreamChunk{
		Content: "hello",
		Usage: &llmtrace.Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.record(chunk)
	}
}

func BenchmarkMetrics_Compute(b *testing.B) {
	collector := NewCollector()
	ch := make(chan llmtrace.StreamChunk, 500)
	for j := 0; j < 500; j++ {
		ch <- llmtrace.StreamChunk{
			Content: "x",
		}
	}
	close(ch)

	wrapped := collector.Wrap(ch)
	for range wrapped {
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collector.Metrics()
	}
}

func BenchmarkSortDurations(b *testing.B) {
	ds := make([]time.Duration, 100)
	for i := range ds {
		ds[i] = time.Duration(i*17%100) * time.Millisecond
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sortDurations(ds)
	}
}
