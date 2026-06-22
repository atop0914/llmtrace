// Package traceexport provides trace export functionality for LLMTrace.
//
// Traces stored in the in-memory TraceStore can be exported to various
// formats (JSON, CSV) for compliance, offline analysis, and integration
// with external tools like Excel, BigQuery, or data lakes.
//
// Usage:
//
//	store := llmtrace.NewTraceStore(llmtrace.TraceStoreConfig{MaxSize: 10000})
//
//	// Export to JSON file
//	exp := traceexport.NewJSONExporter("traces.json")
//	traces := store.Query(llmtrace.TraceQuery{Since: time.Now().Add(-24 * time.Hour)})
//	if err := exp.Export(context.Background(), traces); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Export to CSV
//	csv := traceexport.NewCSVExporter("traces.csv")
//	if err := csv.Export(context.Background(), traces); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Batch export with periodic flush
//	batch := traceexport.NewBatchExporter(traceexport.BatchConfig{
//	    Exporter: exp,
//	    Interval: 5 * time.Minute,
//	})
//	batch.Add(traces...)
//	batch.Start(ctx)
//	defer batch.Stop()
package traceexport

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	llmtrace "github.com/atop0914/llmtrace"
)

// Exporter defines the interface for trace exporters.
// Implementations write trace records to a destination (file, writer, network).
type Exporter interface {
	// Export writes the given traces to the destination.
	// The context can be used for cancellation and deadline enforcement.
	Export(ctx context.Context, traces []llmtrace.TraceRecord) error

	// Close flushes any buffered data and releases resources.
	Close() error
}

// JSONExporter writes traces as a JSON array to a file or writer.
type JSONExporter struct {
	mu     sync.Mutex
	w      io.Writer
	file   *os.File // non-nil when created from a file path
	indent bool
}

// JSONOption configures a JSONExporter.
type JSONOption func(*JSONExporter)

// WithIndent enables pretty-printed JSON output with 2-space indentation.
func WithIndent() JSONOption {
	return func(e *JSONExporter) {
		e.indent = true
	}
}

// NewJSONExporter creates a JSONExporter that writes to the given file path.
// The file is created if it doesn't exist, truncated if it does.
func NewJSONExporter(path string, opts ...JSONOption) (*JSONExporter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("traceexport: create file: %w", err)
	}
	e := &JSONExporter{w: f, file: f}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

// NewJSONWriterExporter creates a JSONExporter that writes to an io.Writer.
func NewJSONWriterExporter(w io.Writer, opts ...JSONOption) *JSONExporter {
	e := &JSONExporter{w: w}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Export writes traces as a JSON array.
func (e *JSONExporter) Export(_ context.Context, traces []llmtrace.TraceRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	enc := json.NewEncoder(e.w)
	if e.indent {
		enc.SetIndent("", "  ")
	}

	if err := enc.Encode(traces); err != nil {
		return fmt.Errorf("traceexport: json encode: %w", err)
	}
	return nil
}

// Close closes the underlying file if one was opened.
func (e *JSONExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.file != nil {
		return e.file.Close()
	}
	return nil
}

// CSVExporter writes traces as CSV rows to a file or writer.
type CSVExporter struct {
	mu     sync.Mutex
	w      *csv.Writer
	file   *os.File
	closer io.Closer // non-nil when created from a file path
	header bool
}

// CSVOption configures a CSVExporter.
type CSVOption func(*CSVExporter)

// WithCSVHeader enables writing a header row before the first data rows.
func WithCSVHeader() CSVOption {
	return func(e *CSVExporter) {
		e.header = true
	}
}

// NewCSVExporter creates a CSVExporter that writes to the given file path.
func NewCSVExporter(path string, opts ...CSVOption) (*CSVExporter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("traceexport: create file: %w", err)
	}
	e := &CSVExporter{w: csv.NewWriter(f), file: f, closer: f}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

// NewCSVWriterExporter creates a CSVExporter that writes to an io.Writer.
func NewCSVWriterExporter(w io.Writer, opts ...CSVOption) *CSVExporter {
	e := &CSVExporter{w: csv.NewWriter(w)}
	for _, o := range opts {
		o(e)
	}
	return e
}

var csvHeader = []string{
	"id", "provider", "model", "status", "error",
	"input_tokens", "output_tokens", "total_tokens",
	"cost_usd", "latency_ms", "message_count",
	"max_tokens", "temperature", "response_id",
	"finish_reason", "stream", "started_at", "completed_at",
}

// Export writes traces as CSV rows.
func (e *CSVExporter) Export(_ context.Context, traces []llmtrace.TraceRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.header {
		if err := e.w.Write(csvHeader); err != nil {
			return fmt.Errorf("traceexport: csv header: %w", err)
		}
		e.header = false // only write header once
	}

	for _, t := range traces {
		row := []string{
			t.ID,
			t.Provider,
			t.Model,
			t.Status,
			t.Error,
			fmt.Sprintf("%d", t.InputTokens),
			fmt.Sprintf("%d", t.OutputTokens),
			fmt.Sprintf("%d", t.TotalTokens),
			fmt.Sprintf("%.6f", t.CostUSD),
			fmt.Sprintf("%.2f", t.LatencyMS),
			fmt.Sprintf("%d", t.MessageCount),
			fmt.Sprintf("%d", t.MaxTokens),
			fmt.Sprintf("%.2f", t.Temperature),
			t.ResponseID,
			t.FinishReason,
			fmt.Sprintf("%t", t.Stream),
			t.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
			t.CompletedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
		if err := e.w.Write(row); err != nil {
			return fmt.Errorf("traceexport: csv write: %w", err)
		}
	}

	e.w.Flush()
	return e.w.Error()
}

// Close flushes the CSV writer and closes the underlying file.
func (e *CSVExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.w.Flush()
	if e.closer != nil {
		return e.closer.Close()
	}
	return e.w.Error()
}
