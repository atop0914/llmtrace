// Package anthropic implements the llmtrace.Provider interface for Anthropic's Messages API.
//
// It communicates with the Anthropic REST API (v1/messages) and translates
// between llmtrace's generic types and Anthropic's JSON format.
//
// Usage:
//
//	provider := anthropic.New(
//	    anthropic.WithAPIKey("sk-ant-..."),
//	    anthropic.WithModel("claude-sonnet-4-20250514"),
//	)
//	tracer := llmtrace.NewTracer("my-service",
//	    llmtrace.WithProvider("anthropic"),
//	)
//	resp, err := tracer.Complete(ctx, req, provider.Complete)
package anthropic

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
	// DefaultBaseURL is the default Anthropic API endpoint.
	DefaultBaseURL = "https://api.anthropic.com"

	// DefaultModel is the default model when none is specified.
	DefaultModel = "claude-sonnet-4-20250514"

	// messagesPath is the API path for messages.
	messagesPath = "/v1/messages"

	// apiVersion is the Anthropic API version header value.
	apiVersion = "2023-06-01"
)

// Provider implements llmtrace.Provider for Anthropic's Messages API.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	maxRetries   int
}

// Option configures an Anthropic Provider.
type Option func(*Provider)

// WithAPIKey sets the Anthropic API key.
func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
	}
}

// WithBaseURL sets a custom base URL (e.g. for proxies).
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

// New creates a new Anthropic provider with the given options.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:      DefaultBaseURL,
		defaultModel: DefaultModel,
		client:       &http.Client{Timeout: 120 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns "anthropic".
func (p *Provider) Name() string { return "anthropic" }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — Anthropic supports streaming via SSE.
func (p *Provider) SupportsStreaming() bool { return true }

// --- Request/Response types matching Anthropic's API format ---

type messagesRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	MaxTokens     int            `json:"max_tokens"`
	System        string         `json:"system,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	ID         string          `json:"id"`
	Model      string          `json:"model"`
	Content    []contentBlock  `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      usageResponse   `json:"usage"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type usageResponse struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Streaming types ---

// streamEvent represents a generic SSE event from Anthropic's streaming API.
type streamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
	// message_start event
	Message *streamMessageStart `json:"message,omitempty"`
	// message_delta event
	Usage *usageResponse `json:"usage,omitempty"`
	// content_block_delta event
	Index *int `json:"index,omitempty"`
}

type streamMessageStart struct {
	ID    string        `json:"id"`
	Model string        `json:"model"`
	Usage usageResponse `json:"usage"`
}

type contentDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// apiError represents an error response from the Anthropic API.
type apiError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete makes a non-streaming messages request.
func (p *Provider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	msgReq := p.buildRequest(req, false)

	body, err := json.Marshal(msgReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+messagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	p.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Type:       apiErr.Error.Type,
			}
		}
		return nil, fmt.Errorf("anthropic: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var msgResp messagesResponse
	if err := json.Unmarshal(respBody, &msgResp); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	return p.toResponse(&msgResp, req.Model, latency), nil
}

// Stream makes a streaming messages request.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	msgReq := p.buildRequest(req, true)

	body, err := json.Marshal(msgReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+messagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: do request: %w", err)
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
			}
		}
		return nil, fmt.Errorf("anthropic: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	ch := make(chan llmtrace.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		var currentEvent string
		var usage *llmtrace.Usage

		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: "event: <type>" followed by "data: <json>"
			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "message_start":
				var evt streamEvent
				if err := json.Unmarshal([]byte(data), &evt); err != nil {
					ch <- llmtrace.StreamChunk{Error: fmt.Errorf("anthropic: parse message_start: %w", err)}
					return
				}
				if evt.Message != nil {
					usage = &llmtrace.Usage{
						InputTokens: evt.Message.Usage.InputTokens,
					}
				}

			case "content_block_delta":
				var evt streamEvent
				if err := json.Unmarshal([]byte(data), &evt); err != nil {
					ch <- llmtrace.StreamChunk{Error: fmt.Errorf("anthropic: parse content_block_delta: %w", err)}
					return
				}
				var delta contentDelta
				if err := json.Unmarshal(evt.Delta, &delta); err == nil && delta.Text != "" {
					ch <- llmtrace.StreamChunk{Content: delta.Text}
				}

			case "message_delta":
				var evt streamEvent
				if err := json.Unmarshal([]byte(data), &evt); err != nil {
					ch <- llmtrace.StreamChunk{Error: fmt.Errorf("anthropic: parse message_delta: %w", err)}
					return
				}
				if evt.Usage != nil {
					if usage == nil {
						usage = &llmtrace.Usage{}
					}
					usage.OutputTokens = evt.Usage.OutputTokens
					usage.TotalTokens = usage.InputTokens + usage.OutputTokens
				}

			case "message_stop":
				// Stream complete — send final chunk with usage
				if usage != nil {
					ch <- llmtrace.StreamChunk{Usage: usage}
				}

			case "error":
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("anthropic: stream error: %s", data)}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("anthropic: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// buildRequest converts an llmtrace.Request to an Anthropic messages request.
func (p *Provider) buildRequest(req *llmtrace.Request, stream bool) *messagesRequest {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	// Anthropic requires max_tokens; default to 4096 if not specified.
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	// Extract system message from messages (Anthropic uses a separate system field).
	var system string
	var msgs []chatMessage
	for _, m := range req.Messages {
		if m.Role == llmtrace.RoleSystem {
			system = m.Content
			continue
		}
		msgs = append(msgs, chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	// Anthropic requires at least one message.
	if len(msgs) == 0 {
		msgs = []chatMessage{{Role: "user", Content: ""}}
	}

	return &messagesRequest{
		Model:         model,
		Messages:      msgs,
		MaxTokens:     maxTokens,
		System:        system,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		Stream:        stream,
	}
}

// toResponse converts an Anthropic response to an llmtrace.Response.
func (p *Provider) toResponse(resp *messagesResponse, reqModel string, latency time.Duration) *llmtrace.Response {
	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &llmtrace.Response{
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		FinishReason: resp.StopReason,
		Usage: llmtrace.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		Latency:  latency,
		Provider: "anthropic",
	}
}

// setHeaders sets the required HTTP headers for Anthropic API requests.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", apiVersion)
	if p.apiKey != "" {
		req.Header.Set("x-api-key", p.apiKey)
	}
}

// APIError represents an error returned by the Anthropic API.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic: API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}
