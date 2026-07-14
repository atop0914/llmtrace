// Package main demonstrates W3C Trace Context propagation with LLMTrace.
//
// This example shows how to propagate distributed trace context across
// service boundaries using the propagation package.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/atop0914/llmtrace/propagation"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	prop := propagation.New()

	fmt.Println("=== W3C Trace Context Propagation ===")
	fmt.Println()

	// --- 1. Manual Inject/Extract ---
	fmt.Println("--- Manual Inject & Extract ---")

	// Create a span context (normally from an active OTel span)
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	// Inject into HTTP headers
	headers := http.Header{}
	prop.InjectIntoHTTP(spanCtx, headers)
	fmt.Printf("  traceparent: %s\n", headers.Get("Traceparent"))

	// Extract back from headers
	extracted, ok := prop.ExtractFromHTTP(headers)
	if !ok {
		log.Fatal("failed to extract trace context")
	}
	fmt.Printf("  Extracted TraceID: %s\n", extracted.TraceID())
	fmt.Printf("  Extracted SpanID:  %s\n", extracted.SpanID())
	fmt.Printf("  Sampled:           %v\n", extracted.IsSampled())
	fmt.Println()

	// --- 2. MapCarrier for non-HTTP transports ---
	fmt.Println("--- MapCarrier (gRPC, custom transports) ---")

	carrier := propagation.MapCarrier{}
	prop.InjectIntoMap(spanCtx, carrier)
	fmt.Printf("  traceparent: %s\n", carrier["Traceparent"])

	extractedMap, ok := prop.ExtractFromMap(carrier)
	if ok {
		fmt.Printf("  Extracted TraceID: %s\n", extractedMap.TraceID())
	}
	fmt.Println()

	// --- 3. HTTP Middleware ---
	fmt.Println("--- HTTP Middleware ---")

	// Downstream service: extract trace context from incoming request
	downstreamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanCtx, ok := propagation.SpanContextFromContext(r.Context())
		if ok {
			fmt.Printf("  [downstream] Received TraceID: %s\n", spanCtx.TraceID())
			fmt.Printf("  [downstream] Received SpanID:  %s\n", spanCtx.SpanID())
		}
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with propagation middleware to auto-extract trace context
	downstreamServer := httptest.NewServer(propagation.Middleware(prop)(downstreamHandler))
	defer downstreamServer.Close()

	// Simulate upstream service injecting trace context into outgoing request
	injectingClient := &http.Client{
		Transport: propagation.ClientMiddleware(prop)(
			http.DefaultTransport,
		),
	}

	// Build request with trace context in Go context
	ctx := propagation.ContextWithSpanContext(context.Background(), spanCtx)
	req, _ := http.NewRequestWithContext(ctx, "GET", downstreamServer.URL+"/api", nil)

	resp, err := injectingClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()
	fmt.Printf("  [upstream]   Status: %d\n", resp.StatusCode)
	fmt.Println()

	// --- 4. Format helpers ---
	fmt.Println("--- Format Helpers ---")
	formatted, _ := propagation.FormatTraceParent(traceID, spanID, true)
	fmt.Printf("  Formatted traceparent: %s\n", formatted)
	fmt.Println()

	// --- 5. Tracestate preservation ---
	fmt.Println("--- Tracestate Preservation ---")
	headersWithState := http.Header{
		"Traceparent": {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		"Tracestate":  {"vendor1=value1,vendor2=value2"},
	}
	extractedWithState, ok := prop.ExtractFromHTTP(headersWithState)
	if ok {
		fmt.Printf("  TraceID: %s\n", extractedWithState.TraceID())
	}

	// Re-inject preserves tracestate
	outHeaders := http.Header{}
	prop.InjectIntoHTTP(extractedWithState, outHeaders)
	fmt.Printf("  Re-injected tracestate: %s\n", outHeaders.Get("Tracestate"))

	fmt.Println()
	fmt.Println("=== Done ===")
}
