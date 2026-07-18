// Package proxy implements an OpenAI-compatible local proxy server that routes
// requests through LLMTrace providers with full observability.
//
// Any OpenAI-compatible client (curl, openai SDK, langchain, etc.) can point at
// this proxy to get automatic tracing, metrics, rate limiting, circuit breaking,
// and cost tracking — without changing any client code.
//
// Usage:
//
//	proxy := proxy.New(proxy.Config{
//	    Listen: ":8080",
//	    Providers: map[string]proxy.ProviderEntry{
//	        "gpt-4o": {Provider: openaiProvider, Default: true},
//	        "claude": {Provider: anthropicProvider},
//	    },
//	    Tracer: llmtrace.NewTracer("my-proxy"),
//	})
//	log.Fatal(proxy.ListenAndServe())
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atop0914/llmtrace"
)

// OpenAI API request/response types.

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
	Object  string        `json:"object"`
	Created int64         `json:"created"`
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

// streamChunk matches OpenAI's SSE chunk format.
type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
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

// modelsResponse matches OpenAI's GET /v1/models format.
type modelsResponse struct {
	Object string      `json:"object"`
	Data   []modelInfo `json:"data"`
}

type modelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// apiError matches OpenAI's error format.
type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// ProviderEntry associates a model name pattern with a provider.
type ProviderEntry struct {
	// Provider is the LLMTrace provider to route requests to.
	Provider llmtrace.Provider

	// Default marks this as the fallback provider when model doesn't match any entry.
	Default bool
}

// Config configures the proxy server.
type Config struct {
	// Listen is the address to listen on (e.g. ":8080").
	Listen string

	// Providers maps model name patterns to providers.
	// Keys are matched against the request's model field (prefix match).
	Providers map[string]ProviderEntry

	// Tracer is the LLMTrace tracer for observability. Optional.
	Tracer *llmtrace.Tracer

	// Middlewares to apply to each request. Optional.
	Middlewares []llmtrace.Middleware

	// APIKey for authentication. If empty, no auth is required.
	APIKey string

	// Logger for request logging. Defaults to slog.Default().
	Logger *slog.Logger
}

// Server is an OpenAI-compatible proxy server.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	server *http.Server
	mu     sync.RWMutex
}

// New creates a new proxy server with the given config.
func New(cfg Config) *Server {
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{cfg: cfg}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.server = &http.Server{
		Addr:    cfg.Listen,
		Handler: s.withLogging(s.withAuth(s.mux)),
	}
	return s
}

// ListenAndServe starts the proxy server.
func (s *Server) ListenAndServe() error {
	s.cfg.Logger.Info("proxy listening", "addr", s.cfg.Listen)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Handler returns the HTTP handler for testing or embedding.
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}

// findProvider matches a model name to a configured provider.
// It tries exact match first, then prefix match, then default.
func (s *Server) findProvider(model string) (llmtrace.Provider, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Exact match
	for key, entry := range s.cfg.Providers {
		if key == model {
			return entry.Provider, key
		}
	}

	// Prefix match (longest prefix wins)
	bestKey := ""
	var bestProvider llmtrace.Provider
	for key, entry := range s.cfg.Providers {
		if strings.HasPrefix(model, key) && len(key) > len(bestKey) {
			bestKey = key
			bestProvider = entry.Provider
		}
	}
	if bestProvider != nil {
		return bestProvider, bestKey
	}

	// Default provider
	for _, entry := range s.cfg.Providers {
		if entry.Default {
			return entry.Provider, "default"
		}
	}

	return nil, ""
}

// handleChatCompletions handles POST /v1/chat/completions.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body")
		return
	}

	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "messages is required")
		return
	}

	provider, providerKey := s.findProvider(req.Model)
	if provider == nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "No provider found for model: "+req.Model)
		return
	}

	llmReq := s.toLLMRequest(&req)
	ctx := r.Context()

	if req.Stream {
		s.handleStream(w, ctx, llmReq, provider, providerKey)
	} else {
		s.handleComplete(w, ctx, llmReq, provider, providerKey)
	}
}

// handleComplete handles non-streaming requests.
func (s *Server) handleComplete(w http.ResponseWriter, ctx context.Context, req *llmtrace.Request, p llmtrace.Provider, providerKey string) {
	var resp *llmtrace.Response
	var err error

	if s.cfg.Tracer != nil {
		resp, err = s.cfg.Tracer.Chat(ctx, req, p,
			llmtrace.WithCallMiddleware(llmtrace.Chain(s.cfg.Middlewares...)))
	} else {
		fn := p.Complete
		if len(s.cfg.Middlewares) > 0 {
			fn = llmtrace.Chain(s.cfg.Middlewares...)(fn)
		}
		resp, err = fn(ctx, req)
	}

	if err != nil {
		s.cfg.Logger.Error("proxy complete error", "provider", providerKey, "error", err)
		s.writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}

	apiResp := s.toAPIResponse(resp, req.Model)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiResp)
}

// handleStream handles streaming requests using SSE.
func (s *Server) handleStream(w http.ResponseWriter, ctx context.Context, req *llmtrace.Request, p llmtrace.Provider, providerKey string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "server_error", "Streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var ch <-chan llmtrace.StreamChunk
	var err error

	if s.cfg.Tracer != nil {
		ch, err = s.cfg.Tracer.ChatStream(ctx, req, p)
	} else {
		ch, err = p.Stream(ctx, req)
	}

	if err != nil {
		s.cfg.Logger.Error("proxy stream error", "provider", providerKey, "error", err)
		s.writeSSEError(w, flusher, err.Error())
		return
	}

	chunkID := 0
	for chunk := range ch {
		if chunk.Error != nil {
			s.cfg.Logger.Error("proxy stream chunk error", "provider", providerKey, "error", chunk.Error)
			s.writeSSEError(w, flusher, chunk.Error.Error())
			return
		}

		sc := streamChunk{
			ID:      fmt.Sprintf("chatcmpl-%d", chunkID),
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []streamChoice{
				{
					Index: 0,
					Delta: streamDelta{Content: chunk.Content},
				},
			},
		}

		if chunk.Usage != nil {
			sc.Usage = &usageResponse{
				PromptTokens:     chunk.Usage.InputTokens,
				CompletionTokens: chunk.Usage.OutputTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		data, _ := json.Marshal(sc)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		chunkID++
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleModels handles GET /v1/models.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	models := make([]modelInfo, 0, len(s.cfg.Providers))
	seen := make(map[string]bool)
	for key, entry := range s.cfg.Providers {
		if seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, modelInfo{
			ID:      key,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: entry.Provider.Name(),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })

	resp := modelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// withAuth wraps a handler with API key authentication.
func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.cfg.APIKey == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health endpoint is always open
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			s.writeError(w, http.StatusUnauthorized, "invalid_auth", "Missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != s.cfg.APIKey {
			s.writeError(w, http.StatusUnauthorized, "invalid_auth", "Invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withLogging wraps a handler with request logging.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.cfg.Logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start).String(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// toLLMRequest converts an OpenAI API request to llmtrace.Request.
func (s *Server) toLLMRequest(req *chatRequest) *llmtrace.Request {
	msgs := make([]llmtrace.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = llmtrace.Message{
			Role:    llmtrace.Role(m.Role),
			Content: m.Content,
		}
	}
	return &llmtrace.Request{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
	}
}

// toAPIResponse converts an llmtrace.Response to OpenAI API response.
func (s *Server) toAPIResponse(resp *llmtrace.Response, reqModel string) *chatResponse {
	model := resp.Model
	if model == "" {
		model = reqModel
	}
	finishReason := resp.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}
	return &chatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []choice{
			{
				Index: 0,
				Message: chatMessage{
					Role:    "assistant",
					Content: resp.Content,
				},
				FinishReason: finishReason,
			},
		},
		Usage: usageResponse{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

// writeError writes an OpenAI-format error response.
func (s *Server) writeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiError{
		Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{
			Message: message,
			Type:    errType,
			Code:    fmt.Sprintf("%d", status),
		},
	})
}

// writeSSEError writes an SSE error event.
func (s *Server) writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	errResp := map[string]string{"error": message, "type": "upstream_error"}
	data, _ := json.Marshal(errResp)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// readSSELines reads SSE lines from a reader, parsing out data payloads.
// Used by tests and the streaming proxy logic.
func readSSELines(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
	return lines, scanner.Err()
}
