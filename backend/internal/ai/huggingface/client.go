package huggingface

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

// ClientOptions configures the Hugging Face Inference Providers client.
type ClientOptions struct {
	BaseURL      string
	Token        string
	DefaultModel string
	Provider     string
	HTTPClient   *http.Client
}

// Client implements ai.Provider for Hugging Face Inference Providers via the OpenAI-compatible API.
type Client struct {
	baseURL      string
	token        string
	defaultModel string
	provider     string
	httpClient   *http.Client
}

var _ ai.Provider = (*Client)(nil)

// NewClient constructs a Hugging Face client with sensible transport defaults.
func NewClient(opts ClientOptions) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://router.huggingface.co/v1"
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		// Transport with connection reuse, dial timeout, but NO global client timeout
		// so streaming SSE connections are not terminated prematurely.
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          50,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}

	return &Client{
		baseURL:      baseURL,
		token:        strings.TrimSpace(opts.Token),
		defaultModel: strings.TrimSpace(opts.DefaultModel),
		provider:     strings.TrimSpace(opts.Provider),
		httpClient:   httpClient,
	}
}

// resolveModel determines the model string to send to the router.
// If provider is specified and model does not already contain a provider suffix (':'),
// the provider is appended as model:provider.
func (c *Client) resolveModel(requestedModel string) string {
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		model = c.defaultModel
	}
	if c.provider != "" && !strings.Contains(model, ":") {
		return model + ":" + c.provider
	}
	return model
}
