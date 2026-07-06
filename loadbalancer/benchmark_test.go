package loadbalancer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atop0914/llmtrace"
)

func BenchmarkRoundRobin(b *testing.B) {
	eps := make([]*Endpoint, 5)
	for i := range eps {
		eps[i] = NewEndpoint("ep", &mockProvider{
			name:  "bench",
			model: "test",
			completeFunc: func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
				return &llmtrace.Response{Content: "ok"}, nil
			},
		})
	}

	lb := New(
		WithStrategy(RoundRobin),
		WithEndpoints(eps...),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.Complete(ctx, req)
	}
}

func BenchmarkLeastLatency(b *testing.B) {
	eps := make([]*Endpoint, 5)
	for i := range eps {
		eps[i] = NewEndpoint("ep", &mockProvider{
			name:  "bench",
			model: "test",
			completeFunc: func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
				return &llmtrace.Response{Content: "ok"}, nil
			},
		})
	}

	lb := New(
		WithStrategy(LeastLatency),
		WithEndpoints(eps...),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.Complete(ctx, req)
	}
}

func BenchmarkWeighted(b *testing.B) {
	eps := make([]*Endpoint, 5)
	for i := range eps {
		eps[i] = &Endpoint{
			Name:     "ep",
			Provider: &mockProvider{name: "bench", model: "test"},
			Weight:   i + 1,
			healthy:  true,
		}
	}

	lb := New(
		WithStrategy(Weighted),
		WithEndpoints(eps...),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	req := testRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.Complete(ctx, req)
	}
}

func BenchmarkConcurrent(b *testing.B) {
	eps := make([]*Endpoint, 4)
	for i := range eps {
		eps[i] = NewEndpoint("ep", &mockProvider{
			name:  "bench",
			model: "test",
			completeFunc: func(_ context.Context, _ *llmtrace.Request) (*llmtrace.Response, error) {
				return &llmtrace.Response{Content: "ok"}, nil
			},
		})
	}

	lb := New(
		WithStrategy(RoundRobin),
		WithEndpoints(eps...),
		WithHealthCheckInterval(0),
	)
	defer lb.Stop()

	ctx := context.Background()
	req := testRequest()

	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lb.Complete(ctx, req)
			counter.Add(1)
		}
	})
}

func BenchmarkEndpointRecordSuccess(b *testing.B) {
	ep := NewEndpoint("bench", &mockProvider{name: "bench", model: "test"})
	lat := 50 * time.Millisecond

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ep.recordSuccess(lat)
	}
}

func BenchmarkEndpointRecordError(b *testing.B) {
	ep := NewEndpoint("bench", &mockProvider{name: "bench", model: "test"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ep.recordError()
		// Reset to stay healthy
		if i%3 == 0 {
			ep.recordSuccess(time.Millisecond)
		}
	}
}
