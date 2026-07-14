package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the default OpenAI API endpoint.
	DefaultBaseURL = "https://api.openai.com/v1"

	// DefaultModel is the default embedding model.
	DefaultModel = "text-embedding-3-small"

	// DefaultDimensions is the default dimensionality for text-embedding-3-small.
	DefaultDimensions = 1536

	// DefaultMaxBatchSize is the default maximum inputs per API call.
	DefaultMaxBatchSize = 2048

	// embeddingsPath is the API path for embeddings.
	embeddingsPath = "/embeddings"
)

// OpenAIProvider implements the embedding.Provider interface for
// OpenAI's Embeddings API (/v1/embeddings).
type OpenAIProvider struct {
	apiKey       string
	baseURL      string
	model        string
	dimensions   int
	maxBatchSize int
	client       *http.Client
	mu           sync.RWMutex
}

// Option configures an OpenAIProvider.
type Option func(*OpenAIProvider)

// WithAPIKey sets the OpenAI API key.
func WithAPIKey(key string) Option {
	return func(p *OpenAIProvider) {
		p.apiKey = key
	}
}

// WithBaseURL sets the API base URL. Use this for OpenAI-compatible APIs.
func WithBaseURL(url string) Option {
	return func(p *OpenAIProvider) {
		p.baseURL = url
	}
}

// WithModel sets the embedding model (e.g. "text-embedding-3-small",
// "text-embedding-3-large", "text-embedding-ada-002").
func WithModel(model string) Option {
	return func(p *OpenAIProvider) {
		p.model = model
	}
}

// WithDimensions sets the output dimensionality. Reducing dimensions
// can save storage and improve search speed at the cost of some accuracy.
// Only supported by text-embedding-3-* models.
func WithDimensions(dims int) Option {
	return func(p *OpenAIProvider) {
		p.dimensions = dims
	}
}

// WithMaxBatchSize sets the maximum number of inputs per API call.
func WithMaxBatchSize(size int) Option {
	return func(p *OpenAIProvider) {
		p.maxBatchSize = size
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *OpenAIProvider) {
		p.client = client
	}
}

// NewOpenAI creates a new OpenAI embedding provider.
func NewOpenAI(opts ...Option) *OpenAIProvider {
	p := &OpenAIProvider{
		baseURL:      DefaultBaseURL,
		model:        DefaultModel,
		dimensions:   DefaultDimensions,
		maxBatchSize: DefaultMaxBatchSize,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Dimensions returns the configured dimensionality.
func (p *OpenAIProvider) Dimensions() int {
	return p.dimensions
}

// MaxBatchSize returns the maximum inputs per batch call.
func (p *OpenAIProvider) MaxBatchSize() int {
	return p.maxBatchSize
}

// Model returns the configured model name.
func (p *OpenAIProvider) Model() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// Embed generates an embedding vector for a single text input.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) (*Result, error) {
	results, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding: empty response from API")
	}
	return &results[0], nil
}

// EmbedBatch generates embedding vectors for multiple text inputs.
// It automatically chunks large batches to respect the provider's limits.
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([]Result, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("embedding: empty input")
	}

	// Chunk into sub-batches if needed
	chunkSize := p.maxBatchSize
	if chunkSize <= 0 {
		chunkSize = DefaultMaxBatchSize
	}

	var allResults []Result
	for i := 0; i < len(texts); i += chunkSize {
		end := i + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[i:end]

		results, err := p.doEmbed(ctx, chunk, i)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// doEmbed makes a single API call for a batch of texts.
func (p *OpenAIProvider) doEmbed(ctx context.Context, texts []string, offset int) ([]Result, error) {
	p.mu.RLock()
	apiKey := p.apiKey
	baseURL := p.baseURL
	model := p.model
	dimensions := p.dimensions
	p.mu.RUnlock()

	// Build request
	reqBody := openaiEmbedRequest{
		Model: model,
		Input: texts,
	}
	if dimensions > 0 {
		reqBody.Dimensions = dimensions
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal request: %w", err)
	}

	url := baseURL + embeddingsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp openaiEmbedResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("embedding: unmarshal response: %w", err)
	}

	if len(apiResp.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d results, got %d", len(texts), len(apiResp.Data))
	}

	results := make([]Result, len(apiResp.Data))
	for i, d := range apiResp.Data {
		results[i] = Result{
			Vector: d.Embedding,
			Usage: Usage{
				PromptTokens: apiResp.Usage.PromptTokens,
				TotalTokens:  apiResp.Usage.TotalTokens,
			},
			Model: model,
			Index: d.Index + offset,
		}
	}

	return results, nil
}

// openaiEmbedRequest is the request body for /v1/embeddings.
type openaiEmbedRequest struct {
	Model      string `json:"model"`
	Input      any    `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// openaiEmbedResponse is the response from /v1/embeddings.
type openaiEmbedResponse struct {
	Data  []openaiEmbedData `json:"data"`
	Usage openaiUsage       `json:"usage"`
}

type openaiEmbedData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type openaiUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
