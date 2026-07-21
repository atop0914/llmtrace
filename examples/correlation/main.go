// Package main demonstrates using the correlation package for
// request/response correlation IDs with downstream propagation.
//
// Run this example:
//
//	go run ./examples/correlation/
//
// Then test the endpoints:
//
//	curl -v http://localhost:8080/api/hello
//	curl -v -H "X-Request-ID: my-custom-id" http://localhost:8080/api/hello
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/atop0914/llmtrace/correlation"
)

func main() {
	// Create correlation middleware with default config
	corr := correlation.New(correlation.DefaultConfig())

	mux := http.NewServeMux()

	// API handler that uses the correlation ID
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		id := correlation.IDFromContext(r.Context())

		resp := map[string]string{
			"message":       "Hello from LLMTrace!",
			"correlation_id": id,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Another handler demonstrating log correlation
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		id := correlation.IDFromContext(r.Context())
		log.Printf("[%s] status check requested", id)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":        "ok",
			"correlation_id": id,
		})
	})

	// Wrap the entire mux with correlation middleware
	handler := corr.Middleware(mux)

	addr := ":8080"
	fmt.Printf("Starting correlation ID example on %s\n", addr)
	fmt.Printf("  Try: curl -v http://localhost%s/api/hello\n", addr)
	fmt.Printf("  Try: curl -v -H \"X-Request-ID: custom-123\" http://localhost%s/api/hello\n", addr)

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
