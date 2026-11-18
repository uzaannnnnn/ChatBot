package huggingface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *apiErrorResponse `json:"error,omitempty"`
}

type apiErrorResponse struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// Chat executes a non-streaming chat completion request against the Hugging Face router.
func (c *Client) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	if c.token == "" {
		return ai.ChatResponse{}, ErrTokenNotConfigured
	}

	model := c.resolveModel(req.Model)
	body := ai.ChatRequest{
		Model:       model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}

	rawJSON, err := json.Marshal(body)
	if err != nil {
		return ai.ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(rawJSON))
	if err != nil {
		return ai.ChatResponse{}, fmt.Errorf("create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ai.ChatResponse{}, sanitizeError(err, c.token)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ai.ChatResponse{}, c.handleErrorResponse(resp)
	}

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return ai.ChatResponse{}, fmt.Errorf("decode chat completion response: %w", sanitizeError(err, c.token))
	}

	if len(completion.Choices) == 0 {
		return ai.ChatResponse{}, fmt.Errorf("ai provider returned no choices in response")
	}

	choice := completion.Choices[0]
	return ai.ChatResponse{
		ID:      completion.ID,
		Model:   completion.Model,
		Role:    choice.Message.Role,
		Content: choice.Message.Content,
	}, nil
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*64))
	var apiErr struct {
		Error apiErrorResponse `json:"error"`
	}
	_ = json.Unmarshal(bodyBytes, &apiErr)

	errMsg := apiErr.Error.Message
	if errMsg == "" {
		errMsg = string(bodyBytes)
	}
	if errMsg == "" {
		errMsg = http.StatusText(resp.StatusCode)
	}

	provErr := &ProviderError{
		StatusCode: resp.StatusCode,
		ErrorCode:  fmt.Sprintf("%v", apiErr.Error.Code),
		Message:    errMsg,
		RawBody:    string(bodyBytes),
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrUnauthorized, sanitizeError(provErr, c.token))
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrRateLimit, sanitizeError(provErr, c.token))
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrInvalidRequest, sanitizeError(provErr, c.token))
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", ErrModelUnavailable, sanitizeError(provErr, c.token))
	default:
		return fmt.Errorf("%w: %s", ErrProviderFailure, sanitizeError(provErr, c.token))
	}
}
