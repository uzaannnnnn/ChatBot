package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

type interactionResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Steps []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"steps"`
	// Fallback field for potential future / alternative schema
	OutputText string `json:"output_text"`
}

// Chat executes a non-streaming completion request against the Gemini Interactions API.
func (c *Client) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	if c.apiKey == "" {
		return ai.ChatResponse{}, ErrKeyNotConfigured
	}

	payload := c.buildInteractionPayload(req, false)
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		return ai.ChatResponse{}, fmt.Errorf("marshal gemini chat request: %w", err)
	}

	url := fmt.Sprintf("%s/interactions", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawJSON))
	if err != nil {
		return ai.ChatResponse{}, fmt.Errorf("create gemini http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ai.ChatResponse{}, sanitizeError(err, c.apiKey)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ai.ChatResponse{}, c.handleErrorResponse(resp)
	}

	var result interactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ai.ChatResponse{}, fmt.Errorf("decode gemini response: %w", sanitizeError(err, c.apiKey))
	}

	var output strings.Builder
	if result.OutputText != "" {
		output.WriteString(result.OutputText)
	} else {
		for _, step := range result.Steps {
			if step.Type == "model_output" {
				for _, part := range step.Content {
					if part.Type == "text" || part.Text != "" {
						output.WriteString(part.Text)
					}
				}
			}
		}
	}

	return ai.ChatResponse{
		ID:      result.ID,
		Model:   result.Model,
		Role:    "assistant",
		Content: output.String(),
	}, nil
}
