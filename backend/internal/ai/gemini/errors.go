package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	ErrKeyNotConfigured = errors.New("ai: Gemini API key is not configured")
	ErrUnauthorized     = errors.New("ai: unauthorized - invalid or missing Gemini API key")
	ErrRateLimit        = errors.New("ai: rate limit or quota exceeded on Gemini provider")
	ErrModelUnavailable = errors.New("ai: Gemini model is unavailable or overloaded")
	ErrInvalidRequest   = errors.New("ai: invalid request payload or Gemini model configuration")
	ErrProviderFailure  = errors.New("ai: Gemini provider failed")
)

// ProviderError represents an error returned by the Google Gemini API.
type ProviderError struct {
	StatusCode int
	ErrorCode  string
	Status     string
	Message    string
	RawBody    string
}

func (e *ProviderError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("gemini API error (HTTP %d, code %s): %s", e.StatusCode, e.ErrorCode, e.Message)
	}
	if e.Status != "" {
		return fmt.Sprintf("gemini API error (HTTP %d, status %s): %s", e.StatusCode, e.Status, e.Message)
	}
	return fmt.Sprintf("gemini API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// sanitizeError removes the API key from any error message.
func sanitizeError(err error, apiKey string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if apiKey != "" {
		msg = strings.ReplaceAll(msg, apiKey, "[REDACTED_GEMINI_KEY]")
	}
	return errors.New(msg)
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*64))

	var rawErr struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bodyBytes, &rawErr)

	errMsg := rawErr.Error.Message
	if errMsg == "" {
		errMsg = string(bodyBytes)
	}
	if errMsg == "" {
		errMsg = http.StatusText(resp.StatusCode)
	}

	errCode := fmt.Sprintf("%v", rawErr.Error.Code)
	if errCode == "<nil>" || errCode == "0" {
		errCode = ""
	}

	provErr := &ProviderError{
		StatusCode: resp.StatusCode,
		ErrorCode:  errCode,
		Status:     rawErr.Error.Status,
		Message:    errMsg,
		RawBody:    string(bodyBytes),
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		rawErr.Error.Status == "UNAUTHENTICATED" || rawErr.Error.Status == "PERMISSION_DENIED":
		return fmt.Errorf("%w: %s", ErrUnauthorized, sanitizeError(provErr, c.apiKey))

	case resp.StatusCode == http.StatusTooManyRequests || rawErr.Error.Status == "RESOURCE_EXHAUSTED":
		return fmt.Errorf("%w: %s", ErrRateLimit, sanitizeError(provErr, c.apiKey))

	case resp.StatusCode == http.StatusBadRequest || rawErr.Error.Status == "INVALID_ARGUMENT":
		return fmt.Errorf("%w: %s", ErrInvalidRequest, sanitizeError(provErr, c.apiKey))

	case resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusGatewayTimeout || rawErr.Error.Status == "UNAVAILABLE":
		return fmt.Errorf("%w: %s", ErrModelUnavailable, sanitizeError(provErr, c.apiKey))

	default:
		return fmt.Errorf("%w: %s", ErrProviderFailure, sanitizeError(provErr, c.apiKey))
	}
}
