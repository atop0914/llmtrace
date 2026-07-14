// Package main demonstrates the embedding package for text embedding
// and similarity search using OpenAI's Embeddings API.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/atop0914/llmtrace/embedding"
)

func main() {
	ctx := context.Background()

	// Create an OpenAI embedding provider.
	// Replace with your actual API key.
	provider := embedding.NewOpenAI(
		embedding.WithAPIKey("sk-..."),
		embedding.WithModel("text-embedding-3-small"),
	)

	fmt.Println("=== Single Embedding ===")

	// Generate an embedding for a single text.
	result, err := provider.Embed(ctx, "The quick brown fox jumps over the lazy dog")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Model:     %s\n", result.Model)
	fmt.Printf("Dimensions: %d\n", len(result.Vector))
	fmt.Printf("Tokens:    %d\n", result.Usage.TotalTokens)
	fmt.Printf("First 5:   %v\n", result.Vector[:5])

	// Normalize for cosine similarity search
	normalized := result.Normalize()
	fmt.Printf("Norm:      %.6f\n", normalized.Norm())

	fmt.Println("\n=== Batch Embedding ===")

	// Generate embeddings for multiple texts at once.
	texts := []string{
		"Machine learning is a subset of artificial intelligence",
		"Deep learning uses neural networks with many layers",
		"Cats are popular household pets",
		"The weather is sunny today",
		"Natural language processing enables text understanding",
	}

	results, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		log.Fatal(err)
	}

	for i, r := range results {
		fmt.Printf("[%d] %s — %d tokens\n", i, texts[i][:40], r.Usage.TotalTokens)
	}

	fmt.Println("\n=== Similarity Search ===")

	// Build an in-memory index.
	index := embedding.NewIndex()
	for i, r := range results {
		id := fmt.Sprintf("doc_%d", i)
		index.AddWithMetadata(id, r.Vector, map[string]string{
			"text": texts[i],
		})
	}

	// Search for documents similar to a query.
	queryResult, err := provider.Embed(ctx, "What is AI?")
	if err != nil {
		log.Fatal(err)
	}

	hits, err := index.Search(queryResult.Vector, 3)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Query: 'What is AI?'")
	fmt.Println("Top 3 results:")
	for i, hit := range hits {
		fmt.Printf("  %d. [%s] score=%.4f — %s\n",
			i+1, hit.ID, hit.Score, hit.Metadata["text"])
	}

	fmt.Println("\n=== Direct Similarity Computation ===")

	a := []float64{1, 0, 0}
	b := []float64{0, 1, 0}
	c := []float64{0.7, 0.7, 0}

	simAB, _ := embedding.CosineSimilarity(a, b)
	simAC, _ := embedding.CosineSimilarity(a, c)
	simBC, _ := embedding.CosineSimilarity(b, c)

	fmt.Printf("sim(a,b) = %.4f (orthogonal)\n", simAB)
	fmt.Printf("sim(a,c) = %.4f (similar)\n", simAC)
	fmt.Printf("sim(b,c) = %.4f (similar)\n", simBC)

	distAB, _ := embedding.EuclideanDistance(a, b)
	fmt.Printf("dist(a,b) = %.4f\n", distAB)
}
