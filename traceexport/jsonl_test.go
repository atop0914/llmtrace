package traceexport

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

func TestJSONLExporter_WriterExporter(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	traces := []llmtrace.TraceRecord{
		{
			ID:           "trace-1",
			Provider:     "openai",
			Model:        "gpt-4o",
			Status:       "success",
			InputTokens:  100,
			OutputTokens:  50,
			TotalTokens:  150,
			CostUSD:      0.005,
			LatencyMS:    230.5,
			StartedAt:    time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
			CompletedAt:  time.Date(2026, 1, 15, 10, 0, 0, 230500000, time.UTC),
		},
		{
			ID:           "trace-2",
			Provider:     "anthropic",
			Model:        "claude-3-opus",
			Status:       "error",
			Error:        "rate limit exceeded",
			InputTokens:  200,
			OutputTokens:  0,
			TotalTokens:  200,
			CostUSD:      0,
			LatencyMS:    500.0,
			StartedAt:    time.Date(2026, 1, 15, 10, 1, 0, 0, time.UTC),
			CompletedAt:  time.Date(2026, 1, 15, 10, 1, 0, 500000000, time.UTC),
		},
	}

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Parse first line
	var first llmtrace.TraceRecord
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("failed to parse first line: %v", err)
	}
	if first.ID != "trace-1" {
		t.Errorf("expected ID trace-1, got %s", first.ID)
	}
	if first.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", first.Provider)
	}

	// Parse second line
	var second llmtrace.TraceRecord
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("failed to parse second line: %v", err)
	}
	if second.ID != "trace-2" {
		t.Errorf("expected ID trace-2, got %s", second.ID)
	}
	if second.Error != "rate limit exceeded" {
		t.Errorf("expected error 'rate limit exceeded', got %s", second.Error)
	}
}

func TestJSONLExporter_FileExporter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")

	exp, err := NewJSONLExporter(path)
	if err != nil {
		t.Fatalf("NewJSONLExporter failed: %v", err)
	}

	traces := []llmtrace.TraceRecord{
		{ID: "trace-1", Provider: "openai", Model: "gpt-4o", Status: "success"},
		{ID: "trace-2", Provider: "anthropic", Model: "claude-3-opus", Status: "success"},
	}

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if err := exp.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read file and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var rec llmtrace.TraceRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestJSONLExporter_AppendMode(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	// First batch
	batch1 := []llmtrace.TraceRecord{
		{ID: "trace-1", Provider: "openai"},
	}
	if err := exp.Export(context.Background(), batch1); err != nil {
		t.Fatalf("first Export failed: %v", err)
	}

	// Second batch
	batch2 := []llmtrace.TraceRecord{
		{ID: "trace-2", Provider: "anthropic"},
		{ID: "trace-3", Provider: "gemini"},
	}
	if err := exp.Export(context.Background(), batch2); err != nil {
		t.Fatalf("second Export failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Verify IDs in order
	ids := make([]string, 3)
	for i, line := range lines {
		var rec llmtrace.TraceRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		ids[i] = rec.ID
	}

	expected := []string{"trace-1", "trace-2", "trace-3"}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("line %d: expected ID %s, got %s", i, want, ids[i])
		}
	}
}

func TestJSONLExporter_EmptyTraces(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	if err := exp.Export(context.Background(), nil); err != nil {
		t.Fatalf("Export(nil) failed: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil traces, got %d bytes", buf.Len())
	}

	if err := exp.Export(context.Background(), []llmtrace.TraceRecord{}); err != nil {
		t.Fatalf("Export(empty) failed: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty traces, got %d bytes", buf.Len())
	}
}

func TestJSONLExporter_CloseWriter(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	// Close on writer exporter should be a no-op
	if err := exp.Close(); err != nil {
		t.Errorf("Close on writer exporter should return nil, got: %v", err)
	}
}

func TestJSONLExporter_BatchCompatibility(t *testing.T) {
	// Verify JSONLExporter works with BatchExporter
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	batch := NewBatchExporter(BatchConfig{
		Exporter: exp,
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batch.Start(ctx)

	traces := []llmtrace.TraceRecord{
		{ID: "batch-1", Provider: "openai"},
		{ID: "batch-2", Provider: "anthropic"},
	}
	batch.Add(traces...)

	// Wait for flush
	time.Sleep(50 * time.Millisecond)
	batch.Stop()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines from batch, got %d", len(lines))
	}
}

func TestJSONLExporter_RotateCompatibility(t *testing.T) {
	// Verify JSONLExporter works with RotateExporter
	dir := t.TempDir()

	rotExp, err := NewRotateExporter(RotateConfig{
		Dir:    dir,
		Prefix: "traces",
		Format: "jsonl",
		MaxSize: 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRotateExporter(jsonl) failed: %v", err)
	}

	traces := []llmtrace.TraceRecord{
		{ID: "rotate-1", Provider: "openai"},
		{ID: "rotate-2", Provider: "anthropic"},
	}

	if err := rotExp.Export(context.Background(), traces); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if err := rotExp.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify file was created
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	found := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			found = true
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("ReadFile failed: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 2 {
				t.Errorf("expected 2 lines, got %d", len(lines))
			}
		}
	}
	if !found {
		t.Error("no .jsonl file found in rotate directory")
	}
}

func TestJSONLExporter_AllFields(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	now := time.Now().UTC().Truncate(time.Millisecond)
	traces := []llmtrace.TraceRecord{
		{
			ID:           "full-trace",
			Provider:     "openai",
			Model:        "gpt-4o",
			Status:       "success",
			Error:        "",
			InputTokens:  150,
			OutputTokens:  75,
			TotalTokens:  225,
			CostUSD:      0.0075,
			LatencyMS:    312.5,
			MessageCount: 5,
			MaxTokens:    4096,
			Temperature:  0.7,
			ResponseID:   "resp-abc123",
			FinishReason: "stop",
			Stream:       false,
			StartedAt:    now,
			CompletedAt:  now.Add(312500 * time.Microsecond),
		},
	}

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var rec llmtrace.TraceRecord
	line := strings.TrimSpace(buf.String())
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if rec.ID != "full-trace" {
		t.Errorf("ID: got %s, want full-trace", rec.ID)
	}
	if rec.Provider != "openai" {
		t.Errorf("Provider: got %s, want openai", rec.Provider)
	}
	if rec.Model != "gpt-4o" {
		t.Errorf("Model: got %s, want gpt-4o", rec.Model)
	}
	if rec.InputTokens != 150 {
		t.Errorf("InputTokens: got %d, want 150", rec.InputTokens)
	}
	if rec.OutputTokens != 75 {
		t.Errorf("OutputTokens: got %d, want 75", rec.OutputTokens)
	}
	if rec.TotalTokens != 225 {
		t.Errorf("TotalTokens: got %d, want 225", rec.TotalTokens)
	}
	if rec.CostUSD != 0.0075 {
		t.Errorf("CostUSD: got %f, want 0.0075", rec.CostUSD)
	}
	if rec.MaxTokens != 4096 {
		t.Errorf("MaxTokens: got %d, want 4096", rec.MaxTokens)
	}
	if rec.Temperature != 0.7 {
		t.Errorf("Temperature: got %f, want 0.7", rec.Temperature)
	}
	if rec.ResponseID != "resp-abc123" {
		t.Errorf("ResponseID: got %s, want resp-abc123", rec.ResponseID)
	}
	if rec.FinishReason != "stop" {
		t.Errorf("FinishReason: got %s, want stop", rec.FinishReason)
	}
	if rec.Stream {
		t.Errorf("Stream: got true, want false")
	}
	if rec.MessageCount != 5 {
		t.Errorf("MessageCount: got %d, want 5", rec.MessageCount)
	}
}

func TestJSONLExporter_JQCompatibility(t *testing.T) {
	// Verify output is compatible with jq-style line-by-line processing
	var buf bytes.Buffer
	exp := NewJSONLWriterExporter(&buf)

	traces := []llmtrace.TraceRecord{
		{ID: "jq-1", Provider: "openai", Model: "gpt-4o", Status: "success", CostUSD: 0.01},
		{ID: "jq-2", Provider: "anthropic", Model: "claude-3", Status: "error", CostUSD: 0},
	}

	if err := exp.Export(context.Background(), traces); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Each line should be independently parseable (as jq does)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d: not valid JSON for jq processing: %v", i, err)
		}
	}
}
