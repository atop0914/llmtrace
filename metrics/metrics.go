// Package metrics provides a lightweight, zero-dependency Prometheus-compatible
// metrics collector for LLM observability.
//
// It collects request counts, latency histograms, token usage, and cost metrics
// following Prometheus exposition format conventions.
//
// Usage:
//
//	reg := metrics.NewRegistry("llmtrace")
//	collector := metrics.NewLLMCollector(reg)
//	http.Handle("/metrics", metrics.Handler(reg))
//
//	// Use as middleware
//	resp, err := tracer.Chat(ctx, req, provider,
//	    llmtrace.WithCallMiddleware(collector.Middleware()),
//	)
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType represents the Prometheus metric type.
type MetricType string

const (
	TypeCounter   MetricType = "counter"
	TypeGauge     MetricType = "gauge"
	TypeHistogram MetricType = "histogram"
)

// Metric represents a single metric family (all label variants of one metric name).
type Metric struct {
	Name   string
	Help   string
	Type   MetricType
	Labels []string // label names
}

// Sample represents a single metric data point with labels.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Counter is a monotonically increasing value.
type Counter struct {
	v atomic.Uint64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() { c.Add(1) }

// Add adds the given value to the counter. Panics if val < 0.
func (c *Counter) Add(val float64) {
	for {
		old := c.v.Load()
		new := math.Float64frombits(old) + val
		if c.v.CompareAndSwap(old, math.Float64bits(new)) {
			return
		}
	}
}

// Value returns the current value.
func (c *Counter) Value() float64 {
	return math.Float64frombits(c.v.Load())
}

// Gauge is a value that can go up and down.
type Gauge struct {
	v atomic.Uint64
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() { g.Add(1) }

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() { g.Add(-1) }

// Add adds the given value to the gauge (can be negative).
func (g *Gauge) Add(val float64) {
	for {
		old := g.v.Load()
		new := math.Float64frombits(old) + val
		if g.v.CompareAndSwap(old, math.Float64bits(new)) {
			return
		}
	}
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(val float64) {
	g.v.Store(math.Float64bits(val))
}

// Value returns the current value.
func (g *Gauge) Value() float64 {
	return math.Float64frombits(g.v.Load())
}

// Histogram tracks the distribution of values.
type Histogram struct {
	mu      sync.Mutex
	buckets []float64 // upper bounds
	counts  []uint64  // cumulative counts per bucket
	sum     float64
	count   uint64
}

// DefaultBuckets are the default histogram bucket boundaries (in seconds).
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// NewHistogram creates a new Histogram with the given bucket boundaries.
func NewHistogram(buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	// Ensure buckets are sorted
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &Histogram{
		buckets: sorted,
		counts:  make([]uint64, len(sorted)),
	}
}

// Observe adds an observation to the histogram.
// Stores per-bucket counts; cumulative totals computed in Snapshot().
func (h *Histogram) Observe(val float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += val
	h.count++
	// Find the bucket this value belongs to (first upper bound >= val)
	for i, upper := range h.buckets {
		if val <= upper {
			h.counts[i]++
			return
		}
	}
}

// Snapshot returns a copy of the histogram state with cumulative bucket counts
// in Prometheus exposition format.
func (h *Histogram) Snapshot() ([]Bucket, uint64, float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buckets := make([]Bucket, len(h.buckets))
	var cumCount uint64
	for i, upper := range h.buckets {
		cumCount += h.counts[i]
		buckets[i] = Bucket{Upper: upper, Count: cumCount}
	}
	// Add +Inf bucket
	buckets = append(buckets, Bucket{Upper: math.Inf(1), Count: h.count})
	return buckets, h.count, h.sum
}

// Bucket represents a histogram bucket.
type Bucket struct {
	Upper float64
	Count uint64
}

// Registry collects and exposes metrics.
type Registry struct {
	mu       sync.RWMutex
	ns       string // namespace prefix
	counters map[string]*counterEntry
	gauges   map[string]*gaugeEntry
	histos   map[string]*histoEntry
}

type counterEntry struct {
	metric  *Metric
	samples map[string]*Counter // key is label value string
}

type gaugeEntry struct {
	metric  *Metric
	samples map[string]*Gauge
}

type histoEntry struct {
	metric  *Metric
	samples map[string]*Histogram
}

// NewRegistry creates a new metrics registry with the given namespace prefix.
func NewRegistry(namespace string) *Registry {
	return &Registry{
		ns:       namespace,
		counters: make(map[string]*counterEntry),
		gauges:   make(map[string]*gaugeEntry),
		histos:   make(map[string]*histoEntry),
	}
}

// labelKey returns a stable string key for a set of label values.
func labelKey(labelValues []string) string {
	return strings.Join(labelValues, "\x00")
}

// RegisterCounter registers a new counter metric.
func (r *Registry) RegisterCounter(name, help string, labelNames []string) *CounterVec {
	r.mu.Lock()
	defer r.mu.Unlock()
	fullName := r.prefixedName(name)
	r.counters[fullName] = &counterEntry{
		metric:  &Metric{Name: fullName, Help: help, Type: TypeCounter, Labels: labelNames},
		samples: make(map[string]*Counter),
	}
	return &CounterVec{registry: r, name: fullName, labelNames: labelNames}
}

// RegisterGauge registers a new gauge metric.
func (r *Registry) RegisterGauge(name, help string, labelNames []string) *GaugeVec {
	r.mu.Lock()
	defer r.mu.Unlock()
	fullName := r.prefixedName(name)
	r.gauges[fullName] = &gaugeEntry{
		metric:  &Metric{Name: fullName, Help: help, Type: TypeGauge, Labels: labelNames},
		samples: make(map[string]*Gauge),
	}
	return &GaugeVec{registry: r, name: fullName, labelNames: labelNames}
}

// RegisterHistogram registers a new histogram metric.
func (r *Registry) RegisterHistogram(name, help string, labelNames []string, buckets []float64) *HistogramVec {
	r.mu.Lock()
	defer r.mu.Unlock()
	fullName := r.prefixedName(name)
	r.histos[fullName] = &histoEntry{
		metric:  &Metric{Name: fullName, Help: help, Type: TypeHistogram, Labels: labelNames},
		samples: make(map[string]*Histogram),
	}
	return &HistogramVec{registry: r, name: fullName, labelNames: labelNames, buckets: buckets}
}

func (r *Registry) prefixedName(name string) string {
	if r.ns == "" {
		return name
	}
	return r.ns + "_" + name
}

// CounterVec is a set of counters with the same name but different label values.
type CounterVec struct {
	registry   *Registry
	name       string
	labelNames []string
}

// With returns the counter for the given label values.
func (cv *CounterVec) With(labelValues ...string) *Counter {
	key := labelKey(labelValues)
	r := cv.registry
	r.mu.Lock()
	entry := r.counters[cv.name]
	if c, ok := entry.samples[key]; ok {
		r.mu.Unlock()
		return c
	}
	c := &Counter{}
	entry.samples[key] = c
	r.mu.Unlock()
	return c
}

// GaugeVec is a set of gauges with different label values.
type GaugeVec struct {
	registry   *Registry
	name       string
	labelNames []string
}

// With returns the gauge for the given label values.
func (gv *GaugeVec) With(labelValues ...string) *Gauge {
	key := labelKey(labelValues)
	r := gv.registry
	r.mu.Lock()
	entry := r.gauges[gv.name]
	if g, ok := entry.samples[key]; ok {
		r.mu.Unlock()
		return g
	}
	g := &Gauge{}
	entry.samples[key] = g
	r.mu.Unlock()
	return g
}

// HistogramVec is a set of histograms with different label values.
type HistogramVec struct {
	registry   *Registry
	name       string
	labelNames []string
	buckets    []float64
}

// With returns the histogram for the given label values.
func (hv *HistogramVec) With(labelValues ...string) *Histogram {
	key := labelKey(labelValues)
	r := hv.registry
	r.mu.Lock()
	entry := r.histos[hv.name]
	if h, ok := entry.samples[key]; ok {
		r.mu.Unlock()
		return h
	}
	h := NewHistogram(hv.buckets)
	entry.samples[key] = h
	r.mu.Unlock()
	return h
}

// WritePrometheus writes all metrics in Prometheus text exposition format.
func (r *Registry) WritePrometheus() string {
	var sb strings.Builder
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Write counters
	for _, entry := range r.counters {
		writeMetricHeader(&sb, entry.metric)
		writeCounterSamples(&sb, entry.metric, entry.samples)
		sb.WriteByte('\n')
	}

	// Write gauges
	for _, entry := range r.gauges {
		writeMetricHeader(&sb, entry.metric)
		writeGaugeSamples(&sb, entry.metric, entry.samples)
		sb.WriteByte('\n')
	}

	// Write histograms
	for _, entry := range r.histos {
		writeMetricHeader(&sb, entry.metric)
		writeHistogramSamples(&sb, entry.metric, entry.samples)
		sb.WriteByte('\n')
	}

	return sb.String()
}

func writeMetricHeader(sb *strings.Builder, m *Metric) {
	fmt.Fprintf(sb, "# HELP %s %s\n", m.Name, m.Help)
	fmt.Fprintf(sb, "# TYPE %s %s\n", m.Name, m.Type)
}

func writeCounterSamples(sb *strings.Builder, m *Metric, samples map[string]*Counter) {
	// Sort keys for stable output
	keys := make([]string, 0, len(samples))
	for k := range samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := samples[k]
		labels := labelString(m.Labels, k)
		fmt.Fprintf(sb, "%s%s %g\n", m.Name, labels, c.Value())
	}
}

func writeGaugeSamples(sb *strings.Builder, m *Metric, samples map[string]*Gauge) {
	keys := make([]string, 0, len(samples))
	for k := range samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := samples[k]
		labels := labelString(m.Labels, k)
		fmt.Fprintf(sb, "%s%s %g\n", m.Name, labels, g.Value())
	}
}

func writeHistogramSamples(sb *strings.Builder, m *Metric, samples map[string]*Histogram) {
	keys := make([]string, 0, len(samples))
	for k := range samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h := samples[k]
		buckets, count, sum := h.Snapshot()
		labels := labelString(m.Labels, k)
		// Strip outer braces for bucket injection
		base := strings.TrimPrefix(labels, "{")
		base = strings.TrimSuffix(base, "}")
		for _, b := range buckets {
			if base == "" {
				fmt.Fprintf(sb, "%s_bucket{le=\"%g\"} %d\n", m.Name, b.Upper, b.Count)
			} else {
				fmt.Fprintf(sb, "%s_bucket{%s,le=\"%g\"} %d\n", m.Name, base, b.Upper, b.Count)
			}
		}
		fmt.Fprintf(sb, "%s_count%s %d\n", m.Name, labels, count)
		fmt.Fprintf(sb, "%s_sum%s %g\n", m.Name, labels, sum)
	}
}

func labelString(names []string, key string) string {
	if len(names) == 0 {
		return ""
	}
	values := strings.Split(key, "\x00")
	var sb strings.Builder
	sb.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		val := ""
		if i < len(values) {
			val = values[i]
		}
		fmt.Fprintf(&sb, "%s=\"%s\"", name, val)
	}
	sb.WriteByte('}')
	return sb.String()
}

// Snapshot returns all metric names and their types for inspection.
func (r *Registry) Snapshot() []MetricInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var infos []MetricInfo
	for name, entry := range r.counters {
		infos = append(infos, MetricInfo{Name: name, Type: TypeCounter, Help: entry.metric.Help})
	}
	for name, entry := range r.gauges {
		infos = append(infos, MetricInfo{Name: name, Type: TypeGauge, Help: entry.metric.Help})
	}
	for name, entry := range r.histos {
		infos = append(infos, MetricInfo{Name: name, Type: TypeHistogram, Help: entry.metric.Help})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// MetricInfo describes a registered metric.
type MetricInfo struct {
	Name string
	Type MetricType
	Help string
}

// Reset clears all metric values (useful for testing).
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.counters {
		entry.samples = make(map[string]*Counter)
	}
	for _, entry := range r.gauges {
		entry.samples = make(map[string]*Gauge)
	}
	for _, entry := range r.histos {
		entry.samples = make(map[string]*Histogram)
	}
}

// DurationBuckets returns histogram buckets suitable for measuring durations
// in seconds, covering sub-millisecond to multi-second ranges.
func DurationBuckets() []float64 {
	return []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
}

// TokenBuckets returns histogram buckets suitable for token counts.
func TokenBuckets() []float64 {
	return []float64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000}
}

// FormatTimestamp returns a Prometheus-compatible millisecond timestamp.
func FormatTimestamp(t time.Time) int64 {
	return t.UnixMilli()
}
