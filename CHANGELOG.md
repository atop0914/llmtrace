# Changelog

All notable changes to this project will be documented in this file.

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
