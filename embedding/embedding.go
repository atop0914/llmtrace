// Package embedding provides traced, observable text embedding operations
// for LLM applications. It defines a provider interface for generating
// vector embeddings from text, with built-in token tracking, batching,
// and similarity search utilities.
//
// Embeddings are the foundation of RAG pipelines, semantic search,
// document classification, and many other LLM-powered features.
//
// Usage:
//
//	provider := openaiemb.New(openaiemb.WithAPIKey("sk-..."))
//
//	// Single text
//	result, err := provider.Embed(ctx, "Hello world")
//
//	// Batch
//	results, err := provider.EmbedBatch(ctx, []string{"Hello", "World"})
//
//	// Similarity search
//	index := embedding.NewIndex()
//	index.Add("doc1", vec1)
//	hits := index.Search(queryVec, 5)
package embedding

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Provider generates vector embeddings from text inputs.
type Provider interface {
	// Embed generates an embedding vector for a single text input.
	Embed(ctx context.Context, text string) (*Result, error)

	// EmbedBatch generates embedding vectors for multiple text inputs
	// in a single API call. Providers may impose batch size limits.
	EmbedBatch(ctx context.Context, texts []string) ([]Result, error)

	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int

	// MaxBatchSize returns the maximum number of inputs per batch call.
	MaxBatchSize() int
}

// Result holds the embedding vector and associated metadata for one input.
type Result struct {
	// Vector is the embedding as a float64 slice.
	Vector []float64

	// Usage tracks token consumption for this embedding call.
	Usage Usage

	// Model is the embedding model that produced this vector.
	Model string

	// Index is the position in the batch (0 for single Embed calls).
	Index int
}

// Usage tracks token consumption for embedding calls.
type Usage struct {
	// PromptTokens is the number of tokens in the input text(s).
	PromptTokens int

	// TotalTokens is the total tokens consumed (same as PromptTokens
	// for embedding calls, which produce no output tokens).
	TotalTokens int
}

// Validate checks that the result has a non-empty vector.
func (r *Result) Validate() error {
	if len(r.Vector) == 0 {
		return errors.New("embedding: empty vector")
	}
	for i, v := range r.Vector {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("embedding: invalid value at index %d: %v", i, v)
		}
	}
	return nil
}

// Norm returns the L2 norm of the embedding vector.
func (r *Result) Norm() float64 {
	var sum float64
	for _, v := range r.Vector {
		sum += v * v
	}
	return math.Sqrt(sum)
}

// Normalize returns a new Result with a unit-length vector.
func (r *Result) Normalize() *Result {
	norm := r.Norm()
	if norm == 0 {
		return r
	}
	normalized := make([]float64, len(r.Vector))
	for i, v := range r.Vector {
		normalized[i] = v / norm
	}
	return &Result{
		Vector: normalized,
		Usage:  r.Usage,
		Model:  r.Model,
		Index:  r.Index,
	}
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value in [-1, 1] where 1 means identical direction,
// 0 means orthogonal, and -1 means opposite direction.
func CosineSimilarity(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding: dimension mismatch: %d vs %d", len(a), len(b))
	}
	if len(a) == 0 {
		return 0, errors.New("embedding: empty vectors")
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0, nil
	}
	return dot / denom, nil
}

// EuclideanDistance computes the L2 distance between two vectors.
func EuclideanDistance(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding: dimension mismatch: %d vs %d", len(a), len(b))
	}
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return math.Sqrt(sum), nil
}

// DotProduct computes the dot product of two vectors.
func DotProduct(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding: dimension mismatch: %d vs %d", len(a), len(b))
	}
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum, nil
}
