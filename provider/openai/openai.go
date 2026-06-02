// Package openai implements the llmtrace.Provider interface for OpenAI's Chat API.
//
// It communicates with the OpenAI REST API (v1/chat/completions) and translates
// between llmtrace's generic types and OpenAI's JSON format.
//
// Usage:
//
//	provider := openai.New(
//	    openai.WithAPIKey("sk-..."),
//	    openai.WithModel("gpt-4o"),
//	)
//	tracer := llmtrace.NewTracer("my-service",
//	    llmtrace.WithProvider("openai"),
//	)
//	resp, err := tracer.Complete(ctx, req, provider.Complete)
package openai

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
	// DefaultBaseURL is the default OpenAI API endpoint.
	DefaultBaseURL = "https://api.openai.com/v1"

	// DefaultModel is the default model when none is specified.
	DefaultModel = "gpt-4o-mini"

	// chatCompletionsPath is the API path for chat completions.
	chatCompletionsPath = "/chat/completions"
)

// Provider implements llmtrace.Provider for OpenAI's Chat Completions API.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	maxRetries   int
}

// Option configures an OpenAI Provider.
type Option func(*Provider)

// WithAPIKey sets the OpenAI API key.
func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
	}
}

// WithBaseURL sets a custom base URL (e.g. for Azure OpenAI or proxies).
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

// New creates a new OpenAI provider with the given options.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:      DefaultBaseURL,
		defaultModel: DefaultModel,
		client:       &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns "openai".
func (p *Provider) Name() string { return "openai" }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — OpenAI supports streaming via SSE.
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

// apiError represents an error response from the OpenAI API.
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
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	p.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: do request: %w", err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
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
		return nil, fmt.Errorf("openai: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	return p.toResponse(&chatResp, latency), nil
}

// Stream makes a streaming chat completion request.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	chatReq := p.buildRequest(req, true)

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: do request: %w", err)
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
		return nil, fmt.Errorf("openai: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
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
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("openai: parse stream chunk: %w", err)}
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
				_ = lastID // used for debugging; could be exposed in future
				ch <- sc
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("openai: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// buildRequest converts an llmtrace.Request to an OpenAI chat request.
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

// toResponse converts an OpenAI response to an llmtrace.Response.
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
		Provider: "openai",
	}
}

// setHeaders sets the required HTTP headers for OpenAI API requests.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}

// APIError represents an error returned by the OpenAI API.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai: API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}
