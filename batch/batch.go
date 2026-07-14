// Package batch provides concurrent async batch execution for LLM requests.
//
// It enables sending multiple LLM requests concurrently with configurable
// parallelism, aggregate metrics collection, and per-item error handling.
//
// Usage:
//
//	b := batch.New(provider, batch.WithMaxConcurrency(5))
//	results, err := b.Run(ctx, []*llmtrace.Request{req1, req2, req3})
//	for _, r := range results.Items {
//	    if r.Error != nil { ... }
//	    fmt.Println(r.Response.Content)
//	}
//	fmt.Println("Total tokens:", results.Metrics.TotalTokens)
package batch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atop0914/llmtrace"
)

// ErrorHandling controls how the batch handles individual item failures.
type ErrorHandling int

const (
	// ErrorContinue continues processing remaining items after a failure.
	// This is the default behavior.
	ErrorContinue ErrorHandling = iota

	// ErrorCancel cancels the entire batch on the first failure.
	ErrorCancel
)

// Config holds configuration for batch execution.
type Config struct {
	// MaxConcurrency is the maximum number of concurrent requests.
	// 0 or negative means unlimited concurrency.
	MaxConcurrency int

	// Timeout is the overall timeout for the entire batch.
	// 0 means no timeout (inherits parent context).
	Timeout time.Duration

	// PerItemTimeout is the timeout for each individual request.
	// 0 means no per-item timeout.
	PerItemTimeout time.Duration

	// OnError controls error handling strategy.
	OnError ErrorHandling

	// OnProgress is called after each item completes (success or failure).
	// May be called concurrently from multiple goroutines.
	OnProgress func(item int, result *Result)

	// Name is an optional identifier for the batch (used in tracing).
	Name string
}

// Option configures a batch.
type Option func(*Config)

// WithMaxConcurrency sets the maximum number of concurrent requests.
func WithMaxConcurrency(n int) Option {
	return func(c *Config) {
		c.MaxConcurrency = n
	}
}

// WithTimeout sets the overall batch timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.Timeout = d
	}
}

// WithPerItemTimeout sets the timeout for each individual request.
func WithPerItemTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.PerItemTimeout = d
	}
}

// WithErrorHandling sets the error handling strategy.
func WithErrorHandling(h ErrorHandling) Option {
	return func(c *Config) {
		c.OnError = h
	}
}

// WithOnProgress sets a progress callback called after each item completes.
func WithOnProgress(fn func(item int, result *Result)) Option {
	return func(c *Config) {
		c.OnProgress = fn
	}
}

// WithName sets an identifier for the batch.
func WithName(name string) Option {
	return func(c *Config) {
		c.Name = name
	}
}

// Item represents a single request in a batch.
type Item struct {
	// Request is the LLM request.
	Request *llmtrace.Request

	// Metadata is user-defined data attached to this item.
	// It is passed through to the corresponding Result unchanged.
	Metadata any
}

// Result represents the outcome of a single batch item.
type Result struct {
	// Index is the position of this item in the original batch.
	Index int

	// Response is the LLM response (nil if Error is set).
	Response *llmtrace.Response

	// Error is the error that occurred (nil on success).
	Error error

	// Latency is the duration of this individual request.
	Latency time.Duration

	// Metadata is the user-defined data from the corresponding Item.
	Metadata any
}

// Metrics contains aggregate statistics for the entire batch.
type Metrics struct {
	// TotalRequests is the number of items in the batch.
	TotalRequests int

	// Successful is the number of items that completed without error.
	Successful int

	// Failed is the number of items that returned an error.
	Failed int

	// TotalTokens is the sum of all token usage across the batch.
	TotalTokens int

	// InputTokens is the sum of input tokens across the batch.
	InputTokens int

	// OutputTokens is the sum of output tokens across the batch.
	OutputTokens int

	// TotalLatency is the wall-clock time for the entire batch.
	TotalLatency time.Duration

	// AvgLatency is the average per-item latency.
	AvgLatency time.Duration

	// MaxLatency is the slowest individual request.
	MaxLatency time.Duration

	// MinLatency is the fastest individual request (success only).
	MinLatency time.Duration
}

// Response contains the full batch execution results.
type Response struct {
	// Items contains results in the same order as the input requests.
	Items []*Result

	// Metrics contains aggregate statistics.
	Metrics Metrics

	// Canceled reports whether the batch was canceled (via ErrorCancel or context).
	Canceled bool
}

// Batcher executes multiple LLM requests concurrently.
type Batcher struct {
	provider llmtrace.Provider
	config   Config
}

// New creates a new Batcher for the given provider.
func New(provider llmtrace.Provider, opts ...Option) *Batcher {
	cfg := Config{
		MaxConcurrency: 10, // sensible default
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Batcher{
		provider: provider,
		config:   cfg,
	}
}

// Run executes all requests concurrently and returns aggregate results.
//
// The requests are dispatched immediately up to MaxConcurrency. Each request
// uses the provider's Complete method. Per-item errors are captured in Result.Error
// and do not affect other items (unless ErrorCancel is set).
//
// The context controls cancellation for the entire batch. If the context is
// canceled, all in-flight requests are canceled and no new ones are started.
func (b *Batcher) Run(ctx context.Context, requests []*llmtrace.Request) (*Response, error) {
	items := make([]*Item, len(requests))
	for i, req := range requests {
		items[i] = &Item{Request: req}
	}
	return b.RunItems(ctx, items)
}

// RunItems executes all batch items concurrently and returns aggregate results.
// This variant accepts Items which can carry per-request metadata.
func (b *Batcher) RunItems(ctx context.Context, items []*Item) (*Response, error) {
	if len(items) == 0 {
		return &Response{Items: nil, Metrics: Metrics{}}, nil
	}

	// Apply overall timeout
	if b.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.config.Timeout)
		defer cancel()
	}

	start := time.Now()
	results := make([]*Result, len(items))

	// Semaphore for concurrency control
	var sem chan struct{}
	if b.config.MaxConcurrency > 0 {
		sem = make(chan struct{}, b.config.MaxConcurrency)
	}

	var wg sync.WaitGroup
	var canceled atomic.Bool
	var mu sync.Mutex

	for i, item := range items {
		// Check if batch was canceled
		if canceled.Load() {
			break
		}

		// Acquire semaphore
		if sem != nil {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				canceled.Store(true)
				break
			}
		}

		if canceled.Load() {
			break
		}

		wg.Add(1)
		go func(idx int, itm *Item) {
			defer wg.Done()
			if sem != nil {
				defer func() { <-sem }()
			}

			// Apply per-item timeout
			itemCtx := ctx
			var itemCancel context.CancelFunc
			if b.config.PerItemTimeout > 0 {
				itemCtx, itemCancel = context.WithTimeout(ctx, b.config.PerItemTimeout)
				defer itemCancel()
			}

			reqStart := time.Now()
			resp, err := b.provider.Complete(itemCtx, itm.Request)
			latency := time.Since(reqStart)

			result := &Result{
				Index:    idx,
				Response: resp,
				Error:    err,
				Latency:  latency,
				Metadata: itm.Metadata,
			}

			mu.Lock()
			results[idx] = result
			mu.Unlock()

			// Handle error strategy
			if err != nil && b.config.OnError == ErrorCancel {
				canceled.Store(true)
			}

			// Progress callback
			if b.config.OnProgress != nil {
				b.config.OnProgress(idx, result)
			}
		}(i, item)
	}

	wg.Wait()

	totalLatency := time.Since(start)

	// Compute metrics
	metrics := computeMetrics(results, totalLatency)

	resp := &Response{
		Items:    results,
		Metrics:  metrics,
		Canceled: canceled.Load(),
	}

	// Fill in nil slots (for canceled batches)
	for i := range resp.Items {
		if resp.Items[i] == nil {
			resp.Items[i] = &Result{
				Index: i,
				Error: fmt.Errorf("batch canceled before item %d was executed", i),
			}
		}
	}

	return resp, nil
}

func computeMetrics(results []*Result, totalLatency time.Duration) Metrics {
	var m Metrics
	m.TotalRequests = len(results)
	m.TotalLatency = totalLatency
	m.MinLatency = time.Duration(1<<63 - 1) // max duration

	var totalItemLatency time.Duration
	var latencyCount int

	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Error != nil {
			m.Failed++
		} else {
			m.Successful++
			if r.Response != nil {
				m.TotalTokens += r.Response.Usage.TotalTokens
				m.InputTokens += r.Response.Usage.InputTokens
				m.OutputTokens += r.Response.Usage.OutputTokens
			}
			if r.Latency < m.MinLatency {
				m.MinLatency = r.Latency
			}
		}
		if r.Latency > m.MaxLatency {
			m.MaxLatency = r.Latency
		}
		totalItemLatency += r.Latency
		latencyCount++
	}

	if latencyCount > 0 {
		m.AvgLatency = totalItemLatency / time.Duration(latencyCount)
	}
	if m.Successful == 0 {
		m.MinLatency = 0
	}

	return m
}
