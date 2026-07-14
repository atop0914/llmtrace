// Command semcache demonstrates semantic caching for LLM responses.
//
// Semantic caching uses embedding similarity to find cached responses for
// semantically equivalent queries, reducing API calls and costs.
//
// Usage:
//
//	export OPENAI_API_KEY="***"
//	go run ./examples/semcache
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/atop0914/llmtrace"
	"github.com/atop0914/llmtrace/provider/openai"
	"github.com/atop0914/llmtrace/semcache"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func main() {
	// Set up OpenTelemetry.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("Set OPENAI_API_KEY environment variable")
	}

	provider := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithModel("gpt-4o-mini"),
	)

	tracer := llmtrace.NewTracer("semcache-demo",
		llmtrace.WithProvider("openai"),
	)

	// Create semantic cache with Bag-of-Words embedder.
	// For production, replace with a real embedding model (OpenAI, Cohere, etc.).
	cache := semcache.New(semcache.Config{
		Embedder:   semcache.NewBowEmbedder(256),
		Threshold:  0.75, // 75% similarity required for a cache hit
		MaxEntries: 100,
		TTL:        30 * time.Minute,
		OnHit: func(query string, sim float64, cached any) {
			fmt.Printf("  [CACHE HIT] similarity=%.4f\n", sim)
		},
		OnMiss: func(query string, bestSim float64) {
			fmt.Printf("  [CACHE MISS] best similarity=%.4f\n", bestSim)
		},
	})

	ctx := context.Background()

	// First query — cache miss, calls the LLM.
	queries := []string{
		"What is the Go programming language?",
		"Tell me about the Go programming language",     // semantically similar
		"Explain Go language to me",                      // another variant
		"How do I make a chocolate cake?",                // completely different
		"What's the recipe for chocolate cake?",          // similar to previous
	}

	for i, q := range queries {
		fmt.Printf("\n--- Query %d: %q ---\n", i+1, q)
		req := &llmtrace.Request{
			Model: "gpt-4o-mini",
			Messages: []llmtrace.Message{
				{Role: llmtrace.RoleUser, Content: q},
			},
			MaxTokens: llmtrace.IntPtr(100),
		}

		resp, err := tracer.Chat(ctx, req, provider,
			llmtrace.WithCallMiddleware(semcache.WithSemanticCache(cache)),
		)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		// Truncate for display
		content := resp.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("  Response: %s\n", content)
	}

	// Show cache stats.
	stats := cache.Stats()
	fmt.Printf("\n--- Cache Stats ---\n")
	fmt.Printf("Hits: %d, Misses: %d, Hit Rate: %.1f%%\n",
		stats.Hits, stats.Misses, stats.HitRate()*100)
	fmt.Printf("Cached entries: %d, Avg hit similarity: %.4f\n",
		stats.Size, stats.AvgScore)

	// Show top matches for a query.
	fmt.Printf("\n--- Top 3 matches for 'Go language tutorial' ---\n")
	matches := cache.TopK("Go language tutorial", 3)
	for _, m := range matches {
		resp := m.Response.(*llmtrace.Response)
		snippet := resp.Content
		if len(snippet) > 60 {
			snippet = snippet[:60] + "..."
		}
		fmt.Printf("  [%.4f] %q → %s\n", m.Similarity, m.Query, snippet)
	}

	// Clean up expired entries.
	purged := cache.Purge()
	fmt.Printf("\nPurged %d expired entries\n", purged)

	_ = strings.Join(queries, ", ")
}
