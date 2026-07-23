// Command adapters demonstrates the HTTP framework adapters package.
//
// The adapters middleware adds request correlation, OpenTelemetry tracing,
// and response metadata to any Go HTTP application.
//
// Usage:
//
//	go run ./examples/adapters
//
// Then test the endpoints:
//
//	curl -v http://localhost:8080/v1/chat
//	curl -v -H "X-Request-ID: custom-id-123" http://localhost:8080/v1/chat
//	curl -v http://localhost:8080/v1/models
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/atop0914/llmtrace/adapters"
)

func main() {
	mux := http.NewServeMux()

	// Create the middleware with default configuration.
	// Features:
	//   - Auto-generates X-Request-ID if not present
	//   - Creates OpenTelemetry spans per request
	//   - Adds response headers (X-Response-Time-Ms, X-LLM-Provider, X-Tokens-Used)
	//   - Recovers from panics with structured JSON errors
	mw := adapters.Middleware(adapters.DefaultConfig())

	// Chat completion handler — sets provider and token metadata.
	mux.HandleFunc("POST /v1/chat", func(w http.ResponseWriter, r *http.Request) {
		// Access request metadata from context
		data := adapters.RequestDataFromContext(r.Context())
		if data != nil {
			log.Printf("[%s] chat request received", data.RequestID)
		}

		// Set LLM-specific metadata (appears in response headers)
		adapters.SetProvider(r.Context(), "openai")
		adapters.SetModel(r.Context(), "gpt-4o-mini")
		adapters.SetTokensUsed(r.Context(), 150)

		// Simulate LLM response
		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Hello! How can I help you today?",
					},
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     50,
				"completion_tokens": 100,
				"total_tokens":      150,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Models list handler — minimal metadata.
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		data := adapters.RequestDataFromContext(r.Context())
		if data != nil {
			log.Printf("[%s] models list requested", data.RequestID)
		}

		resp := map[string]any{
			"data": []map[string]string{
				{"id": "gpt-4o", "owned_by": "openai"},
				{"id": "gpt-4o-mini", "owned_by": "openai"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Health endpoint (unwrapped — no middleware).
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Wrap all handlers with the middleware
	handler := mw(mux)

	addr := ":8080"
	fmt.Printf("HTTP Adapters example on %s\n", addr)
	fmt.Printf("  POST http://localhost%s/v1/chat\n", addr)
	fmt.Printf("  GET  http://localhost%s/v1/models\n", addr)
	fmt.Printf("  GET  http://localhost%s/health\n", addr)
	fmt.Println()
	fmt.Println("Response headers added:")
	fmt.Println("  X-Request-ID       — unique correlation ID")
	fmt.Println("  X-Response-Time-Ms  — request duration")
	fmt.Println("  X-LLM-Provider     — LLM provider name")
	fmt.Println("  X-Tokens-Used      — total tokens consumed")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
