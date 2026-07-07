# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- **Provider Load Balancer** (`loadbalancer/`)
  - Distribute LLM requests across multiple provider instances
  - 4 strategies: RoundRobin, LeastLatency, Random, Weighted
  - Automatic health tracking with exponential moving average latency
  - Failover to healthy endpoints on provider errors
  - Configurable health check probes for unhealthy endpoint recovery
  - Thread-safe concurrent access with atomic counters
  - Implements `llmtrace.Provider` interface for seamless integration
  - 24 tests and 6 benchmarks

- **Streaming Metrics** (`streammetric/`)
  - Real-time performance metrics for streaming LLM responses
  - Time to First Token (TTFT) measurement
  - Inter-chunk latency tracking with P50/P99 percentiles
  - Tokens per second (TPS) throughput calculation
  - Live monitoring via `TTFT()` and `ChunkCount()` methods
  - `WithStreamMetrics()` StreamMiddleware for automatic per-call collection
  - Thread-safe with atomic operations for concurrent access
  - 16 tests and 4 benchmarks (137ns per record, zero allocs)

## [2.2.0] - 2026-07-05

### Added
- **Guardrails package** (`guardrails/`)
  - Composable input and output validators for LLM calls
  - `Gate` enforces rules as middleware in the call pipeline
  - 14 built-in rules: MaxPromptLength, MinPromptLength, MaxMessages, BlockedTerms, WarnedTerms, BlockedPattern, WarnedPattern, RequiredRoles, MinResponseLength, MaxResponseLength, RequiredFinishReason, BlockedOutputTerms, MaxTokenUsage, OutputMustMatch/NotMatch
  - `Severity` levels: Warn (log + allow) and Block (return error)
  - `FailOpen` mode for lenient enforcement
  - `OnViolation` callback for custom alerting/logging
  - `GateStats` for tracking violation counts by rule
  - `StreamMiddleware` support for streaming LLM calls
  - 29 tests covering all rules, gate behavior, callbacks, stats, and streaming

- **Token Counting & Context Window Management** (`tokencount/`)
  - Token estimation with configurable characters-per-token ratio
  - Context window validation for 15+ LLM models
  - Cost estimation before API calls
  - Conversation truncation to fit within limits
  - Model recommendations based on requirements
  - Built-in model registry with pricing and context window data

- **Prompt Template Management** (`prompt/`)
  - Versioned prompt templates with Go template syntax
  - Variable definitions with required/optional and defaults
  - Template rendering with validation
  - A/B testing with deterministic variant selection
  - Template diff for version comparison
  - Tag-based categorization and filtering

- **Multi-turn Conversation Session Tracking** (`session/`)
  - Session manager with configurable limits (max sessions, TTL, max turns)
  - Conversation history tracking with automatic token counting
  - System prompt and metadata support
  - Session statistics and health monitoring
  - Automatic cleanup of expired sessions
  - Thread-safe concurrent access

- **Token Counting Middleware** (`tokencount/`)
  - Automatic token usage tracking from LLM responses
  - Real-time cost accumulation based on model pricing
  - Per-model statistics breakdown
  - Middleware integration with existing call pipeline
  - Thread-safe for production use

## [2.1.0] - 2026-06-24

### Added
- **Real-time Dashboard** (`dashboard/`)
  - Full-featured web dashboard with Go embed, Chart.js, and dark theme
  - 6 dashboard pages: Overview, Providers, Models, Costs, Errors, Traces
  - SSE (Server-Sent Events) real-time metric updates
  - In-memory `TraceStore` for individual LLM call trace history
  - `/api/metrics`, `/api/traces`, `/api/traces/summary` endpoints
  - Auto-refresh charts with configurable intervals

- **Provider Health Analysis**
  - `/api/providers/health` endpoint with error rate, P50/P95/P99 latency percentiles
  - Cost per 1K tokens efficiency metrics and throughput tracking
  - Health score with status badges (healthy/degraded/unhealthy)
  - Latency percentile chart and cost efficiency chart on Providers page

- **Model Analysis Enhancements**
  - `/api/models/health`, `/api/models/compare`, `/api/models/rankings` endpoints
  - Model health cards, comparison table, and performance rankings
  - Token efficiency and latency analysis per model

- **Cost & Error Monitoring**
  - `/api/costs/trend` with daily cost aggregation from traces
  - `/api/costs/breakdown` with provider-level cost analysis
  - `/api/errors/trend` with daily error rate tracking
  - `/api/errors/recent` with recent error trace details
  - Enhanced Costs page with trend chart, provider breakdown table
  - Enhanced Errors page with error rate trend chart, recent errors table

- **Trace export package** (`traceexport/`)
  - `Exporter` interface for composable trace export destinations
  - `JSONExporter` — write traces as JSON arrays to files or `io.Writer`
  - `CSVExporter` — write traces as CSV rows with optional header, to files or `io.Writer`
  - `BatchExporter` — buffer traces and flush periodically or on size threshold
  - `RotateExporter` — auto-rotate output files by size or age, with max-files cleanup
  - 24 tests covering all exporters: file I/O, in-memory writers, batch flush, rotation, edge cases

- **Evaluation package** (`eval/`)
  - `Evaluator` interface for composable response quality checks
  - 13 built-in evaluators: MinLength, MaxLength, NonEmpty, Contains, ContainsAny, NotContains, ValidJSON, FinishReason, RegexMatch, TokenLimit, MaxLatency, ResponseID, Custom
  - `Suite` for grouping and running multiple evaluators together
  - `Suite.Middleware()` integrates with the llmtrace middleware pipeline
  - 45 tests covering all evaluators, suite logic, middleware integration, and edge cases

- **Integration tests & examples**
  - Example programs: basic, dashboard, middleware, streaming
  - Integration tests for cross-package workflows

## [1.0.0] - 2026-06-02

### Added
- **CI/CD pipelines** (`.github/workflows/`)
  - `ci.yml`: multi-version Go matrix testing (1.23/1.24/1.25), benchmarks, lint, and build verification on push/PR
  - `release.yml`: automated release on tag push with test validation and GitHub Release notes generation
- **Makefile enhancements**: `ci` target (vet + lint + test + bench), `clean` target

### Changed
- This is the first stable release of LLMTrace
- All features from v0.1.0 through v0.9.0 are included and battle-tested

## [0.9.0] - 2026-06-01

### Added
- **Structured logging with slog** (`slog.go`)
  - `WithSlog()` middleware for completion calls with configurable log levels
  - `WithStreamSlog()` middleware for streaming calls
  - `SlogConfig` with options for log levels, request/response/error logging control
  - Automatic error classification logging for `ProviderError` (provider, status_code, error_code, error_type)
  - Latency tracking in all log entries
  - Support for custom `slog.Logger` instances (falls back to `slog.Default()`)
  - 18 comprehensive tests covering success, error, disabled logging, nil logger, and integration scenarios

## [0.8.0] - 2026-05-31

### Added
- **Comprehensive README** with full documentation
  - Architecture diagram showing system components
  - Detailed sections for all features (providers, streaming, retry, rate limiting, middleware, metrics, errors)
  - Code examples for every major feature
  - Prometheus metrics reference table
  - Configuration guide with all options
  - Contributing guidelines
  - Go Reference and CI badges
- **MIT License** file (`LICENSE`)

## [0.7.0] - 2026-05-28

### Added
- **Comprehensive benchmark suite** (`bench_test.go`)
  - 35 benchmarks covering all major components: Tracer, CostCalculator, Retry, Limiter, Middleware, Chat, Errors, Provider
  - Parallel benchmarks for concurrent hot paths (CostCalculator, Limiter)
  - End-to-end Chat benchmarks with and without middleware/retry
  - Error classification and provider error benchmarks
- **Unified error types** (`errors.go`)
  - `ProviderError` struct with Provider, StatusCode, Code, Message, Type fields
  - `ErrorType` classification: auth, rate_limit, invalid_request, server_error, timeout, quota_exceeded, model_not_found, context_length
  - Sentinel errors: `ErrRateLimit`, `ErrAuth`, `ErrInvalidRequest`, `ErrServerError`, `ErrTimeout`, `ErrQuotaExceeded`, `ErrModelNotFound`, `ErrContextLengthExceeded`
  - Helper functions: `IsRateLimit()`, `IsAuthError()`, `IsServerError()`, `IsInvalidRequest()`, `IsTransient()`, `IsProviderError()`
  - `ClassifyHTTPStatus()` maps HTTP status codes to error types
  - `NewProviderError()` auto-classifies errors from status codes
- **Cross-provider integration tests** (`provider/provider_test.go`)
  - `TestProvider_InterfaceCompliance` — verifies all providers implement the interface
  - `TestProvider_CompleteRoundTrip` — end-to-end test with mock servers
  - `TestProvider_ErrorHandling` — consistent error behavior across providers
  - `TestProvider_EmptyMessages` — edge case handling
  - `TestProvider_ConcurrentRequests` — thread-safety verification
- 25+ new tests for error classification and provider integration

## [0.6.0] - 2026-05-27

### Added
- **Rate limiter middleware** (`ratelimit.go`)
  - `Limiter` struct with token bucket algorithm (configurable rate and burst)
  - `NewLimiter(rate float64, burst int)` creates a new rate limiter
  - `Allow()` / `AllowN()` for non-blocking token checks
  - `Wait()` / `WaitN()` for blocking token acquisition with context support
  - `WithRateLimit()` middleware for completion calls
  - `WithStreamRateLimit()` middleware for streaming calls
  - `WithCallRateLimit()` ChatOption for easy integration with Chat/ChatStream
  - `ErrRateLimitExceeded` error when rate limit cannot be satisfied
  - Thread-safe implementation with proper token refill logic
  - 20 comprehensive tests including concurrent access and context cancellation

## [0.5.0] - 2026-05-26

### Added
- **Retry with exponential backoff** (`retry.go`)
  - `RetryConfig` with configurable max retries, initial interval, max interval, multiplier, and jitter
  - `WithRetry()` and `WithRetryResult()` generic retry functions
  - `RetryableError` type to mark errors as retryable
  - `IsTransientError()` detects 429/5xx status codes and retryable errors
  - Context cancellation support during retry delays
- **Middleware pattern** (`middleware.go`)
  - `Middleware` type wraps `CompleteFunc` for request/response interceptors
  - `StreamMiddleware` type wraps `StreamFunc` for streaming interceptors
  - `Chain()` and `ChainStream()` compose multiple middlewares
  - `WithCompleteHook()` middleware for post-request callbacks
  - `WithTiming()` middleware for duration tracking
- **Chat convenience methods** (`chat.go`)
  - `Tracer.Chat()` accepts a `Provider` directly instead of `CompleteFunc`
  - `Tracer.ChatStream()` accepts a `Provider` directly instead of `StreamFunc`
  - `ChatOption` functional options: `WithCallRetry()`, `WithCallMiddleware()`
- **Examples directory** (`examples/basic/`)
  - Full usage demo with provider, cost tracking, retry, and hooks
- Comprehensive tests for retry, middleware, and chat (30+ new test cases)

### Fixed
- Stream() defer order: `span.End()` now runs before `close(out)` for reliable span export

## [0.4.0] - 2026-05-25

### Added
- Gemini provider implementation (`provider/gemini/`)
  - generateContent API with full request/response mapping
  - SSE streaming support via streamGenerateContent
  - System instruction handling (separate from message contents)
  - Assistant→model role mapping for Gemini's API format
  - 16 comprehensive tests with httptest mock server

## [0.3.0] - 2026-05-24

### Added
- Provider interface (`provider.go`) with Name, Complete, Stream, DefaultModel, SupportsStreaming
- ProviderConfig with functional options (WithAPIKey, WithBaseURL, WithModel, WithMaxRetries, WithExtra)
- OpenAI provider (`provider/openai/`) — Chat Completions API with SSE streaming
- Anthropic provider (`provider/anthropic/`) — Messages API with SSE streaming
- Comprehensive provider tests (15 OpenAI tests, 15 Anthropic tests)

## [0.2.0] - 2026-05-22

### Added
- Comprehensive test suite (28 tests across all packages)
- core_test.go: Tracer.Complete and Tracer.Stream with OTel span verification
- cost_test.go: CostCalculator pricing, calculation, concurrent access tests
- llmtrace_test.go: types, helpers, struct construction tests
- option_test.go: functional options tests
- internal/version/version_test.go: version info tests

### Fixed
- Stream() now preserves Error status instead of overriding with Ok
- Makefile module path updated from gollm to llmtrace
- version Info.String() now says "llmtrace" instead of "gollm"

## [0.1.0] - 2026-05-22

### Added
- Core tracer with OpenTelemetry GenAI semantic conventions
- Request/response attribute capture
- Token usage tracking
- Cost calculator with default pricing for GPT-4o, Claude, Gemini
- Tracer options (WithProvider, WithCostCalculator)
- Project scaffolding and CI/CD