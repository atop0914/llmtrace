package traceexport

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

func sampleTraces() []llmtrace.TraceRecord {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	return []llmtrace.TraceRecord{
		{
			ID:           "trace-0001",
			Provider:     "openai",
			Model:        "gpt-4o",
			Status:       "success",
			InputTokens:  150,
			OutputTokens: 50,
			TotalTokens:  200,
			CostUSD:      0.0025,
			LatencyMS:    1234.56,
			MessageCount: 3,
			MaxTokens:    1000,
			Temperature:  0.7,
			ResponseID:   "resp-abc123",
			FinishReason: "stop",
			Stream:       false,
			StartedAt:    now,
			CompletedAt:  now.Add(1234 * time.Millisecond),
		},
		{
			ID:           "trace-0002",
			Provider:     "anthropic",
			Model:        "claude-3-opus",
			Status:       "error",
			Error:        "rate limit exceeded",
			InputTokens:  0,
			OutputTokens: 0,
			TotalTokens:  0,
			CostUSD:      0,
			LatencyMS:    500.00,
			MessageCount: 1,
			Stream:       true,
			StartedAt:    now.Add(5 * time.Second),
			CompletedAt:  now.Add(5500 * time.Millisecond),
		},
		{
			ID:           "trace-0003",
			Provider:     "gemini",
			Model:        "gemini-1.5-pro",
			Status:       "success",
			InputTokens:  500,
			OutputTokens: 200,
			TotalTokens:  700,
			CostUSD:      0.0035,
			LatencyMS:    2500.00,
			MessageCount: 5,
			MaxTokens:    4096,
			Temperature:  0.3,
			ResponseID:   "resp-xyz789",
			FinishReason: "stop",
			Stream:       false,
			StartedAt:    now.Add(10 * time.Second),
			CompletedAt:  now.Add(12500 * time.Millisecond),
		},
	}
}

// --- JSON Exporter Tests ---

func TestJSONExporter_Writer(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONWriterExporter(&buf)
	traces := sampleTraces()

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("export: %v", err)
	}

	var result []llmtrace.TraceRecord
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("got %d traces, want 3", len(result))
	}
	if result[0].ID != "trace-0001" {
		t.Errorf("first trace ID = %q, want trace-0001", result[0].ID)
	}
	if result[1].Provider != "anthropic" {
		t.Errorf("second trace provider = %q, want anthropic", result[1].Provider)
	}
}

func TestJSONExporter_Indent(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONWriterExporter(&buf, WithIndent())
	traces := sampleTraces()[:1]

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("export: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "  ") {
		t.Error("expected indented output")
	}
}

func TestJSONExporter_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	exp, err := NewJSONExporter(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	traces := sampleTraces()
	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("export: %v", err)
	}
	exp.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var result []llmtrace.TraceRecord
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d traces, want 3", len(result))
	}
}

func TestJSONExporter_EmptyTraces(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONWriterExporter(&buf)

	if err := exp.Export(context.Background(), nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if output != "null" {
		t.Errorf("empty traces = %q, want null", output)
	}
}

func TestJSONExporter_ContextCancellation(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONWriterExporter(&buf)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// JSON export doesn't check context (synchronous), so this should still work
	err := exp.Export(ctx, sampleTraces())
	if err != nil {
		t.Errorf("export should succeed even with cancelled context: %v", err)
	}
}

// --- CSV Exporter Tests ---

func TestCSVExporter_WithHeader(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVWriterExporter(&buf, WithCSVHeader())
	traces := sampleTraces()

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("export: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	// Header + 3 data rows
	if len(records) != 4 {
		t.Fatalf("got %d rows, want 4 (header + 3)", len(records))
	}

	// Check header
	expectedHeader := csvHeader
	for i, h := range expectedHeader {
		if records[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, records[0][i], h)
		}
	}

	// Check first data row
	if records[1][0] != "trace-0001" {
		t.Errorf("row[1][0] = %q, want trace-0001", records[1][0])
	}
	if records[1][1] != "openai" {
		t.Errorf("row[1][1] = %q, want openai", records[1][1])
	}
	if records[1][3] != "success" {
		t.Errorf("row[1][3] = %q, want success", records[1][3])
	}
}

func TestCSVExporter_WithoutHeader(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVWriterExporter(&buf)

	if err := exp.Export(context.Background(), sampleTraces()[:1]); err != nil {
		t.Fatalf("export: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("got %d rows, want 1 (no header)", len(records))
	}
}

func TestCSVExporter_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")

	exp, err := NewCSVExporter(path, WithCSVHeader())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	if err := exp.Export(context.Background(), sampleTraces()); err != nil {
		t.Fatalf("export: %v", err)
	}
	exp.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(data))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}

	if len(records) != 4 { // header + 3
		t.Fatalf("got %d rows, want 4", len(records))
	}
}

func TestCSVExporter_MultipleExports(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVWriterExporter(&buf, WithCSVHeader())

	// First export: writes header + 2 rows
	if err := exp.Export(context.Background(), sampleTraces()[:2]); err != nil {
		t.Fatalf("first export: %v", err)
	}

	// Second export: no header (only written once), 1 row
	if err := exp.Export(context.Background(), sampleTraces()[2:]); err != nil {
		t.Fatalf("second export: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	// 1 header + 3 data rows
	if len(records) != 4 {
		t.Fatalf("got %d rows, want 4", len(records))
	}
}

func TestCSVExporter_EmptyTraces(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVWriterExporter(&buf, WithCSVHeader())

	if err := exp.Export(context.Background(), nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	// Only header
	if len(records) != 1 {
		t.Fatalf("got %d rows, want 1 (header only)", len(records))
	}
}

func TestCSVExporter_ErrorRow(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVWriterExporter(&buf)

	// Export a trace with error
	traces := []llmtrace.TraceRecord{
		{
			ID:          "trace-err",
			Provider:    "openai",
			Model:       "gpt-4o",
			Status:      "error",
			Error:       "connection refused: timeout after 30s",
			StartedAt:   time.Now(),
			CompletedAt: time.Now(),
		},
	}

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("export: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("got %d rows, want 1", len(records))
	}
	if records[0][3] != "error" {
		t.Errorf("status = %q, want error", records[0][3])
	}
	if records[0][4] != "connection refused: timeout after 30s" {
		t.Errorf("error = %q, want 'connection refused: timeout after 30s'", records[0][4])
	}
}

// --- Batch Exporter Tests ---

type countingExporter struct {
	mu      sync.Mutex
	count   int
	batches int
	last    []llmtrace.TraceRecord
}

func (e *countingExporter) Export(_ context.Context, traces []llmtrace.TraceRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count += len(traces)
	e.batches++
	e.last = traces
	return nil
}

func (e *countingExporter) Close() error { return nil }

func (e *countingExporter) getCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

func (e *countingExporter) getBatches() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.batches
}

func TestBatchExporter_Add(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter:     ce,
		Interval:     time.Hour, // won't trigger
		MaxBatchSize: 100,
	})

	traces := sampleTraces()
	batch.Add(traces...)

	if batch.Len() != 3 {
		t.Fatalf("buffer len = %d, want 3", batch.Len())
	}
}

func TestBatchExporter_MaxBatchSize(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter:     ce,
		Interval:     time.Hour,
		MaxBatchSize: 2,
	})

	traces := sampleTraces()
	batch.Add(traces...)

	// Should trigger immediate flush when buffer reaches 3 > 2
	time.Sleep(50 * time.Millisecond)

	if ce.getCount() != 3 {
		t.Errorf("exported %d traces, want 3", ce.getCount())
	}
	if batch.Len() != 0 {
		t.Errorf("buffer len = %d, want 0 after flush", batch.Len())
	}
}

func TestBatchExporter_PeriodicFlush(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter:     ce,
		Interval:     100 * time.Millisecond,
		MaxBatchSize: 0, // no size-based flush
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batch.Start(ctx)
	batch.Add(sampleTraces()...)

	// Wait for periodic flush
	time.Sleep(250 * time.Millisecond)

	if ce.getCount() != 3 {
		t.Errorf("exported %d traces, want 3", ce.getCount())
	}
}

func TestBatchExporter_Stop(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter:     ce,
		Interval:     time.Hour,
		MaxBatchSize: 0,
	})

	batch.Start(context.Background())
	batch.Add(sampleTraces()...)

	if err := batch.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Should have flushed remaining traces on stop
	if ce.getCount() != 3 {
		t.Errorf("exported %d traces, want 3", ce.getCount())
	}
}

func TestBatchExporter_DoubleStart(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter: ce,
		Interval: time.Hour,
	})

	ctx := context.Background()
	batch.Start(ctx)
	batch.Start(ctx) // should be no-op

	batch.Stop()
}

func TestBatchExporter_EmptyFlush(t *testing.T) {
	ce := &countingExporter{}
	batch := NewBatchExporter(BatchConfig{
		Exporter:     ce,
		Interval:     time.Hour,
		MaxBatchSize: 0,
	})

	batch.Start(context.Background())
	time.Sleep(50 * time.Millisecond) // let ticker fire (empty buffer)

	if ce.getBatches() != 0 {
		t.Errorf("batches = %d, want 0 (empty buffer)", ce.getBatches())
	}

	batch.Stop()
}

// --- Rotate Exporter Tests ---

func TestRotateExporter_Basic(t *testing.T) {
	dir := t.TempDir()
	exp, err := NewRotateExporter(RotateConfig{
		Dir:     dir,
		Prefix:  "test",
		Format:  "json",
		MaxSize: 1024 * 1024, // 1MB — won't rotate
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	if err := exp.Export(context.Background(), sampleTraces()); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Check file exists
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "test-") {
		t.Errorf("filename = %q, want prefix 'test-'", entries[0].Name())
	}
	if !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Errorf("filename = %q, want suffix '.json'", entries[0].Name())
	}
}

func TestRotateExporter_CSV(t *testing.T) {
	dir := t.TempDir()
	exp, err := NewRotateExporter(RotateConfig{
		Dir:    dir,
		Prefix: "traces",
		Format: "csv",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	if err := exp.Export(context.Background(), sampleTraces()[:1]); err != nil {
		t.Fatalf("export: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".csv") {
		t.Errorf("filename = %q, want suffix '.csv'", entries[0].Name())
	}
}

func TestRotateExporter_SizeRotation(t *testing.T) {
	dir := t.TempDir()
	exp, err := NewRotateExporter(RotateConfig{
		Dir:      dir,
		Prefix:   "rt",
		Format:   "json",
		MaxSize:  80, // very small, forces rotation after each export
		MaxAge:   0,  // disable age-based
		MaxFiles: 5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	// Export multiple times to trigger rotation
	for i := 0; i < 5; i++ {
		if err := exp.Export(context.Background(), sampleTraces()); err != nil {
			t.Fatalf("export %d: %v", i, err)
		}
	}

	// Check that multiple files were created (rotation happened)
	entries, _ := os.ReadDir(dir)
	if len(entries) < 2 {
		t.Errorf("got %d files, want >= 2 (rotation should have occurred)", len(entries))
	}
}

func TestRotateExporter_MaxFiles(t *testing.T) {
	dir := t.TempDir()
	exp, err := NewRotateExporter(RotateConfig{
		Dir:      dir,
		Prefix:   "rt",
		Format:   "json",
		MaxSize:  100,
		MaxFiles: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer exp.Close()

	// Generate many files
	for i := 0; i < 10; i++ {
		if err := exp.Export(context.Background(), sampleTraces()); err != nil {
			t.Fatalf("export %d: %v", i, err)
		}
	}

	// Allow cleanup goroutine to run
	time.Sleep(100 * time.Millisecond)

	entries, _ := os.ReadDir(dir)
	if len(entries) > 3 { // current + MaxFiles (give some slack)
		t.Errorf("got %d files, want <= 3 (MaxFiles=2 + current)", len(entries))
	}
}

func TestRotateExporter_InvalidDir(t *testing.T) {
	_, err := NewRotateExporter(RotateConfig{
		Dir:    "",
		Prefix: "test",
		Format: "json",
	})
	if err == nil {
		t.Error("expected error for invalid dir")
	}
}

func TestRotateExporter_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := NewRotateExporter(RotateConfig{
		Dir:    dir,
		Prefix: "test",
		Format: "xml", // unsupported
	})
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestRotateExporter_EmptyDir(t *testing.T) {
	_, err := NewRotateExporter(RotateConfig{
		Format: "json",
	})
	if err == nil {
		t.Error("expected error for empty Dir")
	}
}

// --- Interface Compliance ---

var (
	_ Exporter = (*JSONExporter)(nil)
	_ Exporter = (*CSVExporter)(nil)
	_ Exporter = (*RotateExporter)(nil)
)
