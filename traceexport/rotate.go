package traceexport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	llmtrace "github.com/atop0914/llmtrace"
)

// RotateConfig configures a RotateExporter.
type RotateConfig struct {
	// Dir is the directory to write rotated files to.
	Dir string

	// Prefix is the filename prefix. Files are named: {prefix}-{timestamp}.{ext}
	Prefix string

	// Format is the export format: "json" or "csv". Default: "json".
	Format string

	// MaxSize is the maximum file size in bytes before rotation. Default: 100MB.
	// 0 means no size-based rotation.
	MaxSize int64

	// MaxAge is the maximum age of a file before rotation. Default: 24h.
	// 0 means no age-based rotation.
	MaxAge time.Duration

	// MaxFiles is the maximum number of rotated files to keep. 0 means unlimited.
	MaxFiles int

	// Indent enables pretty JSON (only for JSON format).
	Indent bool
}

// RotateExporter writes traces to files that are automatically rotated
// based on size or age. Old files are cleaned up when MaxFiles is set.
//
// Usage:
//
//	exp, err := traceexport.NewRotateExporter(traceexport.RotateConfig{
//	    Dir:      "/var/log/llmtrace",
//	    Prefix:   "traces",
//	    Format:   "json",
//	    MaxSize:  100 * 1024 * 1024, // 100MB
//	    MaxAge:   24 * time.Hour,
//	    MaxFiles: 10,
//	})
type RotateExporter struct {
	cfg         RotateConfig
	mu          sync.Mutex
	current     Exporter
	createdAt   time.Time
	currentPath string
	seq         int // rotation sequence counter for unique filenames
}

// NewRotateExporter creates a new RotateExporter.
func NewRotateExporter(cfg RotateConfig) (*RotateExporter, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("traceexport: Dir is required")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "traces"
	}
	if cfg.Format == "" {
		cfg.Format = "json"
	}
	if cfg.Format != "json" && cfg.Format != "csv" && cfg.Format != "jsonl" {
		return nil, fmt.Errorf("traceexport: unsupported format %q", cfg.Format)
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 100 * 1024 * 1024 // 100MB
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 24 * time.Hour
	}

	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("traceexport: create dir: %w", err)
	}

	exp := &RotateExporter{cfg: cfg}
	if err := exp.rotate(); err != nil {
		return nil, err
	}
	return exp, nil
}

// Export writes traces, rotating the file if necessary.
func (e *RotateExporter) Export(ctx context.Context, traces []llmtrace.TraceRecord) error {
	e.mu.Lock()

	// Check current file size
	if e.cfg.MaxSize > 0 && e.currentPath != "" {
		if info, err := os.Stat(e.currentPath); err == nil && info.Size() >= e.cfg.MaxSize {
			e.mu.Unlock()
			if err := e.rotate(); err != nil {
				return err
			}
			e.mu.Lock()
		}
	} else if e.cfg.MaxAge > 0 && !e.createdAt.IsZero() && time.Since(e.createdAt) >= e.cfg.MaxAge {
		e.mu.Unlock()
		if err := e.rotate(); err != nil {
			return err
		}
		e.mu.Lock()
	}

	exp := e.current
	e.mu.Unlock()

	return exp.Export(ctx, traces)
}

// Close closes the current exporter.
func (e *RotateExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.current != nil {
		return e.current.Close()
	}
	return nil
}

// rotate closes the current file and opens a new one.
func (e *RotateExporter) rotate() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Close current
	if e.current != nil {
		_ = e.current.Close()
	}

	// Generate filename with sequence for uniqueness
	e.seq++
	ts := time.Now().UTC().Format("20060102-150405")
	ext := "." + e.cfg.Format
	name := fmt.Sprintf("%s-%s-%03d.%s", e.cfg.Prefix, ts, e.seq, ext)
	path := filepath.Join(e.cfg.Dir, name)

	var exp Exporter
	var err error
	switch e.cfg.Format {
	case "json":
		var opts []JSONOption
		if e.cfg.Indent {
			opts = append(opts, WithIndent())
		}
		exp, err = NewJSONExporter(path, opts...)
	case "jsonl":
		exp = NewJSONLWriterExporter(nil)
		// For JSONL, open the file directly
		f, ferr := os.Create(path)
		if ferr != nil {
			return fmt.Errorf("traceexport: create jsonl file: %w", ferr)
		}
		exp = NewJSONLWriterExporter(f)
		// We need to track the file for closing
		e.current = exp
		e.currentPath = path
		e.createdAt = time.Now()
		go e.cleanup()
		return nil
	case "csv":
		exp, err = NewCSVExporter(path, WithCSVHeader())
	default:
		return fmt.Errorf("traceexport: unsupported format %q", e.cfg.Format)
	}
	if err != nil {
		return err
	}

	e.current = exp
	e.currentPath = path
	e.createdAt = time.Now()

	// Clean up old files
	go e.cleanup()

	return nil
}

// cleanup removes old rotated files exceeding MaxFiles.
func (e *RotateExporter) cleanup() {
	if e.cfg.MaxFiles <= 0 {
		return
	}

	entries, err := os.ReadDir(e.cfg.Dir)
	if err != nil {
		return
	}

	prefix := e.cfg.Prefix + "-"
	ext := "." + e.cfg.Format

	// Collect matching files
	type fileInfo struct {
		name    string
		modTime time.Time
	}
	var files []fileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > len(prefix)+len(ext) && name[:len(prefix)] == prefix && name[len(name)-len(ext):] == ext {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, fileInfo{name: name, modTime: info.ModTime()})
		}
	}

	// Sort oldest first
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime.Before(files[i].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	// Remove excess files
	if len(files) > e.cfg.MaxFiles {
		for _, f := range files[:len(files)-e.cfg.MaxFiles] {
			_ = os.Remove(filepath.Join(e.cfg.Dir, f.name))
		}
	}
}
