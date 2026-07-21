// Package correlation provides HTTP middleware for request/response correlation IDs.
//
// A correlation ID is a unique identifier attached to each request that flows through
// the system. It enables end-to-end tracing of requests across multiple services,
// making it easier to debug distributed systems and correlate logs.
//
// The middleware:
//   - Extracts a correlation ID from incoming request headers (if present)
//   - Generates a new ID if none is provided (configurable generator)
//   - Stores the ID in the request context for downstream access
//   - Adds the ID to response headers for client-side correlation
//   - Logs the ID via slog for structured log correlation
//
// # Usage
//
// Server middleware:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/api/data", handler)
//
//	corr := correlation.New(correlation.DefaultConfig())
//	handler := corr.Middleware(mux)
//
// Accessing the ID in handlers:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//	    id := correlation.IDFromContext(r.Context())
//	    log.Printf("processing request %s", id)
//	}
//
// Client middleware for downstream propagation:
//
//	client := &http.Client{
//	    Transport: corr.ClientMiddleware(http.DefaultTransport),
//	}
package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

// Default header names used for correlation IDs.
const (
	// HeaderRequestID is the default header name for request correlation.
	HeaderRequestID = "X-Request-ID"

	// HeaderCorrelationID is an alternative header name for correlation.
	HeaderCorrelationID = "X-Correlation-ID"

	// HeaderTraceID can be used for trace-level correlation.
	HeaderTraceID = "X-Trace-ID"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// IDGenerator is a function that generates a new correlation ID.
type IDGenerator func() string

// Config configures the correlation ID middleware behavior.
type Config struct {
	// HeaderName is the HTTP header used to read/write the correlation ID.
	// Multiple header names can be checked in order by setting ExtraHeaders.
	// Default: "X-Request-ID".
	HeaderName string

	// ExtraHeaders are additional header names to check (in order) when
	// the primary HeaderName is not present. This is useful when different
	// clients or proxies use different header names.
	// Default: ["X-Correlation-ID", "X-Trace-ID"].
	ExtraHeaders []string

	// GenerateID is the function used to generate new correlation IDs.
	// Default: random hex (16 bytes = 32 hex characters).
	GenerateID IDGenerator

	// DisableGeneration prevents the middleware from generating a new ID
	// when none is found in the request headers. Useful when you only want
	// to propagate existing IDs.
	// Default: false.
	DisableGeneration bool

	// SetResponseHeader controls whether the correlation ID is added to
	// the response headers. Default: true.
	SetResponseHeader bool

	// LogWithSlog controls whether the correlation ID is logged via slog
	// at the start of each request. Default: false.
	LogWithSlog bool

	// PropagateHeaders is a list of additional headers to propagate from
	// the incoming request to the outgoing response. Useful for forwarding
	// trace context headers.
	PropagateHeaders []string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		HeaderName:        HeaderRequestID,
		ExtraHeaders:      []string{HeaderCorrelationID, HeaderTraceID},
		GenerateID:        GenerateHexID,
		SetResponseHeader: true,
	}
}

// Correlation provides HTTP middleware for correlation ID management.
type Correlation struct {
	cfg Config
}

// New creates a new Correlation middleware instance with the given config.
// If cfg is zero-valued, DefaultConfig() is used.
func New(cfg Config) *Correlation {
	if cfg.HeaderName == "" {
		cfg.HeaderName = HeaderRequestID
	}
	if cfg.GenerateID == nil {
		cfg.GenerateID = GenerateHexID
	}
	if len(cfg.ExtraHeaders) == 0 {
		cfg.ExtraHeaders = []string{HeaderCorrelationID, HeaderTraceID}
	}
	cfg.SetResponseHeader = true // always set response header
	return &Correlation{cfg: cfg}
}

// Middleware returns an HTTP middleware that extracts or generates a correlation ID,
// stores it in the request context, and adds it to the response headers.
func (c *Correlation) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to extract ID from headers
		id := c.extractID(r.Header)

		// Generate new ID if none found and generation is enabled
		if id == "" && !c.cfg.DisableGeneration {
			id = c.cfg.GenerateID()
		}

		// Store in context
		ctx := ContextWithID(r.Context(), id)

		// Log if configured
		if c.cfg.LogWithSlog && id != "" {
			slog.Info("request",
				"correlation_id", id,
				"method", r.Method,
				"path", r.URL.Path,
			)
		}

		// Set response header
		if c.cfg.SetResponseHeader && id != "" {
			w.Header().Set(c.cfg.HeaderName, id)
		}

		// Propagate additional headers
		for _, h := range c.cfg.PropagateHeaders {
			if v := r.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FuncMiddleware returns a middleware that wraps an http.HandlerFunc with correlation ID support.
// This is a convenience method for use with individual handler functions.
func (c *Correlation) FuncMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := c.extractID(r.Header)
		if id == "" && !c.cfg.DisableGeneration {
			id = c.cfg.GenerateID()
		}

		ctx := ContextWithID(r.Context(), id)

		if c.cfg.LogWithSlog && id != "" {
			slog.Info("request",
				"correlation_id", id,
				"method", r.Method,
				"path", r.URL.Path,
			)
		}

		if c.cfg.SetResponseHeader && id != "" {
			w.Header().Set(c.cfg.HeaderName, id)
		}

		for _, h := range c.cfg.PropagateHeaders {
			if v := r.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}

		next(w, r.WithContext(ctx))
	}
}

// ClientMiddleware returns an http.RoundTripper that injects the correlation ID
// from the request context into outgoing HTTP requests. This enables downstream
// services to receive the same correlation ID for end-to-end tracing.
//
// Usage:
//
//	client := &http.Client{
//	    Transport: corr.ClientMiddleware(http.DefaultTransport),
//	}
func (c *Correlation) ClientMiddleware(next http.RoundTripper) http.RoundTripper {
	return &injectingTransport{
		cfg:  c.cfg,
		next: next,
	}
}

// injectingTransport injects correlation IDs into outgoing HTTP requests.
type injectingTransport struct {
	cfg  Config
	next http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *injectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := IDFromContext(req.Context())
	if id != "" {
		req.Header.Set(t.cfg.HeaderName, id)
	}
	return t.next.RoundTrip(req)
}

// extractID attempts to extract a correlation ID from the request headers.
// It checks the primary header first, then any extra headers in order.
func (c *Correlation) extractID(h http.Header) string {
	if id := h.Get(c.cfg.HeaderName); id != "" {
		return strings.TrimSpace(id)
	}
	for _, name := range c.cfg.ExtraHeaders {
		if id := h.Get(name); id != "" {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// ContextWithID stores a correlation ID in the context.
func ContextWithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IDFromContext extracts the correlation ID from the context.
// Returns an empty string if no ID is found.
func IDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(contextKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateHexID creates a random hex correlation ID (16 bytes = 32 hex chars).
// This is the default ID generator.
func GenerateHexID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateShortID creates a shorter random hex ID (8 bytes = 16 hex chars).
// Useful when shorter IDs are preferred for log readability.
func GenerateShortID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GeneratePrefixedID returns a function that creates IDs with a fixed prefix.
// Example: GeneratePrefixedID("req-") produces IDs like "req-a1b2c3d4...".
func GeneratePrefixedID(prefix string) IDGenerator {
	return func() string {
		return prefix + GenerateHexID()
	}
}
