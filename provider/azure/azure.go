// Package azure implements the llmtrace.Provider interface for Azure OpenAI Service.
//
// Azure OpenAI uses a different URL scheme and authentication mechanism than the
// public OpenAI API. This provider handles the Azure-specific requirements:
//   - Deployment-based URLs: https://{resource}.openai.azure.com/openai/deployments/{deployment}/...
//   - API key authentication via api-key header
//   - API version query parameter
//   - Token-based authentication via Bearer token (optional)
//
// Usage:
//
//	provider := azure.New(
//	    azure.WithEndpoint("https://myresource.openai.azure.com"),
//	    azure.WithDeployment("gpt-4o"),
//	    azure.WithAPIKey(os.Getenv("AZURE_OPENAI_API_KEY")),
//	)
//
//	// Or using Entra ID token:
//	provider := azure.New(
//	    azure.WithEndpoint("https://myresource.openai.azure.com"),
//	    azure.WithDeployment("gpt-4o"),
//	    azure.WithToken(accessToken),
//	)
package azure

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/atop0914/llmtrace"
)

const (
	// DefaultAPIVersion is the default Azure OpenAI API version.
	DefaultAPIVersion = "2024-06-01"

	// DefaultModel is the default model identifier.
	DefaultModel = "gpt-4o-mini"

	// chatCompletionsPath is the Azure OpenAI chat completions path template.
	chatCompletionsPath = "/openai/deployments/%s/chat/completions"
)

// Provider implements llmtrace.Provider for Azure OpenAI Service.
type Provider struct {
	endpoint     string // e.g. "https://myresource.openai.azure.com"
	deployment   string // deployment name (not model name)
	apiKey       string // Azure API key
	token        string // Entra ID bearer token (alternative to API key)
	apiVersion   string
	defaultModel string
	client       *http.Client
	maxRetries   int
}

// Option configures an Azure OpenAI Provider.
type Option func(*Provider)

// WithEndpoint sets the Azure OpenAI endpoint (e.g. "https://myresource.openai.azure.com").
func WithEndpoint(endpoint string) Option {
	return func(p *Provider) {
		p.endpoint = strings.TrimRight(endpoint, "/")
	}
}

// WithDeployment sets the Azure OpenAI deployment name.
// This is the deployment you created in the Azure portal, not the model name.
func WithDeployment(deployment string) Option {
	return func(p *Provider) {
		p.deployment = deployment
	}
}

// WithAPIKey sets the Azure OpenAI API key.
func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
	}
}

// WithToken sets a Bearer token for Entra ID (formerly Azure AD) authentication.
// Use this instead of WithAPIKey when using managed identity or service principal.
func WithToken(token string) Option {
	return func(p *Provider) {
		p.token = token
	}
}

// WithAPIVersion sets the Azure OpenAI API version (default: "2024-06-01").
func WithAPIVersion(version string) Option {
	return func(p *Provider) {
		p.apiVersion = version
	}
}

// WithModel sets the default model name (used in request body, not URL).
func WithModel(model string) Option {
	return func(p *Provider) {
		p.defaultModel = model
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		p.client = client
	}
}

// WithMaxRetries sets the number of retry attempts for transient errors.
func WithMaxRetries(n int) Option {
	return func(p *Provider) {
		p.maxRetries = n
	}
}

// New creates a new Azure OpenAI provider with the given options.
// At minimum, WithEndpoint and WithDeployment should be set.
func New(opts ...Option) *Provider {
	p := &Provider{
		apiVersion:   DefaultAPIVersion,
		defaultModel: DefaultModel,
		client:       &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns "azure".
func (p *Provider) Name() string { return "azure" }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — Azure OpenAI supports streaming via SSE.
func (p *Provider) SupportsStreaming() bool { return true }

// --- Request/Response types matching Azure OpenAI's API format ---

type chatRequest struct {
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []choice      `json:"choices"`
	Usage   usageResponse `json:"usage"`
}

type choice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type usageResponse struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Streaming types ---

type streamChunk struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
	Usage   *usageResponse `json:"usage,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// apiError represents an error response from the Azure OpenAI API.
type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// buildURL constructs the full Azure OpenAI chat completions URL.
func (p *Provider) buildURL() string {
	path := fmt.Sprintf(chatCompletionsPath, p.deployment)
	return fmt.Sprintf("%s%s?api-version=%s", p.endpoint, path, p.apiVersion)
}

// Complete makes a non-streaming chat completion request.
func (p *Provider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	chatReq := p.buildRequest(req, false)

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("azure: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.buildURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azure: create request: %w", err)
	}
	p.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure: do request: %w", err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("azure: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Type:       apiErr.Error.Type,
				Code:       apiErr.Error.Code,
			}
		}
		return nil, fmt.Errorf("azure: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("azure: unmarshal response: %w", err)
	}

	return p.toResponse(&chatResp, latency), nil
}

// Stream makes a streaming chat completion request.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	chatReq := p.buildRequest(req, true)

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("azure: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.buildURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azure: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure: do request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Type:       apiErr.Error.Type,
				Code:       apiErr.Error.Code,
			}
		}
		return nil, fmt.Errorf("azure: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	ch := make(chan llmtrace.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		var lastID string

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("azure: parse stream chunk: %w", err)}
				return
			}

			if chunk.ID != "" {
				lastID = chunk.ID
			}

			for _, c := range chunk.Choices {
				sc := llmtrace.StreamChunk{
					Content: c.Delta.Content,
				}
				if chunk.Usage != nil {
					sc.Usage = &llmtrace.Usage{
						InputTokens:  chunk.Usage.PromptTokens,
						OutputTokens: chunk.Usage.CompletionTokens,
						TotalTokens:  chunk.Usage.TotalTokens,
					}
				}
				_ = lastID
				ch <- sc
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("azure: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// buildRequest converts an llmtrace.Request to an Azure OpenAI chat request.
func (p *Provider) buildRequest(req *llmtrace.Request, stream bool) *chatRequest {
	msgs := make([]chatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	return &chatRequest{
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Stream:      stream,
	}
}

// toResponse converts an Azure OpenAI response to an llmtrace.Response.
func (p *Provider) toResponse(resp *chatResponse, latency time.Duration) *llmtrace.Response {
	var content string
	var finishReason string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		finishReason = resp.Choices[0].FinishReason
	}

	model := resp.Model
	if model == "" {
		model = p.defaultModel
	}

	return &llmtrace.Response{
		ID:           resp.ID,
		Model:        model,
		Content:      content,
		FinishReason: finishReason,
		Usage: llmtrace.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Latency:  latency,
		Provider: "azure",
	}
}

// setHeaders sets the required HTTP headers for Azure OpenAI API requests.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("api-key", p.apiKey)
	} else if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
}

// APIError represents an error returned by the Azure OpenAI API.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("azure: API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}
