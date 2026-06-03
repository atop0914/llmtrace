// Package ollama implements the llmtrace.Provider interface for Ollama's local LLM server.
//
// Ollama runs open-source LLMs locally (Llama, Mistral, Gemma, etc.) and exposes
// a REST API at http://localhost:11434. This provider supports both streaming and
// non-streaming chat completions.
//
// Usage:
//
//	provider := ollama.New(
//	    ollama.WithBaseURL("http://localhost:11434"),
//	    ollama.WithModel("llama3"),
//	)
//	tracer := llmtrace.NewTracer("my-service",
//	    llmtrace.WithProvider("ollama"),
//	)
//	resp, err := tracer.Chat(ctx, &llmtrace.Request{
//	    Model:    "llama3",
//	    Messages: []llmtrace.Message{{Role: "user", Content: "Hello!"}},
//	}, provider)
package ollama

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
	// DefaultBaseURL is the default Ollama server endpoint.
	DefaultBaseURL = "http://localhost:11434"

	// DefaultModel is the default model when none is specified.
	DefaultModel = "llama3"

	// chatPath is the API path for chat completions.
	chatPath = "/api/chat"

	// generatePath is the API path for text generation.
	generatePath = "/api/generate"
)

// Provider implements llmtrace.Provider for Ollama's local LLM server.
type Provider struct {
	baseURL      string
	defaultModel string
	client       *http.Client
}

// Option configures an Ollama Provider.
type Option func(*Provider)

// WithBaseURL sets the Ollama server URL.
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

// New creates a new Ollama provider with the given options.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:      DefaultBaseURL,
		defaultModel: DefaultModel,
		client:       &http.Client{Timeout: 300 * time.Second}, // LLMs can be slow locally
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns "ollama".
func (p *Provider) Name() string { return "ollama" }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — Ollama supports streaming via newline-delimited JSON.
func (p *Provider) SupportsStreaming() bool { return true }

// --- Request/Response types matching Ollama's API format ---

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  *chatOptions  `json:"options,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"` // equivalent to max_tokens
	Stop        []string `json:"stop,omitempty"`
}

type chatResponse struct {
	Model              string        `json:"model"`
	Message            *chatMessage  `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      int64         `json:"total_duration"`       // nanoseconds
	LoadDuration       int64         `json:"load_duration"`        // nanoseconds
	PromptEvalCount    int           `json:"prompt_eval_count"`    // input tokens
	PromptEvalDuration int64         `json:"prompt_eval_duration"` // nanoseconds
	EvalCount          int           `json:"eval_count"`           // output tokens
	EvalDuration       int64         `json:"eval_duration"`        // nanoseconds
	Error              string        `json:"error,omitempty"`
}

// Complete makes a non-streaming chat completion request to Ollama.
func (p *Provider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	// Build Ollama chat request
	ollamaReq := chatRequest{
		Model:    model,
		Messages: convertMessages(req.Messages),
		Stream:   false,
	}

	// Apply optional parameters
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil || len(req.Stop) > 0 {
		opts := &chatOptions{
			Temperature: req.Temperature,
			TopP:        req.TopP,
			NumPredict:   req.MaxTokens,
			Stop:        req.Stop,
		}
		ollamaReq.Options = opts
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	url := p.baseURL + chatPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: http request: %w", err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, &llmtrace.ProviderError{
			Provider: "ollama",
			Message:  fmt.Sprintf("HTTP %d: %s", httpResp.StatusCode, string(respBody)),
			Code:     fmt.Sprintf("%d", httpResp.StatusCode),
		}
	}

	var ollamaResp chatResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: unmarshal response: %w", err)
	}

	if ollamaResp.Error != "" {
		return nil, &llmtrace.ProviderError{
			Provider: "ollama",
			Message:  ollamaResp.Error,
		}
	}

	if ollamaResp.Message == nil {
		return nil, &llmtrace.ProviderError{
			Provider: "ollama",
			Message:  "empty message in response",
		}
	}

	return &llmtrace.Response{
		Model:    ollamaResp.Model,
		Content:  ollamaResp.Message.Content,
		Provider: "ollama",
		Latency:  latency,
		Usage: llmtrace.Usage{
			InputTokens:  ollamaResp.PromptEvalCount,
			OutputTokens: ollamaResp.EvalCount,
			TotalTokens:  ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Stream makes a streaming chat completion request to Ollama.
// It returns a channel that yields partial responses as they arrive.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	ollamaReq := chatRequest{
		Model:    model,
		Messages: convertMessages(req.Messages),
		Stream:   true,
	}

	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil || len(req.Stop) > 0 {
		opts := &chatOptions{
			Temperature: req.Temperature,
			TopP:        req.TopP,
			NumPredict:   req.MaxTokens,
			Stop:        req.Stop,
		}
		ollamaReq.Options = opts
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	url := p.baseURL + chatPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: http request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, &llmtrace.ProviderError{
			Provider: "ollama",
			Message:  fmt.Sprintf("HTTP %d: %s", httpResp.StatusCode, string(respBody)),
			Code:     fmt.Sprintf("%d", httpResp.StatusCode),
		}
	}

	ch := make(chan llmtrace.StreamChunk, 32)

	go func() {
		defer httpResp.Body.Close()
		defer close(ch)

		var totalInputTokens, totalOutputTokens int
		scanner := bufio.NewScanner(httpResp.Body)
		// Increase buffer size for large responses
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var chunk chatResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("ollama: unmarshal chunk: %w", err)}
				return
			}

			if chunk.Error != "" {
				ch <- llmtrace.StreamChunk{Error: &llmtrace.ProviderError{
					Provider: "ollama",
					Message:  chunk.Error,
				}}
				return
			}

			if chunk.Message != nil && chunk.Message.Content != "" {
				ch <- llmtrace.StreamChunk{Content: chunk.Message.Content}
			}

			// Accumulate token counts
			totalInputTokens += chunk.PromptEvalCount
			totalOutputTokens += chunk.EvalCount

			// Final chunk with usage
			if chunk.Done {
				ch <- llmtrace.StreamChunk{
					Usage: &llmtrace.Usage{
						InputTokens:  totalInputTokens,
						OutputTokens: totalOutputTokens,
						TotalTokens:  totalInputTokens + totalOutputTokens,
					},
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("ollama: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// convertMessages converts llmtrace messages to Ollama's format.
func convertMessages(msgs []llmtrace.Message) []chatMessage {
	result := make([]chatMessage, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	return result
}
