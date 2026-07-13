package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Result tests ---

func TestResult_Validate_OK(t *testing.T) {
	r := &Result{Vector: []float64{1, 2, 3}}
	if err := r.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestResult_Validate_Empty(t *testing.T) {
	r := &Result{Vector: nil}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty vector")
	}
}

func TestResult_Validate_NaN(t *testing.T) {
	r := &Result{Vector: []float64{1, math.NaN(), 3}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for NaN")
	}
}

func TestResult_Validate_Inf(t *testing.T) {
	r := &Result{Vector: []float64{1, math.Inf(1), 3}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for Inf")
	}
}

func TestResult_Norm(t *testing.T) {
	r := &Result{Vector: []float64{3, 4}}
	got := r.Norm()
	if diff := got - 5.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Norm() = %f, want 5.0", got)
	}
}

func TestResult_Norm_Zero(t *testing.T) {
	r := &Result{Vector: []float64{0, 0, 0}}
	got := r.Norm()
	if diff := got - 0.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Norm() = %f, want 0.0", got)
	}
}

func TestResult_Normalize(t *testing.T) {
	r := &Result{
		Vector: []float64{3, 4},
		Model:  "test",
		Index:  2,
	}
	n := r.Normalize()

	norm := n.Norm()
	if diff := norm - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Normalize() norm = %f, want 1.0", norm)
	}
	if n.Model != "test" {
		t.Errorf("Model = %q, want %q", n.Model, "test")
	}
	if n.Index != 2 {
		t.Errorf("Index = %d, want 2", n.Index)
	}
}

func TestResult_Normalize_ZeroVector(t *testing.T) {
	r := &Result{Vector: []float64{0, 0, 0}}
	n := r.Normalize()
	if len(n.Vector) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(n.Vector))
	}
}

// --- CosineSimilarity tests ---

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := sim - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want 1.0", sim)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{-1, 0, 0}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := sim - (-1.0); diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want -1.0", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{0, 1}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := sim - 0.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want 0.0", sim)
	}
}

func TestCosineSimilarity_DimMismatch(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{1, 0, 0}
	_, err := CosineSimilarity(a, b)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	_, err := CosineSimilarity(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty vectors")
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float64{0, 0}
	b := []float64{1, 0}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if sim != 0 {
		t.Errorf("got %f, want 0", sim)
	}
}

func TestCosineSimilarity_KnownValues(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{4, 5, 6}
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: (1*4+2*5+3*6) / (sqrt(14)*sqrt(77)) = 32 / 32.8024... ≈ 0.974632
	expected := 32.0 / (math.Sqrt(14) * math.Sqrt(77))
	if diff := sim - expected; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("got %f, want %f", sim, expected)
	}
}

// --- EuclideanDistance tests ---

func TestEuclideanDistance_Zero(t *testing.T) {
	a := []float64{1, 2, 3}
	d, err := EuclideanDistance(a, a)
	if err != nil {
		t.Fatal(err)
	}
	if diff := d - 0.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want 0.0", d)
	}
}

func TestEuclideanDistance_Known(t *testing.T) {
	a := []float64{0, 0}
	b := []float64{3, 4}
	d, err := EuclideanDistance(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := d - 5.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want 5.0", d)
	}
}

func TestEuclideanDistance_Mismatch(t *testing.T) {
	_, err := EuclideanDistance([]float64{1}, []float64{1, 2})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

// --- DotProduct tests ---

func TestDotProduct_Known(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{4, 5, 6}
	dp, err := DotProduct(a, b)
	if err != nil {
		t.Fatal(err)
	}
	// 1*4 + 2*5 + 3*6 = 32
	if diff := dp - 32.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %f, want 32.0", dp)
	}
}

func TestDotProduct_Mismatch(t *testing.T) {
	_, err := DotProduct([]float64{1}, []float64{1, 2})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

// --- Index tests ---

func TestIndex_AddAndSearch(t *testing.T) {
	idx := NewIndex()
	if err := idx.Add("a", []float64{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("b", []float64{0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("c", []float64{0.7, 0.7, 0}); err != nil {
		t.Fatal(err)
	}

	if idx.Size() != 3 {
		t.Fatalf("Size() = %d, want 3", idx.Size())
	}

	hits, err := idx.Search([]float64{1, 0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].ID != "a" {
		t.Errorf("top hit = %q, want %q", hits[0].ID, "a")
	}
	if hits[1].ID != "c" {
		t.Errorf("second hit = %q, want %q", hits[1].ID, "c")
	}
}

func TestIndex_EmptySearch(t *testing.T) {
	idx := NewIndex()
	hits, err := idx.Search([]float64{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for empty index, got %d", len(hits))
	}
}

func TestIndex_SearchWithMetadata(t *testing.T) {
	idx := NewIndex()
	idx.AddWithMetadata("doc1", []float64{1, 0}, map[string]string{"source": "web"})
	idx.AddWithMetadata("doc2", []float64{0, 1}, map[string]string{"source": "api"})

	hits, err := idx.Search([]float64{1, 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].Metadata["source"] != "web" {
		t.Errorf("metadata source = %q, want %q", hits[0].Metadata["source"], "web")
	}
}

func TestIndex_Remove(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	idx.Add("b", []float64{0, 1})

	if !idx.Remove("a") {
		t.Fatal("expected Remove to return true")
	}
	if idx.Size() != 1 {
		t.Fatalf("Size() = %d, want 1", idx.Size())
	}
	if idx.Remove("nonexistent") {
		t.Fatal("expected Remove to return false for nonexistent ID")
	}
}

func TestIndex_Get(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 2, 3})

	vec, meta, ok := idx.Get("a")
	if !ok {
		t.Fatal("expected Get to find entry")
	}
	if len(vec) != 3 || vec[0] != 1 || vec[1] != 2 || vec[2] != 3 {
		t.Errorf("vector = %v", vec)
	}
	if meta != nil {
		t.Errorf("expected nil metadata, got %v", meta)
	}

	_, _, ok = idx.Get("nonexistent")
	if ok {
		t.Fatal("expected Get to return false for nonexistent ID")
	}
}

func TestIndex_DimensionMismatch(t *testing.T) {
	idx := NewIndex()
	if err := idx.Add("a", []float64{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("b", []float64{1, 0, 0}); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestIndex_Clear(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	idx.Add("b", []float64{0, 1})

	idx.Clear()
	if idx.Size() != 0 {
		t.Fatalf("Size() = %d after Clear(), want 0", idx.Size())
	}
	if idx.Dimensions() != 0 {
		t.Fatalf("Dimensions() = %d after Clear(), want 0", idx.Dimensions())
	}
}

func TestIndex_IDs(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	idx.Add("b", []float64{0, 1})

	ids := idx.IDs()
	if len(ids) != 2 {
		t.Fatalf("got %d IDs, want 2", len(ids))
	}
	if ids[0] != "a" || ids[1] != "b" {
		t.Errorf("IDs = %v", ids)
	}
}

func TestIndex_SearchK(t *testing.T) {
	idx := NewIndex()
	for i := 0; i < 10; i++ {
		idx.Add(fmt.Sprintf("v%d", i), []float64{float64(i), float64(10 - i)})
	}

	hits, err := idx.Search([]float64{1, 9}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3", len(hits))
	}
	// v1 = [1,9] should be closest to query [1,9]
	if hits[0].ID != "v1" {
		t.Errorf("top hit = %q, want %q", hits[0].ID, "v1")
	}
}

func TestIndex_SearchKGreaterThanSize(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	idx.Add("b", []float64{0, 1})

	hits, err := idx.Search([]float64{1, 0}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
}

func TestIndex_SearchMetrics(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	idx.Add("b", []float64{0, 1})

	// Euclidean: a=[1,0] is exact match to query [1,0], distance=0; b=[0,1] distance=sqrt(2)
	hits, err := idx.SearchWithMetric([]float64{1, 0}, 2, EuclideanMetric)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].ID != "a" {
		t.Errorf("euclidean top = %q, want %q", hits[0].ID, "a")
	}
	if hits[0].Score != 0 {
		t.Errorf("euclidean score = %f, want 0 (exact match)", hits[0].Score)
	}

	// DotProduct
	hits, err = idx.SearchWithMetric([]float64{1, 0}, 2, DotProductMetric)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].ID != "a" {
		t.Errorf("dotproduct top = %q, want %q", hits[0].ID, "a")
	}
}

func TestIndex_SearchEmptyQuery(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	_, err := idx.Search(nil, 1)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestIndex_SearchInvalidK(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	_, err := idx.Search([]float64{1, 0}, 0)
	if err == nil {
		t.Fatal("expected error for k=0")
	}
}

func TestIndex_AddEmptyVector(t *testing.T) {
	idx := NewIndex()
	if err := idx.Add("a", nil); err == nil {
		t.Fatal("expected error for empty vector")
	}
}

func TestIndex_SearchQueryDimMismatch(t *testing.T) {
	idx := NewIndex()
	idx.Add("a", []float64{1, 0})
	_, err := idx.Search([]float64{1, 0, 0}, 1)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

// --- OpenAI provider tests (with mock server) ---

func newMockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestOpenAI_Embed(t *testing.T) {
	server := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		var req openaiEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		resp := openaiEmbedResponse{
			Data: []openaiEmbedData{
				{Embedding: []float64{0.1, 0.2, 0.3}, Index: 0},
			},
			Usage: openaiUsage{PromptTokens: 5, TotalTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := NewOpenAI(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithDimensions(3),
	)

	result, err := p.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Vector) != 3 {
		t.Fatalf("vector length = %d, want 3", len(result.Vector))
	}
	if result.Usage.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", result.Usage.PromptTokens)
	}
	if result.Model == "" {
		t.Error("expected non-empty model")
	}
}

func TestOpenAI_EmbedBatch(t *testing.T) {
	server := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req openaiEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)

		inputs := req.Input.([]any)
		data := make([]openaiEmbedData, len(inputs))
		for i := range inputs {
			data[i] = openaiEmbedData{
				Embedding: []float64{float64(i), float64(i + 1)},
				Index:     i,
			}
		}

		resp := openaiEmbedResponse{
			Data:  data,
			Usage: openaiUsage{PromptTokens: 10, TotalTokens: 10},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	p := NewOpenAI(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithDimensions(2),
	)

	results, err := p.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Index != 0 || results[1].Index != 1 || results[2].Index != 2 {
		t.Errorf("unexpected indices: %d, %d, %d", results[0].Index, results[1].Index, results[2].Index)
	}
}

func TestOpenAI_EmbedBatch_Empty(t *testing.T) {
	p := NewOpenAI(WithAPIKey("test-key"))
	_, err := p.EmbedBatch(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestOpenAI_APIError(t *testing.T) {
	server := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": "invalid api key"}`)
	})
	defer server.Close()

	p := NewOpenAI(
		WithAPIKey("bad-key"),
		WithBaseURL(server.URL),
	)

	_, err := p.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain status code: %v", err)
	}
}

func TestOpenAI_ContextCancellation(t *testing.T) {
	server := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		select {
		case <-r.Context().Done():
			return
		default:
		}
	})
	defer server.Close()

	p := NewOpenAI(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.Embed(ctx, "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOpenAI_MaxBatchSize(t *testing.T) {
	p := NewOpenAI(WithMaxBatchSize(100))
	if p.MaxBatchSize() != 100 {
		t.Errorf("MaxBatchSize() = %d, want 100", p.MaxBatchSize())
	}
}

func TestOpenAI_Dimensions(t *testing.T) {
	p := NewOpenAI(WithDimensions(256))
	if p.Dimensions() != 256 {
		t.Errorf("Dimensions() = %d, want 256", p.Dimensions())
	}
}

func TestOpenAI_Model(t *testing.T) {
	p := NewOpenAI(WithModel("text-embedding-3-large"))
	if p.Model() != "text-embedding-3-large" {
		t.Errorf("Model() = %q, want %q", p.Model(), "text-embedding-3-large")
	}
}

func TestOpenAI_EmbedBatch_AutoChunk(t *testing.T) {
	callCount := 0
	totalInputs := 0

	server := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req openaiEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		inputs := req.Input.([]any)
		totalInputs += len(inputs)

		data := make([]openaiEmbedData, len(inputs))
		for i := range inputs {
			data[i] = openaiEmbedData{
				Embedding: []float64{0.1},
				Index:     i,
			}
		}
		json.NewEncoder(w).Encode(openaiEmbedResponse{
			Data:  data,
			Usage: openaiUsage{PromptTokens: len(inputs), TotalTokens: len(inputs)},
		})
	})
	defer server.Close()

	p := NewOpenAI(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithMaxBatchSize(3),
		WithDimensions(1),
	)

	texts := make([]string, 7)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}

	results, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 7 {
		t.Fatalf("got %d results, want 7", len(results))
	}
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3 (7 items / batch size 3)", callCount)
	}
	if totalInputs != 7 {
		t.Errorf("totalInputs = %d, want 7", totalInputs)
	}
}
