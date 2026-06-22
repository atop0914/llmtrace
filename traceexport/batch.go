package traceexport

import (
	"context"
	"sync"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

// BatchConfig configures a BatchExporter.
type BatchConfig struct {
	// Exporter is the underlying exporter to flush to.
	Exporter Exporter

	// Interval is how often buffered traces are exported. Default: 5m.
	Interval time.Duration

	// MaxBatchSize triggers an immediate flush when the buffer reaches this size.
	// 0 means no size-based flush (only time-based).
	MaxBatchSize int
}

// BatchExporter buffers trace records and periodically flushes them
// to an underlying Exporter. This is useful for high-throughput scenarios
// where writing each trace individually would be too expensive.
//
// Usage:
//
//	exp := traceexport.NewJSONExporter("traces.json")
//	batch := traceexport.NewBatchExporter(traceexport.BatchConfig{
//	    Exporter:     exp,
//	    Interval:     5 * time.Minute,
//	    MaxBatchSize: 1000,
//	})
//	batch.Start(ctx)
//	defer batch.Stop()
//
//	// Add traces as they come in
//	batch.Add(trace1, trace2, ...)
type BatchExporter struct {
	cfg     BatchConfig
	mu      sync.Mutex
	buf     []llmtrace.TraceRecord
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewBatchExporter creates a new BatchExporter.
func NewBatchExporter(cfg BatchConfig) *BatchExporter {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	return &BatchExporter{
		cfg:  cfg,
		done: make(chan struct{}),
	}
}

// Add appends traces to the buffer. If MaxBatchSize is set and the buffer
// reaches that size, a flush is triggered immediately.
func (b *BatchExporter) Add(traces ...llmtrace.TraceRecord) {
	b.mu.Lock()
	b.buf = append(b.buf, traces...)
	shouldFlush := b.cfg.MaxBatchSize > 0 && len(b.buf) >= b.cfg.MaxBatchSize
	b.mu.Unlock()

	if shouldFlush {
		go b.flush()
	}
}

// Start begins periodic flushing in a background goroutine.
// The context controls the lifecycle; cancellation stops the flusher.
func (b *BatchExporter) Start(ctx context.Context) {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	ctx, b.cancel = context.WithCancel(ctx)
	b.running = true
	b.mu.Unlock()

	go b.run(ctx)
}

// Stop stops the periodic flusher and flushes any remaining traces.
func (b *BatchExporter) Stop() error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil
	}
	b.running = false
	cancel := b.cancel
	b.mu.Unlock()

	cancel()
	<-b.done

	// Final flush
	b.flush()

	return b.cfg.Exporter.Close()
}

// Len returns the current buffer size.
func (b *BatchExporter) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

// run is the periodic flush loop.
func (b *BatchExporter) run(ctx context.Context) {
	defer close(b.done)

	ticker := time.NewTicker(b.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.flush()
		}
	}
}

// flush exports buffered traces and clears the buffer.
func (b *BatchExporter) flush() {
	b.mu.Lock()
	if len(b.buf) == 0 {
		b.mu.Unlock()
		return
	}
	traces := b.buf
	b.buf = make([]llmtrace.TraceRecord, 0, len(traces))
	b.mu.Unlock()

	_ = b.cfg.Exporter.Export(context.Background(), traces)
}
