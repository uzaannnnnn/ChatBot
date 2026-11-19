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

type interactionStep struct {
	ID        string          `json:"id"`
	CallID    string          `json:"call_id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Text      string          `json:"text"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (s interactionStep) resolvedID() string {
	if s.ID != "" {
		return s.ID
	}
	return s.CallID
}

type interactionResponse struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	Steps      []interactionStep `json:"steps"`
	Outputs    []interactionStep `json:"outputs"`
	OutputText string            `json:"output_text"`
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
	var toolCalls []ai.ToolCall

	if result.OutputText != "" {
		output.WriteString(result.OutputText)
	}

	extractSteps := func(steps []interactionStep) {
		for _, step := range steps {
			switch step.Type {
			case "function_call":
				if step.Name != "" {
					toolCalls = append(toolCalls, ai.ToolCall{
						ID:        step.resolvedID(),
						Name:      step.Name,
						Arguments: normalizeArguments(step.Arguments),
					})
				}
			case "model_output":
				for _, part := range step.Content {
					if part.Type == "text" || part.Text != "" {
						output.WriteString(part.Text)
					}
				}
			case "text":
				if step.Text != "" {
					output.WriteString(step.Text)
				}
			}
		}
	}

	if len(result.Steps) > 0 {
		extractSteps(result.Steps)
	} else if len(result.Outputs) > 0 {
		extractSteps(result.Outputs)
	}

	return ai.ChatResponse{
		ID:        result.ID,
		Model:     result.Model,
		Role:      "assistant",
		Content:   output.String(),
		ToolCalls: toolCalls,
	}, nil
}

func normalizeArguments(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`)
	}
	return trimmed
}
