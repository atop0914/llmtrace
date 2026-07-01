package prompt

import (
	"fmt"
	"math/rand/v2"
	"sync"
)

// Variant represents a prompt variant for A/B testing.
type Variant struct {
	// Name identifies the variant (e.g. "control", "treatment-a").
	Name string
	// TemplateName is the registered template name.
	TemplateName string
	// Version is the specific template version for this variant.
	Version string
	// Weight controls selection probability (relative to other variants).
	// A weight of 1.0 is the baseline; 2.0 means twice as likely.
	Weight float64
}

// ExperimentConfig configures an A/B test experiment.
type ExperimentConfig struct {
	// Name identifies the experiment.
	Name string
	// Variants are the prompt variants to test.
	Variants []Variant
}

// Experiment manages A/B testing of prompt variants.
type Experiment struct {
	config   ExperimentConfig
	registry *Registry
	mu       sync.RWMutex
	counts   map[string]int64 // variant name -> selection count
}

// NewExperiment creates a new A/B test experiment.
func NewExperiment(cfg ExperimentConfig, registry *Registry) (*Experiment, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("prompt: experiment name is required")
	}
	if len(cfg.Variants) < 2 {
		return nil, fmt.Errorf("prompt: experiment needs at least 2 variants")
	}
	for _, v := range cfg.Variants {
		if v.Weight <= 0 {
			return nil, fmt.Errorf("prompt: variant %q weight must be positive", v.Name)
		}
		// Validate that the template exists
		if _, err := registry.Get(v.TemplateName, v.Version); err != nil {
			return nil, fmt.Errorf("prompt: variant %q: %w", v.Name, err)
		}
	}
	return &Experiment{
		config:   cfg,
		registry: registry,
		counts:   make(map[string]int64),
	}, nil
}

// Select randomly chooses a variant based on weights.
func (e *Experiment) Select() Variant {
	e.mu.Lock()
	defer e.mu.Unlock()

	totalWeight := 0.0
	for _, v := range e.config.Variants {
		totalWeight += v.Weight
	}

	r := rand.Float64() * totalWeight
	cumulative := 0.0
	for _, v := range e.config.Variants {
		cumulative += v.Weight
		if r <= cumulative {
			e.counts[v.Name]++
			return v
		}
	}

	// Fallback to last variant
	last := e.config.Variants[len(e.config.Variants)-1]
	e.counts[last.Name]++
	return last
}

// SelectAndRender selects a variant and renders it with the given variables.
func (e *Experiment) SelectAndRender(vars map[string]string) (Variant, []RenderedMessage, error) {
	v := e.Select()
	msgs, err := e.registry.RenderVersion(v.TemplateName, v.Version, vars)
	if err != nil {
		return v, nil, err
	}
	return v, msgs, nil
}

// Stats returns selection counts for each variant.
func (e *Experiment) Stats() map[string]int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]int64, len(e.counts))
	for k, v := range e.counts {
		result[k] = v
	}
	return result
}

// Reset clears all selection counts.
func (e *Experiment) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counts = make(map[string]int64)
}
