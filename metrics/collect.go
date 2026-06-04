package metrics

import (
	"sort"
	"strings"
)

// SnapshotData holds all metric values for dashboard consumption.
type SnapshotData struct {
	Counters   []CounterSnapshot   `json:"counters"`
	Gauges     []GaugeSnapshot     `json:"gauges"`
	Histograms []HistogramSnapshot `json:"histograms"`
}

// CounterSnapshot holds a counter family's data.
type CounterSnapshot struct {
	Name    string           `json:"name"`
	Help    string           `json:"help"`
	Labels  []string         `json:"labels"`
	Samples []ValueSample    `json:"samples"`
}

// GaugeSnapshot holds a gauge family's data.
type GaugeSnapshot struct {
	Name    string           `json:"name"`
	Help    string           `json:"help"`
	Labels  []string         `json:"labels"`
	Samples []ValueSample    `json:"samples"`
}

// HistogramSnapshot holds a histogram family's data.
type HistogramSnapshot struct {
	Name    string            `json:"name"`
	Help    string            `json:"help"`
	Labels  []string          `json:"labels"`
	Samples []HistogramSample `json:"samples"`
}

// ValueSample is a single data point with label values.
type ValueSample struct {
	LabelValues []string `json:"label_values"`
	Value       float64  `json:"value"`
}

// HistogramSample is a histogram data point with label values.
type HistogramSample struct {
	LabelValues []string  `json:"label_values"`
	Buckets     []BucketPoint `json:"buckets"`
	Count       uint64    `json:"count"`
	Sum         float64   `json:"sum"`
}

// BucketPoint is a single histogram bucket.
type BucketPoint struct {
	Upper float64 `json:"upper"`
	Count uint64  `json:"count"`
}

// Collect returns a full snapshot of all metric values.
// This is the primary data source for the dashboard API.
func (r *Registry) Collect() SnapshotData {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := SnapshotData{}

	// Counters
	names := make([]string, 0, len(r.counters))
	for name := range r.counters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := r.counters[name]
		cs := CounterSnapshot{
			Name:   name,
			Help:   entry.metric.Help,
			Labels: entry.metric.Labels,
		}
		for key, c := range entry.samples {
			values := strings.Split(key, "\x00")
			cs.Samples = append(cs.Samples, ValueSample{
				LabelValues: values,
				Value:       c.Value(),
			})
		}
		sort.Slice(cs.Samples, func(i, j int) bool {
			return strings.Join(cs.Samples[i].LabelValues, ",") < strings.Join(cs.Samples[j].LabelValues, ",")
		})
		snap.Counters = append(snap.Counters, cs)
	}

	// Gauges
	names = names[:0]
	for name := range r.gauges {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := r.gauges[name]
		gs := GaugeSnapshot{
			Name:   name,
			Help:   entry.metric.Help,
			Labels: entry.metric.Labels,
		}
		for key, g := range entry.samples {
			values := strings.Split(key, "\x00")
			gs.Samples = append(gs.Samples, ValueSample{
				LabelValues: values,
				Value:       g.Value(),
			})
		}
		sort.Slice(gs.Samples, func(i, j int) bool {
			return strings.Join(gs.Samples[i].LabelValues, ",") < strings.Join(gs.Samples[j].LabelValues, ",")
		})
		snap.Gauges = append(snap.Gauges, gs)
	}

	// Histograms
	names = names[:0]
	for name := range r.histos {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := r.histos[name]
		hs := HistogramSnapshot{
			Name:   name,
			Help:   entry.metric.Help,
			Labels: entry.metric.Labels,
		}
		for key, h := range entry.samples {
			values := strings.Split(key, "\x00")
			buckets, count, sum := h.Snapshot()
			bp := make([]BucketPoint, len(buckets))
			for i, b := range buckets {
				bp[i] = BucketPoint{Upper: b.Upper, Count: b.Count}
			}
			hs.Samples = append(hs.Samples, HistogramSample{
				LabelValues: values,
				Buckets:     bp,
				Count:       count,
				Sum:         sum,
			})
		}
		sort.Slice(hs.Samples, func(i, j int) bool {
			return strings.Join(hs.Samples[i].LabelValues, ",") < strings.Join(hs.Samples[j].LabelValues, ",")
		})
		snap.Histograms = append(snap.Histograms, hs)
	}

	return snap
}
