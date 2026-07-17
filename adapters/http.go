package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// HeaderRequestID is the header name for request correlation IDs.
	HeaderRequestID = "X-Request-ID"

	// HeaderResponseTime is the header for response duration in milliseconds.
	HeaderResponseTime = "X-Response-Time-Ms"

	// HeaderLLMProvider is the header for the LLM provider name.
	HeaderLLMProvider = "X-LLM-Provider"

	// HeaderTokensUsed is the header for total tokens consumed.
	HeaderTokensUsed = "X-Tokens-Used"

	// TracerName is the instrumentation name for adapter spans.
	TracerName = "github.com/atop0914/llmtrace/adapters"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// RequestData holds LLM-specific metadata attached to an HTTP request.
type RequestData struct {
	// RequestID is the unique correlation ID for this request.
	RequestID string

	// Provider is the LLM provider name (set after routing).
	Provider string

	// Model is the model identifier (set after routing).
	Model string

	// TokensUsed tracks total tokens consumed (set after response).
	TokensUsed int

	// StartedAt is when the request was received.
	StartedAt time.Time
}

// Config holds configuration for the HTTP middleware.
type Config struct {
	// TracerName overrides the default OpenTelemetry instrumentation name.
	TracerName string

	// GenerateRequestID controls whether a new request ID is generated
	// if none is present in the incoming request. Default: true.
	GenerateRequestID bool

	// AddResponseHeaders controls whether timing and token headers
	// are added to responses. Default: true.
	AddResponseHeaders bool

	// RecoverFromPanic controls whether panics are caught and converted
	// to 500 responses. Default: true.
	RecoverFromPanic bool

	// SpanNameFunc optionally overrides the span name for a request.
	// Default: "HTTP <method> <path>".
	SpanNameFunc func(r *http.Request) string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TracerName:         TracerName,
		GenerateRequestID:  true,
		AddResponseHeaders: true,
		RecoverFromPanic:   true,
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.written += n
	return n, err
}

// Middleware returns an HTTP middleware that adds request correlation,
// OpenTelemetry tracing, and response metadata to each request.
//
// The middleware:
//   - Extracts or generates a request ID (X-Request-ID header)
//   - Creates an OpenTelemetry span for each request
//   - Stores RequestData in context (accessible via RequestDataFromContext)
//   - Adds response headers for timing and token usage
//   - Recovers from panics and returns 500 errors
func Middleware(cfg Config) func(http.Handler) http.Handler {
	tracer := otel.GetTracerProvider().Tracer(cfg.TracerName)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract or generate request ID
			reqID := r.Header.Get(HeaderRequestID)
			if reqID == "" && cfg.GenerateRequestID {
				reqID = generateRequestID()
			}

			// Create OpenTelemetry span
			spanName := fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path)
			if cfg.SpanNameFunc != nil {
				spanName = cfg.SpanNameFunc(r)
			}

			ctx, span := tracer.Start(r.Context(), spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.target", r.URL.Path),
					attribute.String("http.host", r.Host),
					attribute.String("http.user_agent", r.UserAgent()),
					attribute.String("http.request_id", reqID),
				),
			)
			defer span.End()

			// Attach request data to context
			data := &RequestData{
				RequestID: reqID,
				StartedAt: start,
			}
			ctx = context.WithValue(ctx, contextKey{}, data)

			// Inject request ID into response headers
			if reqID != "" {
				w.Header().Set(HeaderRequestID, reqID)
			}

			// Wrap response writer to capture status code
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Recover from panics if configured
			if cfg.RecoverFromPanic {
				defer func() {
					if rec := recover(); rec != nil {
						span.RecordError(fmt.Errorf("panic: %v", rec))
						span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", rec))
						span.SetAttributes(attribute.String("error.stack", string(debug.Stack())))

						rw.WriteHeader(http.StatusInternalServerError)
						fmt.Fprintf(rw, `{"error":"internal server error","request_id":"%s"}`, reqID)
					}
				}()
			}

			// Call the next handler with the enriched context
			next.ServeHTTP(rw, r.WithContext(ctx))

			// Record response attributes on the span
			duration := time.Since(start)
			span.SetAttributes(
				attribute.Int("http.status_code", rw.statusCode),
				attribute.Int("http.response_size", rw.written),
				attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
			)

			if rw.statusCode >= 400 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", rw.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			// Add response headers if configured
			if cfg.AddResponseHeaders {
				w.Header().Set(HeaderResponseTime, fmt.Sprintf("%.2f", float64(duration.Microseconds())/1000.0))
				if data.Provider != "" {
					w.Header().Set(HeaderLLMProvider, data.Provider)
				}
				if data.TokensUsed > 0 {
					w.Header().Set(HeaderTokensUsed, fmt.Sprintf("%d", data.TokensUsed))
				}
			}
		})
	}
}

// RequestDataFromContext extracts RequestData from the context.
// Returns nil if no data is found.
func RequestDataFromContext(ctx context.Context) *RequestData {
	if v, ok := ctx.Value(contextKey{}).(*RequestData); ok {
		return v
	}
	return nil
}

// SetProvider sets the LLM provider name on the request data in context.
func SetProvider(ctx context.Context, provider string) {
	if data := RequestDataFromContext(ctx); data != nil {
		data.Provider = provider
	}
}

// SetModel sets the model name on the request data in context.
func SetModel(ctx context.Context, model string) {
	if data := RequestDataFromContext(ctx); data != nil {
		data.Model = model
	}
}

// SetTokensUsed sets the token count on the request data in context.
func SetTokensUsed(ctx context.Context, tokens int) {
	if data := RequestDataFromContext(ctx); data != nil {
		data.TokensUsed = tokens
	}
}

// generateRequestID creates a random hex request ID (16 bytes = 32 hex chars).
func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
