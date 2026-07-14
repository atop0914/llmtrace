package streammetric

import (
	"context"

	"github.com/atop0914/llmtrace"
)

// MetricsCallback is called when a stream completes with its metrics.
type MetricsCallback func(req *llmtrace.Request, m Metrics)

// WithStreamMetrics returns a StreamMiddleware that collects streaming
// performance metrics (TTFT, ICL, TPS) for every streaming call.
//
// The callback is invoked after the stream channel is fully drained,
// allowing you to log, export, or alert on the metrics.
//
// Example:
//
//	mw := streammetric.WithStreamMetrics(func(req *llmtrace.Request, m streammetric.Metrics) {
//	    slog.Info("stream complete",
//	        "model", req.Model,
//	        "ttft", m.TTFT,
//	        "tps", m.TokensPerSecond,
//	        "chunks", m.ChunkCount,
//	    )
//	})
//	stream := llmtrace.ChainStream(mw)(provider.Stream)
func WithStreamMetrics(callback MetricsCallback) llmtrace.StreamMiddleware {
	return func(next llmtrace.StreamFunc) llmtrace.StreamFunc {
		return func(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
			ch, err := next(ctx, req)
			if err != nil {
				return nil, err
			}

			collector := NewCollector()
			wrapped := collector.Wrap(ch)

			// Re-wrap: caller drains `out`, we observe timing
			out := make(chan llmtrace.StreamChunk)
			go func() {
				defer close(out)
				for chunk := range wrapped {
					out <- chunk
				}
				// Stream complete — invoke callback
				m := collector.Metrics()
				callback(req, m)
			}()

			return out, nil
		}
	}
}
