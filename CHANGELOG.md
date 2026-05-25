# Changelog

All notable changes to this project will be documented in this file.

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
