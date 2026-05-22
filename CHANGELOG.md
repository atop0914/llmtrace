# Changelog

All notable changes to this project will be documented in this file.

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
