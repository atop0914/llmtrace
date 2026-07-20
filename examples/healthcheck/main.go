// Package main demonstrates using the healthcheck package for
// Kubernetes-style liveness and readiness probes.
//
// Run this example:
//
//	go run ./examples/healthcheck/
//
// Then test the endpoints:
//
//	curl http://localhost:8080/healthz
//	curl http://localhost:8080/readyz
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/atop0914/llmtrace/healthcheck"
)

func main() {
	hc := healthcheck.New(healthcheck.Config{
		Timeout:         3 * time.Second,
		AggregateStatus: true,
	})

	// Register readiness checks
	hc.AddReadinessCheck("database", func(ctx context.Context) error {
		// Simulate a database ping
		// In production: return db.PingContext(ctx)
		return nil
	})

	hc.AddReadinessCheck("redis", func(ctx context.Context) error {
		// Simulate a Redis ping
		// In production: return redisClient.Ping(ctx).Err()
		return nil
	})

	hc.AddReadinessCheck("external-api", func(ctx context.Context) error {
		// Simulate checking an external dependency
		// In production: resp, err := http.Get("https://api.example.com/health")
		return nil
	})

	mux := http.NewServeMux()

	// Mount health check endpoints
	mux.HandleFunc("/healthz", hc.LiveHandler)
	mux.HandleFunc("/readyz", hc.ReadyHandler)

	// Your application routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "LLMTrace is running! Uptime: %s\n", hc.Uptime())
	})

	addr := ":8080"
	fmt.Printf("Starting server on %s\n", addr)
	fmt.Printf("  Liveness:  http://localhost%s/healthz\n", addr)
	fmt.Printf("  Readiness: http://localhost%s/readyz\n", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
