// Package gemini implements the llmtrace.Provider interface for Google's Gemini API.
//
// It communicates with the Gemini REST API (generateContent) and translates
// between llmtrace's generic types and Gemini's JSON format.
//
// Usage:
//
//	provider := gemini.New(
//	    gemini.WithAPIKey("AIza..."),
//	    gemini.WithModel("gemini-2.0-flash"),
//	)
//	tracer := llmtrace.NewTracer("my-service",
//	    llmtrace.WithProvider("gemini"),
//	)
//	resp, err := tracer.Complete(ctx, req, provider.Complete)
package gemini

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
	// DefaultBaseURL is the default Gemini API endpoint.
	DefaultBaseURL = "https://generativelanguage.googleapis.com"

	// DefaultModel is the default model when none is specified.
	DefaultModel = "gemini-2.0-flash"

	// apiVersion is the Gemini API version.
	apiVersion = "v1beta"
)

// Provider implements llmtrace.Provider for Google's Gemini API.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	maxRetries   int
}

// Option configures a Gemini Provider.
type Option func(*Provider)

// WithAPIKey sets the Gemini API key.
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

// New creates a new Gemini provider with the given options.
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

// Name returns "gemini".
func (p *Provider) Name() string { return "gemini" }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.defaultModel }

// SupportsStreaming returns true — Gemini supports streaming via SSE.
func (p *Provider) SupportsStreaming() bool { return true }

// --- Request/Response types matching Gemini's API format ---

type generateRequest struct {
	Contents         []content         `json:"contents"`
	GenerationConfig *generationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
}

type content struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type systemInstruction struct {
	Parts []part `json:"parts"`
}

type generateResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata"`
	ModelVersion  string        `json:"modelVersion"`
}

type candidate struct {
	Content      content  `json:"content"`
	FinishReason string   `json:"finishReason"`
	Index        int      `json:"index"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Streaming types ---

type streamChunk struct {
	Candidates    []candidate   `json:"candidates,omitempty"`
	UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"`
}

// apiError represents an error response from the Gemini API.
type apiError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// buildGenerateURL constructs the generateContent URL.
func (p *Provider) buildGenerateURL(model string, stream bool) string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	return fmt.Sprintf("%s/%s/models/%s:%s&key=%s", p.baseURL, apiVersion, model, action, p.apiKey)
}

// Complete makes a non-streaming generateContent request.
func (p *Provider) Complete(ctx context.Context, req *llmtrace.Request) (*llmtrace.Response, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	genReq := p.buildRequest(req)
	body, err := json.Marshal(genReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/models/%s:generateContent?key=%s", p.baseURL, apiVersion, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: do request: %w", err)
	}
	defer httpResp.Body.Close()
	latency := time.Since(start)

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Status:     apiErr.Error.Status,
			}
		}
		return nil, fmt.Errorf("gemini: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var genResp generateResponse
	if err := json.Unmarshal(respBody, &genResp); err != nil {
		return nil, fmt.Errorf("gemini: unmarshal response: %w", err)
	}

	return p.toResponse(&genResp, model, latency), nil
}

// Stream makes a streaming generateContent request.
func (p *Provider) Stream(ctx context.Context, req *llmtrace.Request) (<-chan llmtrace.StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	genReq := p.buildRequest(req)
	body, err := json.Marshal(genReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, apiVersion, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: do request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, &APIError{
				StatusCode: httpResp.StatusCode,
				Message:    apiErr.Error.Message,
				Status:     apiErr.Error.Status,
			}
		}
		return nil, fmt.Errorf("gemini: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	ch := make(chan llmtrace.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				ch <- llmtrace.StreamChunk{Error: fmt.Errorf("gemini: parse stream chunk: %w", err)}
				return
			}

			for _, c := range chunk.Candidates {
				var text string
				for _, part := range c.Content.Parts {
					text += part.Text
				}
				sc := llmtrace.StreamChunk{Content: text}
				if chunk.UsageMetadata != nil {
					sc.Usage = &llmtrace.Usage{
						InputTokens:  chunk.UsageMetadata.PromptTokenCount,
						OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
						TotalTokens:  chunk.UsageMetadata.TotalTokenCount,
					}
				}
				ch <- sc
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- llmtrace.StreamChunk{Error: fmt.Errorf("gemini: stream read: %w", err)}
		}
	}()

	return ch, nil
}

// buildRequest converts an llmtrace.Request to a Gemini generate request.
func (p *Provider) buildRequest(req *llmtrace.Request) *generateRequest {
	var contents []content
	var systemText string

	for _, m := range req.Messages {
		if m.Role == llmtrace.RoleSystem {
			systemText = m.Content
			continue
		}
		// Gemini uses "user" and "model" roles
		role := string(m.Role)
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{
			Role:  role,
			Parts: []part{{Text: m.Content}},
		})
	}

	if len(contents) == 0 {
		contents = []content{{Role: "user", Parts: []part{{Text: ""}}}}
	}

	genReq := &generateRequest{
		Contents: contents,
	}

	// Add generation config if any params are set
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil || len(req.Stop) > 0 {
		genReq.GenerationConfig = &generationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
			StopSequences:   req.Stop,
		}
	}

	// Add system instruction if present
	if systemText != "" {
		genReq.SystemInstruction = &systemInstruction{
			Parts: []part{{Text: systemText}},
		}
	}

	return genReq
}

// toResponse converts a Gemini response to an llmtrace.Response.
func (p *Provider) toResponse(resp *generateResponse, reqModel string, latency time.Duration) *llmtrace.Response {
	var content string
	var finishReason string
	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			content += part.Text
		}
		finishReason = resp.Candidates[0].FinishReason
	}

	return &llmtrace.Response{
		ID:           "", // Gemini doesn't return a request ID
		Model:        resp.ModelVersion,
		Content:      content,
		FinishReason: finishReason,
		Usage: llmtrace.Usage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  resp.UsageMetadata.TotalTokenCount,
		},
		Latency:  latency,
		Provider: "gemini",
	}
}

// APIError represents an error returned by the Gemini API.
type APIError struct {
	StatusCode int
	Message    string
	Status     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gemini: API error %d (%s): %s", e.StatusCode, e.Status, e.Message)
}
