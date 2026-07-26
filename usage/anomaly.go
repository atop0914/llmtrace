package usage

import (
	"math"
	"sort"
	"time"
)

// Trend represents the direction of a usage trend.
type Trend int

const (
	// TrendStable indicates no significant change.
	TrendStable Trend = iota
	// TrendIncreasing indicates usage is increasing.
	TrendIncreasing
	// TrendDecreasing indicates usage is decreasing.
	TrendDecreasing
)

func (t Trend) String() string {
	switch t {
	case TrendIncreasing:
		return "increasing"
	case TrendDecreasing:
		return "decreasing"
	default:
		return "stable"
	}
}

// TrendResult contains trend analysis results.
type TrendResult struct {
	Trend         Trend
	Slope         float64 // cost per hour (or per window unit)
	Confidence    float64 // 0.0 - 1.0
	CurrentAvg    float64 // average cost in recent window
	PreviousAvg   float64 // average cost in previous window
	ChangePercent float64 // percentage change
}

// DetectTrend analyzes cost trends over time using linear regression.
// It splits records into time windows and computes the slope.
func (t *Tracker) DetectTrend() TrendResult {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.records) < 2 {
		return TrendResult{Trend: TrendStable, Confidence: 0}
	}

	// Group by time window
	windows := t.windowCosts()
	if len(windows) < 2 {
		return TrendResult{Trend: TrendStable, Confidence: 0}
	}

	// Linear regression: y = mx + b
	n := float64(len(windows))
	var sumX, sumY, sumXY, sumX2 float64
	for i, w := range windows {
		x := float64(i)
		y := w.cost
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return TrendResult{Trend: TrendStable, Confidence: 0}
	}

	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n

	// R-squared for confidence
	meanY := sumY / n
	var ssTot, ssRes float64
	for i, w := range windows {
		predicted := slope*float64(i) + intercept
		ssRes += (w.cost - predicted) * (w.cost - predicted)
		ssTot += (w.cost - meanY) * (w.cost - meanY)
	}

	confidence := 0.0
	if ssTot > 0 {
		confidence = 1.0 - ssRes/ssTot
		if confidence < 0 {
			confidence = 0
		}
	}

	// Compare last window to previous
	current := windows[len(windows)-1].cost
	previous := windows[len(windows)-2].cost
	changePct := 0.0
	if previous > 0 {
		changePct = (current - previous) / previous * 100
	}

	trend := TrendStable
	if slope > 0 && confidence > 0.5 {
		trend = TrendIncreasing
	} else if slope < 0 && confidence > 0.5 {
		trend = TrendDecreasing
	}

	return TrendResult{
		Trend:         trend,
		Slope:         slope,
		Confidence:    confidence,
		CurrentAvg:    current,
		PreviousAvg:   previous,
		ChangePercent: changePct,
	}
}

// Anomaly represents a detected usage anomaly.
type Anomaly struct {
	Timestamp   time.Time
	Model       string
	Cost        float64
	ZScore      float64
	Description string
}

// DetectAnomalies finds records with unusually high cost using z-score.
func (t *Tracker) DetectAnomalies() []Anomaly {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.records) < 3 {
		return nil
	}

	// Compute mean and stddev of cost per record
	var sum, sumSq float64
	for _, r := range t.records {
		sum += r.Cost
		sumSq += r.Cost * r.Cost
	}
	n := float64(len(t.records))
	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance <= 0 {
		return nil
	}
	stddev := math.Sqrt(variance)

	var anomalies []Anomaly
	for _, r := range t.records {
		z := (r.Cost - mean) / stddev
		if z >= t.cfg.AnomalyThreshold {
			anomalies = append(anomalies, Anomaly{
				Timestamp:   r.Timestamp,
				Model:       r.Provider + "/" + r.Model,
				Cost:        r.Cost,
				ZScore:      z,
				Description: "cost z-score %.1f (mean=%.4f, stddev=%.4f)",
			})
		}
	}

	// Sort by z-score descending
	sort.Slice(anomalies, func(i, j int) bool {
		return anomalies[i].ZScore > anomalies[j].ZScore
	})

	return anomalies
}

// windowCost is internal aggregation for trend analysis.
type windowCost struct {
	start time.Time
	cost  float64
	calls int
}

// windowCosts groups records into time windows and sums costs.
func (t *Tracker) windowCosts() []windowCost {
	if len(t.records) == 0 {
		return nil
	}

	// Find time range
	minT, maxT := t.records[0].Timestamp, t.records[0].Timestamp
	for _, r := range t.records {
		if r.Timestamp.Before(minT) {
			minT = r.Timestamp
		}
		if r.Timestamp.After(maxT) {
			maxT = r.Timestamp
		}
	}

	// Determine window duration
	var windowDur time.Duration
	switch t.cfg.Window {
	case WindowHourly:
		windowDur = time.Hour
	case WindowDaily:
		windowDur = 24 * time.Hour
	case WindowWeekly:
		windowDur = 7 * 24 * time.Hour
	case WindowMonthly:
		windowDur = 30 * 24 * time.Hour
	default:
		windowDur = 24 * time.Hour
	}

	// Build windows
	numWindows := int(maxT.Sub(minT)/windowDur) + 1
	windows := make([]windowCost, numWindows)
	for i := range windows {
		windows[i].start = minT.Add(time.Duration(i) * windowDur)
	}

	for _, r := range t.records {
		idx := int(r.Timestamp.Sub(minT) / windowDur)
		if idx >= 0 && idx < len(windows) {
			windows[idx].cost += r.Cost
			windows[idx].calls++
		}
	}

	return windows
}
