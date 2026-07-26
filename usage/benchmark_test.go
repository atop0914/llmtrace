package usage

import (
	"testing"
	"time"
)

func BenchmarkTracker_Record(b *testing.B) {
	tr := NewTracker(Config{Window: WindowDaily})
	rec := Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		InputTok:  500,
		OutputTok: 200,
		Cost:      0.005,
		Latency:   300 * time.Millisecond,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Record(rec)
	}
}

func BenchmarkTracker_RecordBatch(b *testing.B) {
	records := make([]Record, 100)
	for i := range records {
		records[i] = Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			InputTok:  500,
			OutputTok: 200,
			Cost:      0.005,
			Latency:   300 * time.Millisecond,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr := NewTracker(Config{Window: WindowDaily})
		tr.RecordBatch(records)
	}
}

func BenchmarkReport(b *testing.B) {
	tr := NewTracker(Config{Window: WindowDaily})
	for i := 0; i < 1000; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			InputTok:  500,
			OutputTok: 200,
			Cost:      0.005,
			Latency:   300 * time.Millisecond,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tr.Report()
	}
}

func BenchmarkDetectTrend(b *testing.B) {
	tr := NewTracker(Config{Window: WindowDaily})
	base := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 30; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			Cost:      float64(i) * 0.001,
			Timestamp: base.Add(time.Duration(i) * 24 * time.Hour),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tr.DetectTrend()
	}
}

func BenchmarkDetectAnomalies(b *testing.B) {
	tr := NewTracker(Config{Window: WindowDaily, AnomalyThreshold: 2.5})
	for i := 0; i < 100; i++ {
		tr.Record(Record{
			Provider:  "openai",
			Model:     "gpt-4o",
			Cost:      0.01,
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
		})
	}
	tr.Record(Record{
		Provider:  "openai",
		Model:     "gpt-4o",
		Cost:      5.0,
		Timestamp: time.Now().Add(101 * time.Hour),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tr.DetectAnomalies()
	}
}
