package gemini

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

const (
	DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	DefaultModel   = "gemini-3.1-flash-lite"

	defaultDialTimeout           = 10 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
)

// ClientOptions configures the Gemini API client.
type ClientOptions struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
}

// Client implements the ai.Provider interface using the Google Gemini Interactions API.
type Client struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
}

var _ ai.Provider = (*Client)(nil)

// NewClient returns a new Client configured with the provided options.
func NewClient(opts ClientOptions) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = DefaultModel
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   defaultDialTimeout,
					KeepAlive: defaultKeepAlive,
				}).DialContext,
				TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
				ResponseHeaderTimeout: defaultResponseHeaderTimeout,
				ForceAttemptHTTP2:     true,
			},
			// Timeout is intentionally 0 to allow unbounded streaming connections.
			// Per-request timeouts are enforced via context.WithTimeout.
			Timeout: 0,
		}
	}

	return &Client{
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(opts.APIKey),
		defaultModel: model,
		httpClient:   httpClient,
	}
}

func (c *Client) resolveModel(requested string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	return c.defaultModel
}

// interactionRequest is the JSON payload sent to the Gemini Interactions API.
type interactionRequest struct {
	Model             string            `json:"model"`
	Input             string            `json:"input"`
	SystemInstruction string            `json:"system_instruction,omitempty"`
	Stream            bool              `json:"stream"`
	GenerationConfig  *generationConfig `json:"generation_config,omitempty"`
}

type generationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"max_output_tokens,omitempty"`
}

// buildInteractionPayload constructs an interactionRequest from an ai.ChatRequest.
func (c *Client) buildInteractionPayload(req ai.ChatRequest, stream bool) interactionRequest {
	model := c.resolveModel(req.Model)

	var systemInstructions []string
	var dialogParts []string

	for _, msg := range req.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		switch strings.ToLower(msg.Role) {
		case "system":
			systemInstructions = append(systemInstructions, content)
		case "user":
			if len(req.Messages) == 1 || (len(req.Messages) == 2 && len(systemInstructions) == 1) {
				dialogParts = append(dialogParts, content)
			} else {
				dialogParts = append(dialogParts, fmt.Sprintf("User: %s", content))
			}
		case "assistant", "model":
			dialogParts = append(dialogParts, fmt.Sprintf("Assistant: %s", content))
		default:
			dialogParts = append(dialogParts, fmt.Sprintf("%s: %s", msg.Role, content))
		}
	}

	input := strings.Join(dialogParts, "\n\n")
	systemInstruction := strings.Join(systemInstructions, "\n\n")

	var genCfg *generationConfig
	if req.Temperature > 0 || req.MaxTokens > 0 {
		genCfg = &generationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	return interactionRequest{
		Model:             model,
		Input:             input,
		SystemInstruction: systemInstruction,
		Stream:            stream,
		GenerationConfig:  genCfg,
	}
}
