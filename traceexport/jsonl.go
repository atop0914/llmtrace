package traceexport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	llmtrace "github.com/atop0914/llmtrace"
)

// JSONLExporter writes traces in JSON Lines format (one JSON object per line).
// This format is ideal for:
//   - Streaming/append-only logging (each record is immediately complete)
//   - Unix tool compatibility (grep, jq, wc -l, tail -f)
//   - Large datasets (no need to parse entire array; line-by-line processing)
//   - Integration with data pipelines (BigQuery, Snowflake, Elasticsearch)
//
// Each line contains a single JSON-serialized TraceRecord terminated by \n.
//
// Usage:
//
//	exp, err := traceexport.NewJSONLExporter("traces.jsonl")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer exp.Close()
//
//	// Export traces incrementally
//	if err := exp.Export(ctx, traces); err != nil {
//	    log.Fatal(err)
//	}
type JSONLExporter struct {
	mu   sync.Mutex
	w    io.Writer
	file *os.File // non-nil when created from a file path
}

// JSONLOption configures a JSONLExporter.
type JSONLOption func(*JSONLExporter)

// NewJSONLExporter creates a JSONLExporter that writes to the given file path.
// The file is created if it doesn't exist, truncated if it does.
func NewJSONLExporter(path string, opts ...JSONLOption) (*JSONLExporter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("traceexport: create file: %w", err)
	}
	e := &JSONLExporter{w: f, file: f}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

// NewJSONLWriterExporter creates a JSONLExporter that writes to an io.Writer.
func NewJSONLWriterExporter(w io.Writer, opts ...JSONLOption) *JSONLExporter {
	e := &JSONLExporter{w: w}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Export writes each trace as a separate JSON line.
func (e *JSONLExporter) Export(_ context.Context, traces []llmtrace.TraceRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	enc := json.NewEncoder(e.w)
	for i := range traces {
		if err := enc.Encode(&traces[i]); err != nil {
			return fmt.Errorf("traceexport: jsonl encode: %w", err)
		}
	}
	return nil
}

// Close closes the underlying file if one was opened.
func (e *JSONLExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.file != nil {
		return e.file.Close()
	}
	return nil
}
