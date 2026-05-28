// Package llmtrace provides unified error types for LLM provider interactions.
//
// All provider-specific errors can be wrapped into these common types,
// enabling consistent error handling across OpenAI, Anthropic, Gemini, etc.

package llmtrace

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for common LLM provider failure modes.
var (
	// ErrRateLimit indicates the provider rejected the request due to rate limiting.
	ErrRateLimit = errors.New("rate limit exceeded")

	// ErrAuth indicates authentication failed (invalid API key, expired token, etc.).
	ErrAuth = errors.New("authentication failed")

	// ErrInvalidRequest indicates the request was malformed or contained invalid parameters.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrServerError indicates an internal server error on the provider side.
	ErrServerError = errors.New("server error")

	// ErrTimeout indicates the request timed out.
	ErrTimeout = errors.New("request timeout")

	// ErrQuotaExceeded indicates the provider quota or billing limit was exceeded.
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrModelNotFound indicates the requested model does not exist or is not available.
	ErrModelNotFound = errors.New("model not found")

	// ErrContextLengthExceeded indicates the input exceeded the model's context window.
	ErrContextLengthExceeded = errors.New("context length exceeded")
)

// ProviderError represents a structured error from an LLM provider.
// All provider adapters should wrap their errors in this type for
// consistent error handling by consumers.
type ProviderError struct {
	// Provider is the provider name (e.g. "openai", "anthropic", "gemini").
	Provider string

	// StatusCode is the HTTP status code, if applicable (0 if not HTTP-based).
	StatusCode int

	// Code is the provider-specific error code (e.g. "invalid_api_key", "rate_limit").
	Code string

	// Message is the human-readable error description from the provider.
	Message string

	// Type classifies the error (e.g. "auth", "rate_limit", "invalid_request").
	Type ErrorType

	// Err is the underlying error, if any.
	Err error
}

// ErrorType classifies provider errors into standard categories.
type ErrorType string

const (
	ErrorTypeAuth           ErrorType = "auth"
	ErrorTypeRateLimit      ErrorType = "rate_limit"
	ErrorTypeInvalidRequest ErrorType = "invalid_request"
	ErrorTypeServerError    ErrorType = "server_error"
	ErrorTypeTimeout        ErrorType = "timeout"
	ErrorTypeQuotaExceeded  ErrorType = "quota_exceeded"
	ErrorTypeModelNotFound  ErrorType = "model_not_found"
	ErrorTypeContextLength   ErrorType = "context_length"
	ErrorTypeUnknown         ErrorType = "unknown"
)

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Provider, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// IsProviderError checks if an error is a ProviderError with the given type.
func IsProviderError(err error, t ErrorType) bool {
	var pe *ProviderError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Type == t
}

// IsRateLimit returns true if the error is a rate limit error.
func IsRateLimit(err error) bool {
	return errors.Is(err, ErrRateLimit) || IsProviderError(err, ErrorTypeRateLimit)
}

// IsAuthError returns true if the error is an authentication error.
func IsAuthError(err error) bool {
	return errors.Is(err, ErrAuth) || IsProviderError(err, ErrorTypeAuth)
}

// IsServerError returns true if the error is a server-side error.
func IsServerError(err error) bool {
	return errors.Is(err, ErrServerError) || IsProviderError(err, ErrorTypeServerError)
}

// IsInvalidRequest returns true if the error is an invalid request error.
func IsInvalidRequest(err error) bool {
	return errors.Is(err, ErrInvalidRequest) || IsProviderError(err, ErrorTypeInvalidRequest)
}

// IsTransient returns true if the error is likely transient and the request
// can be retried (rate limit or server error).
func IsTransient(err error) bool {
	return IsRateLimit(err) || IsServerError(err) || errors.Is(err, ErrTimeout)
}

// ClassifyHTTPStatus maps an HTTP status code to an ErrorType.
func ClassifyHTTPStatus(statusCode int) ErrorType {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return ErrorTypeAuth
	case statusCode == http.StatusTooManyRequests:
		return ErrorTypeRateLimit
	case statusCode == http.StatusBadRequest:
		return ErrorTypeInvalidRequest
	case statusCode == http.StatusNotFound:
		return ErrorTypeModelNotFound
	case statusCode == http.StatusRequestEntityTooLarge:
		return ErrorTypeContextLength
	case statusCode >= 500:
		return ErrorTypeServerError
	default:
		return ErrorTypeUnknown
	}
}

// NewProviderError creates a ProviderError with automatic type classification from status code.
func NewProviderError(provider string, statusCode int, message string) *ProviderError {
	return &ProviderError{
		Provider:   provider,
		StatusCode: statusCode,
		Message:    message,
		Type:       ClassifyHTTPStatus(statusCode),
	}
}
