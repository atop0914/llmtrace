package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkSend(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL}},
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Send(ctx, EventCustom, map[string]any{"key": "value"})
	}
}

func BenchmarkSend_WithHMAC(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewAlerter(Config{
		Endpoints: []Endpoint{{URL: server.URL, Secret: "benchmark-secret"}},
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Send(ctx, EventCustom, map[string]any{"key": "value"})
	}
}

func BenchmarkSend_MultipleEndpoints(b *testing.B) {
	servers := make([]*httptest.Server, 3)
	endpoints := make([]Endpoint, 3)
	for i := range servers {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer servers[i].Close()
		endpoints[i] = Endpoint{URL: servers[i].URL}
	}

	a := NewAlerter(Config{Endpoints: endpoints})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Send(ctx, EventCustom, nil)
	}
}

func BenchmarkComputeHMAC(b *testing.B) {
	payload := []byte(`{"type":"benchmark","message":"performance test payload"}`)
	secret := "benchmark-secret-key"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeHMAC(payload, secret)
	}
}

func BenchmarkExponentialBackoff(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExponentialBackoff(5, 100000000, 30000000000, 2.0)
	}
}
