package gemini

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

type geminiStream struct {
	reader *bufio.Reader
	closer io.Closer
	apiKey string
}

var _ ai.Stream = (*geminiStream)(nil)

// StreamChat initiates an SSE streaming completion request against the Gemini Interactions API.
func (c *Client) StreamChat(ctx context.Context, req ai.ChatRequest) (ai.Stream, error) {
	if c.apiKey == "" {
		return nil, ErrKeyNotConfigured
	}

	payload := c.buildInteractionPayload(req, true)
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini stream request: %w", err)
	}

	url := fmt.Sprintf("%s/interactions", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("create gemini stream request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, sanitizeError(err, c.apiKey)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.handleErrorResponse(resp)
	}

	return &geminiStream{
		reader: bufio.NewReader(resp.Body),
		closer: resp.Body,
		apiKey: c.apiKey,
	}, nil
}

type interactionDeltaPayload struct {
	Index int `json:"index"`
	Delta struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"delta"`
	EventType string `json:"event_type"`
}

type candidateChunkPayload struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Recv reads and returns the next StreamChunk from the Gemini SSE stream.
func (s *geminiStream) Recv() (ai.StreamChunk, error) {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(strings.TrimSpace(line)) == 0 {
				return ai.StreamChunk{}, io.EOF
			}
			if !errors.Is(err, io.EOF) {
				return ai.StreamChunk{}, sanitizeError(err, s.apiKey)
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

		// 1. Try parsing as Interactions API delta payload
		var deltaPayload interactionDeltaPayload
		if jsonErr := json.Unmarshal([]byte(payload), &deltaPayload); jsonErr == nil {
			if deltaPayload.Delta.Text != "" {
				return ai.StreamChunk{
					Delta: deltaPayload.Delta.Text,
				}, nil
			}
		}

		// 2. Try parsing as fallback candidate content payload (streamGenerateContent format)
		var candPayload candidateChunkPayload
		if jsonErr := json.Unmarshal([]byte(payload), &candPayload); jsonErr == nil {
			if len(candPayload.Candidates) > 0 {
				for _, part := range candPayload.Candidates[0].Content.Parts {
					if part.Text != "" {
						return ai.StreamChunk{
							Delta: part.Text,
						}, nil
					}
				}
			}
		}

		if errors.Is(err, io.EOF) {
			return ai.StreamChunk{}, io.EOF
		}
	}
}

// Close closes the underlying HTTP response body.
func (s *geminiStream) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
