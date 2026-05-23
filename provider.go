// Package llmtrace provides OpenTelemetry-native observability for LLM calls.
//
// This file defines the Provider interface that all LLM provider adapters must implement.
// Provider adapters translate between the generic llmtrace types and provider-specific
// API formats (OpenAI, Anthropic, Gemini, etc.).

package llmtrace

import "context"

// Provider defines the interface for LLM provider adapters.
// Each provider (OpenAI, Anthropic, Gemini, etc.) implements this interface
// to translate between llmtrace's generic types and the provider's native API format.
//
// A Provider is used with Tracer.Complete and Tracer.Stream to automatically
// handle API communication while getting full observability.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "anthropic", "gemini").
	// This is used as the gen_ai.system span attribute.
	Name() string

	// Complete makes a non-streaming completion request to the provider.
	// It handles HTTP communication, authentication, and response parsing.
	Complete(ctx context.Context, req *Request) (*Response, error)

	// Stream makes a streaming completion request to the provider.
	// It returns a channel that yields partial responses as they arrive.
	// The channel must be closed when the stream is complete.
	Stream(ctx context.Context, req *Request) (<-chan StreamChunk, error)

	// DefaultModel returns the default model to use when none is specified.
	// Returns empty string if there is no default.
	DefaultModel() string

	// SupportsStreaming reports whether this provider supports streaming.
	SupportsStreaming() bool
}

// ProviderConfig holds common configuration for provider adapters.
type ProviderConfig struct {
	// APIKey is the provider's API key for authentication.
	APIKey string

	// BaseURL overrides the provider's default API endpoint.
	// Useful for proxies, Azure OpenAI, or local LLM servers.
	BaseURL string

	// DefaultModel overrides the provider's built-in default model.
	DefaultModel string

	// MaxRetries is the number of retry attempts for transient errors.
	// 0 means no retries.
	MaxRetries int

	// Extra holds provider-specific configuration.
	Extra map[string]any
}

// ProviderOption configures a Provider.
type ProviderOption func(*ProviderConfig)

// WithAPIKey sets the API key for a provider.
func WithAPIKey(key string) ProviderOption {
	return func(c *ProviderConfig) {
		c.APIKey = key
	}
}

// WithBaseURL sets a custom base URL for the provider API.
func WithBaseURL(url string) ProviderOption {
	return func(c *ProviderConfig) {
		c.BaseURL = url
	}
}

// WithDefaultModel sets a custom default model for the provider.
func WithDefaultModel(model string) ProviderOption {
	return func(c *ProviderConfig) {
		c.DefaultModel = model
	}
}

// WithMaxRetries sets the retry count for transient errors.
func WithMaxRetries(n int) ProviderOption {
	return func(c *ProviderConfig) {
		c.MaxRetries = n
	}
}

// WithExtra sets a provider-specific configuration key.
func WithExtra(key string, value any) ProviderOption {
	return func(c *ProviderConfig) {
		if c.Extra == nil {
			c.Extra = make(map[string]any)
		}
		c.Extra[key] = value
	}
}

// ApplyConfig applies a slice of ProviderOptions to a ProviderConfig and returns it.
func ApplyConfig(opts ...ProviderOption) *ProviderConfig {
	cfg := &ProviderConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
