// Package propagation implements W3C Trace Context propagation for distributed tracing.
//
// It provides a standards-compliant implementation of the W3C Trace Context specification
// for propagating trace context across service boundaries via HTTP headers.
//
// The two key headers are:
//   - traceparent: identifies the full trace (trace ID, parent span ID, trace flags)
//   - tracestate: carries vendor-specific trace data as key-value pairs
//
// # Traceparent Format
//
// The traceparent header has the format:
//
//	{version:2}-{trace-id:32}-{parent-span-id:16}-{trace-flags:2}
//
// Example:
//
//	00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
//
// # Usage
//
//	prop := propagation.New()
//
//	// Server side: extract incoming trace context
//	spanCtx, ok := prop.Extract(r.Header)
//
//	// Client side: inject trace context into outgoing request
//	prop.Inject(spanCtx, req.Header)
//
//	// HTTP middleware for automatic propagation
//	handler := propagation.Middleware(prop)(myHandler)
package propagation

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const (
	// HeaderTraceParent is the W3C Trace Context traceparent header.
	HeaderTraceParent = "Traceparent"

	// HeaderTraceState is the W3C Trace Context tracestate header.
	HeaderTraceState = "Tracestate"

	// traceparentVersion is the only supported version.
	traceparentVersion = 0

	// traceIDLen is the hex-encoded length of a trace ID (16 bytes).
	traceIDLen = 32

	// spanIDLen is the hex-encoded length of a span ID (8 bytes).
	spanIDLen = 16

	// flagsLen is the hex-encoded length of trace flags (1 byte).
	flagsLen = 2

	// traceFlagSampled indicates the trace is sampled.
	traceFlagSampled = 0x01
)

// Errors returned by the propagator.
var (
	ErrInvalidTraceParent = errors.New("propagation: invalid traceparent header")
	ErrInvalidTraceID     = errors.New("propagation: invalid trace ID (all zeros)")
	ErrInvalidSpanID      = errors.New("propagation: invalid span ID (all zeros)")
)

// Carrier provides read/write access to a set of headers.
// This abstraction allows the propagator to work with HTTP headers,
// gRPC metadata, or any string-based key-value pairs.
type Carrier interface {
	// Get returns the value associated with the given key.
	Get(key string) string

	// Set sets the value for the given key.
	Set(key, value string)
}

// MapCarrier implements Carrier using a plain map.
// Useful for testing and non-HTTP transports.
type MapCarrier map[string]string

// Get returns the value for the given key.
func (m MapCarrier) Get(key string) string {
	return m[key]
}

// Set sets the value for the given key.
func (m MapCarrier) Set(key, value string) {
	m[key] = value
}

// HTTPCarrier wraps http.Header to implement Carrier.
type HTTPCarrier struct {
	header http.Header
}

// NewHTTPCarrier creates a Carrier backed by the given HTTP headers.
func NewHTTPCarrier(h http.Header) *HTTPCarrier {
	return &HTTPCarrier{header: h}
}

// Get returns the first value for the given key.
func (c *HTTPCarrier) Get(key string) string {
	return c.header.Get(key)
}

// Set sets the header value, replacing any existing values.
func (c *HTTPCarrier) Set(key, value string) {
	c.header.Set(key, value)
}

// TraceContext implements W3C Trace Context propagation.
type TraceContext struct{}

// New creates a new W3C Trace Context propagator.
func New() *TraceContext {
	return &TraceContext{}
}

// Extract extracts a remote SpanContext from the carrier.
// Returns the SpanContext and true if a valid traceparent was found.
// Returns a zero SpanContext and false if no traceparent is present or it's invalid.
//
// When tracestate is present, it is stored in the SpanContext's trace state.
func (tc *TraceContext) Extract(carrier Carrier) (trace.SpanContext, bool) {
	tp := carrier.Get(HeaderTraceParent)
	if tp == "" {
		return trace.SpanContext{}, false
	}

	spanCtx, err := parseTraceParent(tp)
	if err != nil {
		return trace.SpanContext{}, false
	}

	// Parse tracestate if present
	ts := carrier.Get(HeaderTraceState)
	if ts != "" {
		traceState, err := trace.ParseTraceState(ts)
		if err == nil {
			spanCtx = spanCtx.WithTraceState(traceState)
		}
	}

	return spanCtx, true
}

// Inject injects a SpanContext into the carrier as W3C Trace Context headers.
// If the SpanContext is not valid, no headers are set.
func (tc *TraceContext) Inject(spanCtx trace.SpanContext, carrier Carrier) {
	if !spanCtx.IsValid() {
		return
	}

	// Build traceparent header
	flags := "00"
	if spanCtx.IsSampled() {
		flags = "01"
	}
	traceparent := fmt.Sprintf("00-%s-%s-%s",
		spanCtx.TraceID().String(),
		spanCtx.SpanID().String(),
		flags,
	)
	carrier.Set(HeaderTraceParent, traceparent)

	// Build tracestate header if present
	traceState := spanCtx.TraceState()
	if traceState.Len() > 0 {
		var entries []string
		traceState.Walk(func(key, value string) bool {
			entries = append(entries, key+"="+value)
			return true
		})
		carrier.Set(HeaderTraceState, strings.Join(entries, ","))
	}
}

// ExtractFromHTTP is a convenience method for extracting trace context from HTTP headers.
func (tc *TraceContext) ExtractFromHTTP(h http.Header) (trace.SpanContext, bool) {
	return tc.Extract(NewHTTPCarrier(h))
}

// InjectIntoHTTP is a convenience method for injecting trace context into HTTP headers.
func (tc *TraceContext) InjectIntoHTTP(spanCtx trace.SpanContext, h http.Header) {
	tc.Inject(spanCtx, NewHTTPCarrier(h))
}

// ExtractFromMap is a convenience method for extracting from a map.
func (tc *TraceContext) ExtractFromMap(m map[string]string) (trace.SpanContext, bool) {
	return tc.Extract(MapCarrier(m))
}

// InjectIntoMap is a convenience method for injecting into a map.
func (tc *TraceContext) InjectIntoMap(spanCtx trace.SpanContext, m map[string]string) {
	tc.Inject(spanCtx, MapCarrier(m))
}

// FormatTraceParent formats a traceparent header value from trace ID, span ID, and flags.
// Returns an error if the inputs are invalid.
func FormatTraceParent(traceID trace.TraceID, spanID trace.SpanID, sampled bool) (string, error) {
	if traceID.IsValid() == false && traceID != (trace.TraceID{}) {
		return "", ErrInvalidTraceID
	}

	flags := "00"
	if sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s",
		traceID.String(),
		spanID.String(),
		flags,
	), nil
}

// parseTraceParent parses a traceparent header string.
// Format: version-traceid-spanid-flags
// All fields are lowercase hex.
func parseTraceParent(tp string) (trace.SpanContext, error) {
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}

	// Version must be "00"
	version, err := strconv.ParseUint(parts[0], 16, 8)
	if err != nil || version != traceparentVersion {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}

	// Trace ID: 32 hex chars = 16 bytes
	if len(parts[1]) != traceIDLen {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}
	traceIDBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}
	var traceID trace.TraceID
	copy(traceID[:], traceIDBytes)

	// Trace ID must not be all zeros
	if traceID == (trace.TraceID{}) {
		return trace.SpanContext{}, ErrInvalidTraceID
	}

	// Parent Span ID: 16 hex chars = 8 bytes
	if len(parts[2]) != spanIDLen {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}
	spanIDBytes, err := hex.DecodeString(parts[2])
	if err != nil {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}
	var spanID trace.SpanID
	copy(spanID[:], spanIDBytes)

	// Span ID must not be all zeros
	if spanID == (trace.SpanID{}) {
		return trace.SpanContext{}, ErrInvalidSpanID
	}

	// Flags: 2 hex chars = 1 byte
	if len(parts[3]) != flagsLen {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}
	flags, err := strconv.ParseUint(parts[3], 16, 8)
	if err != nil {
		return trace.SpanContext{}, ErrInvalidTraceParent
	}

	// Build SpanContext
	config := []trace.SpanContextConfig{
		{
			TraceID:    traceID,
			SpanID:     spanID,
			Remote:     true,
			TraceFlags: trace.TraceFlags(flags),
		},
	}

	return trace.NewSpanContext(config[0]), nil
}


// context key for storing extracted SpanContext
type spanContextKey struct{}

// ContextWithSpanContext returns a new context with the SpanContext stored.
func ContextWithSpanContext(ctx context.Context, spanCtx trace.SpanContext) context.Context {
	return context.WithValue(ctx, spanContextKey{}, spanCtx)
}

// SpanContextFromContext extracts the SpanContext from the context.
// Returns the SpanContext and true if present.
func SpanContextFromContext(ctx context.Context) (trace.SpanContext, bool) {
	spanCtx, ok := ctx.Value(spanContextKey{}).(trace.SpanContext)
	return spanCtx, ok
}

// Middleware returns an HTTP middleware that extracts trace context from incoming
// requests and stores it in the request context. Downstream handlers can then
// create child spans that are properly connected to the distributed trace.
//
// Usage:
//
//	prop := propagation.New()
//	handler := propagation.Middleware(prop)(myHandler)
func Middleware(prop *TraceContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			spanCtx, ok := prop.ExtractFromHTTP(r.Header)
			if ok {
				ctx := ContextWithSpanContext(r.Context(), spanCtx)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientMiddleware returns an HTTP RoundTripper that injects trace context
// into outgoing requests. It extracts the SpanContext from the request context
// (set by the server-side Middleware) and propagates it.
//
// Usage:
//
//	client := &http.Client{
//	    Transport: propagation.ClientMiddleware(prop)(http.DefaultTransport),
//	}
func ClientMiddleware(prop *TraceContext) func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		return &injectingTransport{
			prop: prop,
			next: next,
		}
	}
}

// injectingTransport injects trace context into outgoing HTTP requests.
type injectingTransport struct {
	prop *TraceContext
	next http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *injectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	spanCtx, ok := SpanContextFromContext(req.Context())
	if ok && spanCtx.IsValid() {
		t.prop.InjectIntoHTTP(spanCtx, req.Header)
	}
	return t.next.RoundTrip(req)
}