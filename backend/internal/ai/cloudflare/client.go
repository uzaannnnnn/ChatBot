// Package cloudflare implements image generation using Cloudflare Workers AI
// with the @cf/black-forest-labs/flux-1-schnell model.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
)

const (
	DefaultBaseURL    = "https://api.cloudflare.com/client/v4"
	DefaultImageModel = "@cf/black-forest-labs/flux-1-schnell"
	DefaultTimeout    = 45 * time.Second

	maxResponseBodyBytes = 25 << 20 // 25 MiB cap for base64 JSON envelope
	maxErrorBodyBytes    = 4096
)

// ClientOptions configures the Cloudflare Workers AI image generation client.
type ClientOptions struct {
	BaseURL    string
	AccountID  string
	APIToken   string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client implements aiimage.Provider backed by Cloudflare Workers AI.
type Client struct {
	baseURL    string
	accountID  string
	apiToken   string
	model      string
	timeout    time.Duration
	httpClient *http.Client
}

var _ aiimage.Provider = (*Client)(nil)

// NewClient constructs a Cloudflare Workers AI image client.
func NewClient(opts ClientOptions) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = DefaultImageModel
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: timeout,
		}
	}

	return &Client{
		baseURL:    baseURL,
		accountID:  strings.TrimSpace(opts.AccountID),
		apiToken:   strings.TrimSpace(opts.APIToken),
		model:      model,
		timeout:    timeout,
		httpClient: httpClient,
	}
}

type fluxRequest struct {
	Prompt string `json:"prompt"`
}

type fluxResponse struct {
	Result struct {
		Image string `json:"image"`
	} `json:"result"`
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Generate sends a validated text prompt to Cloudflare Workers AI FLUX.1 schnell
// and returns validated binary image bytes and metadata.
func (c *Client) Generate(ctx context.Context, req aiimage.GenerateRequest) (aiimage.GenerateResult, error) {
	if c.accountID == "" || c.apiToken == "" {
		return aiimage.GenerateResult{}, aiimage.ErrProviderUnavailable
	}

	prompt, err := aiimage.ValidatePrompt(req.Prompt)
	if err != nil {
		return aiimage.GenerateResult{}, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	rawBody, err := json.Marshal(fluxRequest{Prompt: prompt})
	if err != nil {
		return aiimage.GenerateResult{}, fmt.Errorf("cloudflare: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/ai/run/%s", c.baseURL, c.accountID, c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return aiimage.GenerateResult{}, c.redactError(fmt.Errorf("cloudflare: build request: %w", err))
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return aiimage.GenerateResult{}, err
		}
		return aiimage.GenerateResult{}, fmt.Errorf("%w: %v", aiimage.ErrProviderFailure, c.redactError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return aiimage.GenerateResult{}, c.handleErrorResponse(resp)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return aiimage.GenerateResult{}, fmt.Errorf("%w: read response: %v", aiimage.ErrProviderFailure, c.redactError(err))
	}
	if len(bodyBytes) > maxResponseBodyBytes {
		return aiimage.GenerateResult{}, aiimage.ErrImageTooLarge
	}

	imageBytes, err := decodeImagePayload(resp.Header.Get("Content-Type"), bodyBytes)
	if err != nil {
		return aiimage.GenerateResult{}, err
	}

	mimeType, width, height, err := aiimage.ValidateImageBytes(imageBytes)
	if err != nil {
		return aiimage.GenerateResult{}, err
	}

	return aiimage.GenerateResult{
		Data:     imageBytes,
		MIMEType: mimeType,
		Width:    width,
		Height:   height,
		Prompt:   prompt,
		Model:    c.model,
	}, nil
}

func decodeImagePayload(contentType string, body []byte) ([]byte, error) {
	lowerCT := strings.ToLower(strings.TrimSpace(contentType))

	// Raw binary image fallback if provider returns image/* directly
	if strings.HasPrefix(lowerCT, "image/") {
		return body, nil
	}

	// Standard Cloudflare Workers AI JSON response: {"result": {"image": "<base64>"}, "success": true}
	var env fluxResponse
	if err := json.Unmarshal(body, &env); err != nil {
		// Last-resort check: if body itself starts with valid image magic bytes despite missing header
		if _, _, _, imgErr := aiimage.ValidateImageBytes(body); imgErr == nil {
			return body, nil
		}
		return nil, fmt.Errorf("%w: invalid JSON response from Cloudflare", aiimage.ErrProviderFailure)
	}

	if !env.Success && env.Result.Image == "" {
		if len(env.Errors) > 0 && env.Errors[0].Message != "" {
			return nil, fmt.Errorf("%w: %s", aiimage.ErrProviderFailure, env.Errors[0].Message)
		}
		return nil, fmt.Errorf("%w: Cloudflare returned unsuccessful response", aiimage.ErrProviderFailure)
	}

	encoded := strings.TrimSpace(env.Result.Image)
	if encoded == "" {
		return nil, fmt.Errorf("%w: Cloudflare returned empty image payload", aiimage.ErrProviderFailure)
	}

	// Strip optional data URI prefix (e.g. "data:image/jpeg;base64,...")
	if idx := strings.Index(encoded, ","); idx != -1 && strings.HasPrefix(strings.ToLower(encoded[:idx]), "data:") {
		encoded = strings.TrimSpace(encoded[idx+1:])
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid base64 image data", aiimage.ErrProviderFailure)
		}
	}

	return decoded, nil
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	msg := extractCloudflareErrorMessage(raw)
	msg = c.redactString(msg)

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: HTTP 400: %s", aiimage.ErrInvalidPrompt, msg)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d: %s", aiimage.ErrProviderAuth, resp.StatusCode, msg)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP 429: %s", aiimage.ErrProviderRateLimited, msg)
	default:
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%w: HTTP %d: %s", aiimage.ErrProviderFailure, resp.StatusCode, msg)
		}
		return fmt.Errorf("%w: HTTP %d: %s", aiimage.ErrProviderFailure, resp.StatusCode, msg)
	}
}

func extractCloudflareErrorMessage(raw []byte) string {
	var env fluxResponse
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Errors) > 0 && env.Errors[0].Message != "" {
		return env.Errors[0].Message
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "upstream error"
	}
	if len(trimmed) > 200 {
		return trimmed[:200] + "..."
	}
	return trimmed
}

func (c *Client) redactError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(c.redactString(err.Error()))
}

func (c *Client) redactString(s string) string {
	out := s
	if c.apiToken != "" {
		out = strings.ReplaceAll(out, c.apiToken, "[REDACTED]")
	}
	if c.accountID != "" {
		out = strings.ReplaceAll(out, c.accountID, "[REDACTED_ACCOUNT]")
	}
	return out
}
