// Package compat implements the llmtrace.Provider interface for OpenAI-compatible APIs.
//
// Many LLM services (vLLM, Together AI, Groq, DeepSeek, Fireworks, etc.) expose
// an API that follows the OpenAI Chat Completions format. This provider allows
// connecting to any such service by configuring the base URL and system name.
//
// Usage:
//
//	// vLLM
//	provider := compat.New(
//	    compat.WithName("vllm"),
//	    compat.WithBaseURL("http://localhost:8000/v1"),
//	    compat.WithModel("meta-llama/Llama-3-8B"),
//	)
//
//	// Together AI
//	provider := compat.New(
//	    compat.WithName("together"),
//	    compat.WithBaseURL("https://api.together.xyz/v1"),
//	    compat.WithAPIKey(os.Getenv("TOGETHER_API_KEY")),
//	    compat.WithModel("meta-llama/Llama-3-70b-chat-hf"),
//	)
//
//	// Groq
//	provider := compat.New(
//	    compat.WithName("groq"),
//	    compat.WithBaseURL("https://api.groq.com/openai/v1"),
//	    compat.WithAPIKey(os.Getenv("GROQ_API_KEY")),
//	    compat.WithModel("llama3-70b-8192"),
//	)
package compat

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
	// DefaultModel is the fallback model when none is specified.
	DefaultModel = "gpt-3.5-turbo"

	// chatCompletionsPath is the standard OpenAI-compatible endpoint.
	chatCompletionsPath = "/chat/completions"
)

// Provider implements llmtrace.Provider for any OpenAI-compatible API.
type Provider struct {
	name         string
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	maxRetries   int
	extraHeaders map[string]string
}

// Option configures a compat Provider.
type Option func(*Provider)

// WithName sets the provider name (used as gen_ai.system span attribute).
// Common values: "vllm", "together", "groq", "deepseek", "fireworks".
func WithName(name string) Option {
	return func(p *Provider) {
		p.name = name
	}
}

// WithAPIKey sets the API key for authentication.
func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
	}
}

// WithBaseURL sets the base URL for the API (e.g. "https://api.groq.com/openai/v1").
func WithBaseURL(url string) Option {
	return func(p *Provider) {
		p.baseURL = strings.TrimRight(url, "/")
	}
}

// WithModel sets the default model.
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

// WithExtraHeader adds a custom header to all requests.
// Useful for service-specific headers (e.g. X-Api-Key for some providers).
func WithExtraHeader(key, value string) Option {
	return func(p *Provider) {
		if p.extraHeaders == nil {
			p.extraHeaders = make(map[string]string)
		}
		p.extraHeaders[key] = value
	}
}

// New creates a new OpenAI-compatible provider with the given options.
// At minimum, WithBaseURL should be set. WithName defaults to "openai-compat" if not set.
func New(opts ...Option) *Provider {
	p := &Provider{
		name:         "openai-compat",
		defaultModel: DefaultModel,
		client:       &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.name }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — OpenAI-compatible APIs support SSE streaming.
func (p *Provider) SupportsStreaming() bool { return true }

// --- Request/Response types matching OpenAI's API format ---

type chatRequest struct {
	Model       string        `json:"model"`
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

// apiError represents an error response from the API.
type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Complete makes a non-streaming chat completion request.
func (p *Provider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	chatReq := p.buildRequest(req, false)

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	url := p.baseURL + chatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	p.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: do request: %w", p.name, err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", p.name, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				Provider:   p.name,
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Type:       apiErr.Error.Type,
				Code:       apiErr.Error.Code,
			}
		}
		return nil, fmt.Errorf("%s: unexpected status %d: %s", p.name, httpResp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("%s: unmarshal response: %w", p.name, err)
	}

	return p.toResponse(&chatResp, latency), nil
}

// Stream makes a streaming chat completion request.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	chatReq := p.buildRequest(req, true)

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	url := p.baseURL + chatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: do request: %w", p.name, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				Provider:   p.name,
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Type:       apiErr.Error.Type,
				Code:       apiErr.Error.Code,
			}
		}
		return nil, fmt.Errorf("%s: unexpected status %d: %s", p.name, httpResp.StatusCode, string(respBody))
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
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("%s: parse stream chunk: %w", p.name, err)}
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
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("%s: stream read: %w", p.name, err)}
		}
	}()

	return ch, nil
}

// buildRequest converts an llmtrace.Request to an OpenAI-compatible chat request.
func (p *Provider) buildRequest(req *llmtrace.Request, stream bool) *chatRequest {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	msgs := make([]chatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	return &chatRequest{
		Model:       model,
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Stream:      stream,
	}
}

// toResponse converts an API response to an llmtrace.Response.
func (p *Provider) toResponse(resp *chatResponse, latency time.Duration) *llmtrace.Response {
	var content string
	var finishReason string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		finishReason = resp.Choices[0].FinishReason
	}

	return &llmtrace.Response{
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		FinishReason: finishReason,
		Usage: llmtrace.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Latency:  latency,
		Provider: p.name,
	}
}

// setHeaders sets the required HTTP headers.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for k, v := range p.extraHeaders {
		req.Header.Set(k, v)
	}
}

// APIError represents an error returned by an OpenAI-compatible API.
type APIError struct {
	Provider   string
	StatusCode int
	Message    string
	Type       string
	Code       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: API error %d (%s): %s", e.Provider, e.StatusCode, e.Type, e.Message)
}
