package metrics

import (
	"testing"
)

func TestCollectEmpty(t *testing.T) {
	reg := NewRegistry("test")
	snap := reg.Collect()

	if len(snap.Counters) != 0 {
		t.Errorf("expected 0 counters, got %d", len(snap.Counters))
	}
	if len(snap.Gauges) != 0 {
		t.Errorf("expected 0 gauges, got %d", len(snap.Gauges))
	}
	if len(snap.Histograms) != 0 {
		t.Errorf("expected 0 histograms, got %d", len(snap.Histograms))
	}
}

func TestCollectCounters(t *testing.T) {
	reg := NewRegistry("app")
	c := reg.RegisterCounter("requests", "Total requests", []string{"method", "path"})
	c.With("GET", "/api").Add(10)
	c.With("POST", "/api").Add(5)
	c.With("GET", "/health").Add(100)

	snap := reg.Collect()

	if len(snap.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(snap.Counters))
	}

	cs := snap.Counters[0]
	if cs.Name != "app_requests" {
		t.Errorf("expected app_requests, got %s", cs.Name)
	}
	if cs.Help != "Total requests" {
		t.Errorf("expected help text, got %s", cs.Help)
	}
	if len(cs.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(cs.Labels))
	}
	if len(cs.Samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(cs.Samples))
	}

	// Check samples are sorted by label values
	if cs.Samples[0].Value != 10 || cs.Samples[0].LabelValues[0] != "GET" {
		t.Errorf("unexpected first sample: %v %f", cs.Samples[0].LabelValues, cs.Samples[0].Value)
	}
}

func TestCollectGauges(t *testing.T) {
	reg := NewRegistry("app")
	g := reg.RegisterGauge("connections", "Active connections", []string{"pool"})
	g.With("primary").Set(42)
	g.With("replica").Set(15)

	snap := reg.Collect()

	if len(snap.Gauges) != 1 {
		t.Fatalf("expected 1 gauge, got %d", len(snap.Gauges))
	}

	gs := snap.Gauges[0]
	if gs.Name != "app_connections" {
		t.Errorf("expected app_connections, got %s", gs.Name)
	}
	if len(gs.Samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(gs.Samples))
	}
}

func TestCollectHistograms(t *testing.T) {
	reg := NewRegistry("app")
	h := reg.RegisterHistogram("latency", "Request latency", []string{"method"}, []float64{0.01, 0.05, 0.1, 0.5, 1})
	h.With("GET").Observe(0.03)
	h.With("GET").Observe(0.08)
	h.With("GET").Observe(0.5)
	h.With("POST").Observe(0.15)

	snap := reg.Collect()

	if len(snap.Histograms) != 1 {
		t.Fatalf("expected 1 histogram, got %d", len(snap.Histograms))
	}

	hs := snap.Histograms[0]
	if hs.Name != "app_latency" {
		t.Errorf("expected app_latency, got %s", hs.Name)
	}
	if len(hs.Samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(hs.Samples))
	}

	// Find GET sample
	var getSample *HistogramSample
	for i, s := range hs.Samples {
		if s.LabelValues[0] == "GET" {
			getSample = &hs.Samples[i]
		}
	}
	if getSample == nil {
		t.Fatal("missing GET sample")
	}
	if getSample.Count != 3 {
		t.Errorf("expected 3 observations, got %d", getSample.Count)
	}
	if len(getSample.Buckets) == 0 {
		t.Error("expected non-empty buckets")
	}
}

func TestCollectSorted(t *testing.T) {
	reg := NewRegistry("test")
	c := reg.RegisterCounter("items", "Items", []string{"name"})
	c.With("zebra").Add(1)
	c.With("apple").Add(2)
	c.With("mango").Add(3)

	snap := reg.Collect()

	if len(snap.Counters[0].Samples) != 3 {
		t.Fatal("expected 3 samples")
	}
	// Should be sorted alphabetically
	if snap.Counters[0].Samples[0].LabelValues[0] != "apple" {
		t.Errorf("expected apple first, got %s", snap.Counters[0].Samples[0].LabelValues[0])
	}
	if snap.Counters[0].Samples[1].LabelValues[0] != "mango" {
		t.Errorf("expected mango second, got %s", snap.Counters[0].Samples[1].LabelValues[0])
	}
	if snap.Counters[0].Samples[2].LabelValues[0] != "zebra" {
		t.Errorf("expected zebra third, got %s", snap.Counters[0].Samples[2].LabelValues[0])
	}
}
