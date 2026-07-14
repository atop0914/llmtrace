package embedding

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Hit represents a search result from the Index.
type Hit struct {
	// ID is the unique identifier of the stored vector.
	ID string

	// Score is the similarity score (cosine similarity by default).
	Score float64

	// Metadata is the optional metadata stored with the vector.
	Metadata map[string]string
}

// Index is a thread-safe in-memory vector index for similarity search.
// It stores labeled vectors and supports k-nearest-neighbor queries.
//
// This is designed for moderate-scale use (thousands of vectors).
// For production use with millions of vectors, consider a dedicated
// vector database (Pinecone, Qdrant, Weaviate, etc.).
type Index struct {
	mu      sync.RWMutex
	vectors []indexEntry
	dims    int
}

type indexEntry struct {
	id       string
	vector   []float64
	metadata map[string]string
}

// NewIndex creates a new empty vector index.
func NewIndex() *Index {
	return &Index{}
}

// Add inserts a vector with the given ID into the index.
// Returns an error if the vector dimensions don't match existing entries.
func (idx *Index) Add(id string, vector []float64) error {
	return idx.AddWithMetadata(id, vector, nil)
}

// AddWithMetadata inserts a vector with the given ID and metadata.
func (idx *Index) AddWithMetadata(id string, vector []float64, metadata map[string]string) error {
	if len(vector) == 0 {
		return errors.New("embedding: empty vector")
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.dims == 0 {
		idx.dims = len(vector)
	} else if len(vector) != idx.dims {
		return fmt.Errorf("embedding: dimension mismatch: expected %d, got %d", idx.dims, len(vector))
	}

	// Copy vector to prevent external mutation
	vec := make([]float64, len(vector))
	copy(vec, vector)

	// Copy metadata
	var meta map[string]string
	if metadata != nil {
		meta = make(map[string]string, len(metadata))
		for k, v := range metadata {
			meta[k] = v
		}
	}

	idx.vectors = append(idx.vectors, indexEntry{
		id:       id,
		vector:   vec,
		metadata: meta,
	})
	return nil
}

// Remove deletes a vector by ID from the index.
// Returns true if the vector was found and removed.
func (idx *Index) Remove(id string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for i, entry := range idx.vectors {
		if entry.id == id {
			idx.vectors = append(idx.vectors[:i], idx.vectors[i+1:]...)
			return true
		}
	}
	return false
}

// Size returns the number of vectors in the index.
func (idx *Index) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.vectors)
}

// Dimensions returns the dimensionality of vectors in the index.
func (idx *Index) Dimensions() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.dims
}

// Get retrieves a vector by ID. Returns nil if not found.
func (idx *Index) Get(id string) ([]float64, map[string]string, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for _, entry := range idx.vectors {
		if entry.id == id {
			vec := make([]float64, len(entry.vector))
			copy(vec, entry.vector)
			return vec, entry.metadata, true
		}
	}
	return nil, nil, false
}

// Search finds the k most similar vectors to the query using cosine similarity.
// Returns hits sorted by score in descending order (most similar first).
func (idx *Index) Search(query []float64, k int) ([]Hit, error) {
	return idx.SearchWithMetric(query, k, CosineMetric)
}

// Metric defines a distance/similarity metric for search.
type Metric int

const (
	// CosineMetric uses cosine similarity (higher is better).
	CosineMetric Metric = iota
	// EuclideanMetric uses Euclidean distance (lower is better, returned as negative).
	EuclideanMetric
	// DotProductMetric uses dot product (higher is better).
	DotProductMetric
)

// SearchWithMetric finds the k most similar vectors using the specified metric.
func (idx *Index) SearchWithMetric(query []float64, k int, metric Metric) ([]Hit, error) {
	if len(query) == 0 {
		return nil, errors.New("embedding: empty query vector")
	}
	if k <= 0 {
		return nil, fmt.Errorf("embedding: k must be positive, got %d", k)
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.dims > 0 && len(query) != idx.dims {
		return nil, fmt.Errorf("embedding: query dimension mismatch: expected %d, got %d", idx.dims, len(query))
	}

	if len(idx.vectors) == 0 {
		return nil, nil
	}

	type scored struct {
		entry indexEntry
		score float64
	}

	scores := make([]scored, len(idx.vectors))
	for i, entry := range idx.vectors {
		var s float64
		switch metric {
		case CosineMetric:
			s, _ = CosineSimilarity(query, entry.vector)
		case EuclideanMetric:
			d, _ := EuclideanDistance(query, entry.vector)
			s = -d // negate so higher is better for sorting
		case DotProductMetric:
			s, _ = DotProduct(query, entry.vector)
		}
		scores[i] = scored{entry: entry, score: s}
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if k > len(scores) {
		k = len(scores)
	}

	hits := make([]Hit, k)
	for i := 0; i < k; i++ {
		score := scores[i].score
		if metric == EuclideanMetric {
			score = -score // restore positive distance
		}
		hits[i] = Hit{
			ID:       scores[i].entry.id,
			Score:    score,
			Metadata: scores[i].entry.metadata,
		}
	}

	return hits, nil
}

// Clear removes all vectors from the index.
func (idx *Index) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.vectors = nil
	idx.dims = 0
}

// IDs returns all stored vector IDs.
func (idx *Index) IDs() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := make([]string, len(idx.vectors))
	for i, entry := range idx.vectors {
		ids[i] = entry.id
	}
	return ids
}
