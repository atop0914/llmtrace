package embedding

import (
	"fmt"
	"testing"
)

func BenchmarkCosineSimilarity_Dim1536(b *testing.B) {
	a := make([]float64, 1536)
	c := make([]float64, 1536)
	for i := range a {
		a[i] = float64(i%100) * 0.01
		c[i] = float64((i+50)%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CosineSimilarity(a, c)
	}
}

func BenchmarkCosineSimilarity_Dim256(b *testing.B) {
	a := make([]float64, 256)
	c := make([]float64, 256)
	for i := range a {
		a[i] = float64(i%100) * 0.01
		c[i] = float64((i+50)%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CosineSimilarity(a, c)
	}
}

func BenchmarkEuclideanDistance_Dim1536(b *testing.B) {
	a := make([]float64, 1536)
	c := make([]float64, 1536)
	for i := range a {
		a[i] = float64(i%100) * 0.01
		c[i] = float64((i+50)%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EuclideanDistance(a, c)
	}
}

func BenchmarkDotProduct_Dim1536(b *testing.B) {
	a := make([]float64, 1536)
	c := make([]float64, 1536)
	for i := range a {
		a[i] = float64(i%100) * 0.01
		c[i] = float64((i+50)%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DotProduct(a, c)
	}
}

func BenchmarkIndex_Add(b *testing.B) {
	idx := NewIndex()
	vec := make([]float64, 1536)
	for i := range vec {
		vec[i] = float64(i) * 0.001
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Add(fmt.Sprintf("doc_%d", i), vec)
	}
}

func BenchmarkIndex_Search_1000(b *testing.B) {
	idx := NewIndex()
	query := make([]float64, 1536)
	for i := 0; i < 1000; i++ {
		vec := make([]float64, 1536)
		for j := range vec {
			vec[j] = float64((i+j)%100) * 0.01
		}
		idx.Add(fmt.Sprintf("doc_%d", i), vec)
	}
	for i := range query {
		query[i] = float64(i%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Search(query, 10)
	}
}

func BenchmarkIndex_Search_10000(b *testing.B) {
	idx := NewIndex()
	query := make([]float64, 1536)
	for i := 0; i < 10000; i++ {
		vec := make([]float64, 1536)
		for j := range vec {
			vec[j] = float64((i+j)%100) * 0.01
		}
		idx.Add(fmt.Sprintf("doc_%d", i), vec)
	}
	for i := range query {
		query[i] = float64(i%100) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Search(query, 10)
	}
}

func BenchmarkResult_Normalize(b *testing.B) {
	r := &Result{Vector: make([]float64, 1536)}
	for i := range r.Vector {
		r.Vector[i] = float64(i) * 0.001
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Normalize()
	}
}
