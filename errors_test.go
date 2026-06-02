package llmtrace

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestProviderError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *ProviderError
		want string
	}{
		{
			name: "with underlying error",
			err: &ProviderError{
				Provider: "openai",
				Message:  "rate limit",
				Err:      fmt.Errorf("429 Too Many Requests"),
			},
			want: "openai: rate limit: 429 Too Many Requests",
		},
		{
			name: "without underlying error",
			err: &ProviderError{
				Provider: "anthropic",
				Message:  "invalid API key",
			},
			want: "anthropic: invalid API key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	pe := &ProviderError{Provider: "gemini", Err: inner}

	if !errors.Is(pe, inner) {
		t.Error("errors.Is should match inner error")
	}
}

func TestIsProviderError(t *testing.T) {
	pe := &ProviderError{
		Provider: "openai",
		Type:     ErrorTypeRateLimit,
	}

	if !IsProviderError(pe, ErrorTypeRateLimit) {
		t.Error("should match rate limit type")
	}
	if IsProviderError(pe, ErrorTypeAuth) {
		t.Error("should not match auth type")
	}
	if IsProviderError(errors.New("random"), ErrorTypeRateLimit) {
		t.Error("should not match non-ProviderError")
	}
}

func TestIsRateLimit(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", ErrRateLimit, true},
		{"provider error", &ProviderError{Type: ErrorTypeRateLimit}, true},
		{"wrapped sentinel", fmt.Errorf("wrapped: %w", ErrRateLimit), true},
		{"auth error", &ProviderError{Type: ErrorTypeAuth}, false},
		{"plain error", errors.New("some error"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRateLimit(tt.err); got != tt.want {
				t.Errorf("IsRateLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", ErrAuth, true},
		{"provider error", &ProviderError{Type: ErrorTypeAuth}, true},
		{"rate limit", &ProviderError{Type: ErrorTypeRateLimit}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthError(tt.err); got != tt.want {
				t.Errorf("IsAuthError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsServerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", ErrServerError, true},
		{"provider error", &ProviderError{Type: ErrorTypeServerError}, true},
		{"rate limit", &ProviderError{Type: ErrorTypeRateLimit}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsServerError(tt.err); got != tt.want {
				t.Errorf("IsServerError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", ErrInvalidRequest, true},
		{"provider error", &ProviderError{Type: ErrorTypeInvalidRequest}, true},
		{"server error", &ProviderError{Type: ErrorTypeServerError}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInvalidRequest(tt.err); got != tt.want {
				t.Errorf("IsInvalidRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"rate limit sentinel", ErrRateLimit, true},
		{"server error sentinel", ErrServerError, true},
		{"timeout sentinel", ErrTimeout, true},
		{"rate limit provider", &ProviderError{Type: ErrorTypeRateLimit}, true},
		{"server error provider", &ProviderError{Type: ErrorTypeServerError}, true},
		{"auth error", &ProviderError{Type: ErrorTypeAuth}, false},
		{"invalid request", &ProviderError{Type: ErrorTypeInvalidRequest}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransient(tt.err); got != tt.want {
				t.Errorf("IsTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		code int
		want ErrorType
	}{
		{http.StatusUnauthorized, ErrorTypeAuth},
		{http.StatusForbidden, ErrorTypeAuth},
		{http.StatusTooManyRequests, ErrorTypeRateLimit},
		{http.StatusBadRequest, ErrorTypeInvalidRequest},
		{http.StatusNotFound, ErrorTypeModelNotFound},
		{http.StatusRequestEntityTooLarge, ErrorTypeContextLength},
		{http.StatusInternalServerError, ErrorTypeServerError},
		{http.StatusBadGateway, ErrorTypeServerError},
		{http.StatusServiceUnavailable, ErrorTypeServerError},
		{http.StatusOK, ErrorTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			if got := ClassifyHTTPStatus(tt.code); got != tt.want {
				t.Errorf("ClassifyHTTPStatus(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestNewProviderError(t *testing.T) {
	pe := NewProviderError("openai", http.StatusTooManyRequests, "slow down")
	if pe.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", pe.Provider)
	}
	if pe.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
	if pe.Type != ErrorTypeRateLimit {
		t.Errorf("Type = %q, want rate_limit", pe.Type)
	}
	if pe.Message != "slow down" {
		t.Errorf("Message = %q, want 'slow down'", pe.Message)
	}
}

func TestSentinelErrors_Unwrap(t *testing.T) {
	// Sentinel errors should work with errors.Is
	err := fmt.Errorf("wrapped: %w", ErrRateLimit)
	if !errors.Is(err, ErrRateLimit) {
		t.Error("errors.Is should match wrapped ErrRateLimit")
	}
}
