package huggingface

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrTokenNotConfigured = errors.New("ai: Hugging Face API token is not configured")
	ErrUnauthorized       = errors.New("ai: unauthorized - invalid or missing Hugging Face API token")
	ErrRateLimit          = errors.New("ai: rate limit exceeded on Hugging Face inference provider")
	ErrModelUnavailable   = errors.New("ai: model or provider is currently loading or unavailable")
	ErrInvalidRequest     = errors.New("ai: invalid request payload or model configuration")
	ErrProviderFailure    = errors.New("ai: Hugging Face inference provider failed")
)

// ProviderError represents an error returned by the Hugging Face router API.
type ProviderError struct {
	StatusCode int
	ErrorCode  string
	Message    string
	RawBody    string
}

func (e *ProviderError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("huggingface API error (HTTP %d, code %s): %s", e.StatusCode, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("huggingface API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// sanitizeError removes the API token from any error message.
func sanitizeError(err error, token string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[REDACTED_HF_TOKEN]")
	}
	return errors.New(msg)
}
