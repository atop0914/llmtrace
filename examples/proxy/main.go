// Command proxy demonstrates the OpenAI-compatible local proxy server.
//
// This proxy accepts requests in OpenAI API format and routes them through
// LLMTrace providers with full observability (tracing, metrics, logging).
//
// Usage:
//
//	export OPENAI_API_KEY="sk-..."
//	export ANTHROPIC_API_KEY="sk-ant-..."
//	go run ./examples/proxy
//
// Then use any OpenAI-compatible client:
//
//	curl http://localhost:8080/v1/chat/completions \
//	  -H "Authorization: Bearer my-proxy-key" \
//	  -H "Content-Type: application/json" \
//	  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello!"}]}'
//
//	# Streaming
//	curl http://localhost:8080/v1/chat/completions \
//	  -H "Authorization: Bearer my-proxy-key" \
//	  -H "Content-Type: application/json" \
//	  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello!"}],"stream":true}'
//
//	# List models
//	curl http://localhost:8080/v1/models
//
//	# Health check
//	curl http://localhost:8080/health
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/proxy"
	"github.com/atop0914/llmtrace/provider/anthropic"
	"github.com/atop0914/llmtrace/provider/openai"
)

func main() {
	// Create providers.
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	if openaiKey == "" && anthropicKey == "" {
		log.Fatal("Set OPENAI_API_KEY and/or ANTHROPIC_API_KEY environment variables")
	}

	providers := map[string]proxy.ProviderEntry{}

	if openaiKey != "" {
		providers["gpt"] = proxy.ProviderEntry{
			Provider: openai.New(openai.WithAPIKey(openaiKey)),
			Default:  true,
		}
		fmt.Println("✓ Registered OpenAI provider (gpt-*)")
	}

	if anthropicKey != "" {
		providers["claude"] = proxy.ProviderEntry{
			Provider: anthropic.New(anthropic.WithAPIKey(anthropicKey)),
		}
		fmt.Println("✓ Registered Anthropic provider (claude-*)")
	}

	// Create the proxy server with observability.
	srv := proxy.New(proxy.Config{
		Listen:    ":8080",
		Providers: providers,
		Tracer:    llmtrace.NewTracer("llmtrace-proxy"),
		APIKey:    "my-proxy-key", // Clients must send: Authorization: Bearer my-proxy-key
	})

	fmt.Println("\n🚀 LLMTrace Proxy listening on :8080")
	fmt.Println("   API Key: my-proxy-key")
	fmt.Println("\nEndpoints:")
	fmt.Println("  POST /v1/chat/completions  — Chat completions (streaming & non-streaming)")
	fmt.Println("  GET  /v1/models            — List available models")
	fmt.Println("  GET  /health               — Health check (no auth required)")

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
