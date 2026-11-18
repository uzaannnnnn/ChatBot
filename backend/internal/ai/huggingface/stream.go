package huggingface

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

type chatStreamChunkResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type hfStream struct {
	reader *bufio.Reader
	closer io.Closer
	token  string
}

var _ ai.Stream = (*hfStream)(nil)

// StreamChat initiates an SSE streaming completion request against the Hugging Face router.
func (c *Client) StreamChat(ctx context.Context, req ai.ChatRequest) (ai.Stream, error) {
	if c.token == "" {
		return nil, ErrTokenNotConfigured
	}

	model := c.resolveModel(req.Model)
	body := ai.ChatRequest{
		Model:       model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}

	rawJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal chat stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, sanitizeError(err, c.token)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.handleErrorResponse(resp)
	}

	return &hfStream{
		reader: bufio.NewReader(resp.Body),
		closer: resp.Body,
		token:  c.token,
	}, nil
}

// Recv reads and returns the next StreamChunk from the Hugging Face router SSE stream.
func (s *hfStream) Recv() (ai.StreamChunk, error) {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(strings.TrimSpace(line)) == 0 {
				return ai.StreamChunk{}, io.EOF
			}
			if !errors.Is(err, io.EOF) {
				return ai.StreamChunk{}, sanitizeError(err, s.token)
			}
		}

		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and SSE comments (e.g. ": ping")
		if trimmed == "" || strings.HasPrefix(trimmed, ":") {
			if errors.Is(err, io.EOF) {
				return ai.StreamChunk{}, io.EOF
			}
			continue
		}

		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if payload == "[DONE]" {
			return ai.StreamChunk{}, io.EOF
		}

		var chunk chatStreamChunkResponse
		if jsonErr := json.Unmarshal([]byte(payload), &chunk); jsonErr != nil {
			return ai.StreamChunk{}, fmt.Errorf("invalid json chunk from provider: %w", sanitizeError(jsonErr, s.token))
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		finishReason := ""
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}

		// Return chunk if it contains content or a finish reason
		if choice.Delta.Content != "" || finishReason != "" {
			return ai.StreamChunk{
				ID:           chunk.ID,
				Delta:        choice.Delta.Content,
				FinishReason: finishReason,
			}, nil
		}
	}
}

// Close releases the HTTP stream response body.
func (s *hfStream) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
