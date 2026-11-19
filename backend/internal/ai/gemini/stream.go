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
	"sort"
	"strings"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

type pendingToolCall struct {
	id          string
	name        string
	initialArgs json.RawMessage
	deltaArgs   strings.Builder
}

type geminiStream struct {
	reader        *bufio.Reader
	closer        io.Closer
	apiKey        string
	interactionID string
	pendingCalls  map[int]*pendingToolCall
	emittedCalls  map[string]struct{}
	queued        []ai.StreamChunk
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
		reader:       bufio.NewReader(resp.Body),
		closer:       resp.Body,
		apiKey:       c.apiKey,
		pendingCalls: make(map[int]*pendingToolCall),
		emittedCalls: make(map[string]struct{}),
	}, nil
}

type interactionStreamEvent struct {
	EventType     string `json:"event_type"`
	InteractionID string `json:"interaction_id"`
	Index         int    `json:"index"`
	Delta         struct {
		Type           string          `json:"type"`
		Text           string          `json:"text"`
		ID             string          `json:"id"`
		CallID         string          `json:"call_id"`
		Name           string          `json:"name"`
		Arguments      json.RawMessage `json:"arguments"`
		ArgumentsDelta string          `json:"arguments_delta"`
	} `json:"delta"`
	Step struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"step"`
	Content struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"content"`
	Interaction struct {
		ID      string            `json:"id"`
		Steps   []interactionStep `json:"steps"`
		Outputs []interactionStep `json:"outputs"`
	} `json:"interaction"`
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

func (s *geminiStream) popQueued() (ai.StreamChunk, bool) {
	if len(s.queued) == 0 {
		return ai.StreamChunk{}, false
	}
	chunk := s.queued[0]
	s.queued = s.queued[1:]
	return chunk, true
}

func (s *geminiStream) recordToolCall(tc ai.ToolCall) error {
	if tc.Name == "" {
		return nil
	}
	tc.Arguments = normalizeArguments(tc.Arguments)
	if !json.Valid(tc.Arguments) {
		return fmt.Errorf("gemini: malformed function_call arguments for %q: invalid JSON", tc.Name)
	}
	key := tc.ID
	if key == "" {
		key = tc.Name + ":" + string(tc.Arguments)
	}
	if _, exists := s.emittedCalls[key]; exists {
		return nil
	}
	s.emittedCalls[key] = struct{}{}
	s.queued = append(s.queued, ai.StreamChunk{
		InteractionID: s.interactionID,
		ToolCalls:     []ai.ToolCall{tc},
	})
	return nil
}

func (s *geminiStream) finalizePendingCall(idx int) error {
	pending, ok := s.pendingCalls[idx]
	if !ok {
		return nil
	}
	delete(s.pendingCalls, idx)

	var rawArgs json.RawMessage
	if accumulated := strings.TrimSpace(pending.deltaArgs.String()); accumulated != "" {
		rawArgs = json.RawMessage(accumulated)
	} else if trimmedInit := bytes.TrimSpace(pending.initialArgs); len(trimmedInit) > 0 && !bytes.Equal(trimmedInit, []byte("null")) {
		rawArgs = trimmedInit
	} else {
		rawArgs = json.RawMessage(`{}`)
	}

	return s.recordToolCall(ai.ToolCall{
		ID:        pending.id,
		Name:      pending.name,
		Arguments: rawArgs,
	})
}

func (s *geminiStream) flushPendingCalls() error {
	if len(s.pendingCalls) == 0 {
		return nil
	}
	indices := make([]int, 0, len(s.pendingCalls))
	for idx := range s.pendingCalls {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		if err := s.finalizePendingCall(idx); err != nil {
			return err
		}
	}
	return nil
}

func decodeArgumentsDelta(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	// Live Gemini Interactions API sends delta.arguments as a JSON-encoded string fragment.
	if trimmed[0] == '"' {
		var fragment string
		if err := json.Unmarshal(trimmed, &fragment); err != nil {
			return "", fmt.Errorf("gemini: invalid string-encoded arguments_delta: %w", err)
		}
		return fragment, nil
	}
	// Accept an inline JSON object if a provider variant sends an unquoted JSON object.
	if trimmed[0] == '{' {
		if !json.Valid(trimmed) {
			return "", errors.New("gemini: invalid JSON object in arguments_delta")
		}
		return string(trimmed), nil
	}
	return "", errors.New("gemini: unsupported arguments_delta value type")
}

// Recv reads and returns the next StreamChunk from the Gemini SSE stream.
func (s *geminiStream) Recv() (ai.StreamChunk, error) {
	if chunk, ok := s.popQueued(); ok {
		return chunk, nil
	}

	var currentEventName string

	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(strings.TrimSpace(line)) == 0 {
				if flushErr := s.flushPendingCalls(); flushErr != nil {
					return ai.StreamChunk{}, flushErr
				}
				if chunk, ok := s.popQueued(); ok {
					return chunk, nil
				}
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
			if trimmed == "" {
				currentEventName = ""
			}
			if errors.Is(err, io.EOF) {
				if flushErr := s.flushPendingCalls(); flushErr != nil {
					return ai.StreamChunk{}, flushErr
				}
				if chunk, ok := s.popQueued(); ok {
					return chunk, nil
				}
				return ai.StreamChunk{}, io.EOF
			}
			continue
		}

		if strings.HasPrefix(trimmed, "event:") {
			currentEventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			continue
		}

		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if payload == "[DONE]" {
			if flushErr := s.flushPendingCalls(); flushErr != nil {
				return ai.StreamChunk{}, flushErr
			}
			if chunk, ok := s.popQueued(); ok {
				return chunk, nil
			}
			return ai.StreamChunk{}, io.EOF
		}

		// 1. Try parsing as Gemini Interactions API event
		var evt interactionStreamEvent
		if jsonErr := json.Unmarshal([]byte(payload), &evt); jsonErr == nil {
			if evt.Interaction.ID != "" {
				s.interactionID = evt.Interaction.ID
			} else if evt.InteractionID != "" {
				s.interactionID = evt.InteractionID
			}

			eventType := evt.EventType
			if eventType == "" {
				eventType = currentEventName
			}

			// Handle step.start / content.start for streamed function calls
			if eventType == "step.start" && evt.Step.Type == "function_call" && evt.Step.Name != "" {
				callID := evt.Step.ID
				if callID == "" {
					callID = evt.Step.CallID
				}
				s.pendingCalls[evt.Index] = &pendingToolCall{
					id:          callID,
					name:        evt.Step.Name,
					initialArgs: evt.Step.Arguments,
				}
			} else if eventType == "content.start" && evt.Content.Type == "function_call" && evt.Content.Name != "" {
				callID := evt.Content.ID
				if callID == "" {
					callID = evt.Content.CallID
				}
				s.pendingCalls[evt.Index] = &pendingToolCall{
					id:          callID,
					name:        evt.Content.Name,
					initialArgs: evt.Content.Arguments,
				}
			}

			// Handle streamed function call argument deltas:
			// 1) Live Gemini Interactions API format: delta.type == "arguments_delta" with string-encoded delta.arguments
			if evt.Delta.Type == "arguments_delta" && len(bytes.TrimSpace(evt.Delta.Arguments)) > 0 {
				if p, ok := s.pendingCalls[evt.Index]; ok {
					fragment, decErr := decodeArgumentsDelta(evt.Delta.Arguments)
					if decErr != nil {
						return ai.StreamChunk{}, decErr
					}
					p.deltaArgs.WriteString(fragment)
				}
			}
			// 2) Backward-compatible field: delta.arguments_delta
			if evt.Delta.ArgumentsDelta != "" {
				if p, ok := s.pendingCalls[evt.Index]; ok {
					p.deltaArgs.WriteString(evt.Delta.ArgumentsDelta)
				}
			}

			// Handle step.stop / content.stop / step.end / step.complete flushing a pending function call
			if (eventType == "step.stop" || eventType == "content.stop" || eventType == "step.end" || eventType == "step.complete") && evt.PendingExists(s.pendingCalls) {
				if flushErr := s.finalizePendingCall(evt.Index); flushErr != nil {
					return ai.StreamChunk{}, flushErr
				}
			}

			// Handle single-frame function_call delta
			if evt.Delta.Type == "function_call" && evt.Delta.Name != "" {
				callID := evt.Delta.ID
				if callID == "" {
					callID = evt.Delta.CallID
				}
				if recErr := s.recordToolCall(ai.ToolCall{
					ID:        callID,
					Name:      evt.Delta.Name,
					Arguments: evt.Delta.Arguments,
				}); recErr != nil {
					return ai.StreamChunk{}, recErr
				}
			}

			// Handle text delta
			if evt.Delta.Text != "" {
				return ai.StreamChunk{
					InteractionID: s.interactionID,
					Delta:         evt.Delta.Text,
				}, nil
			}

			// Handle interaction completion carrying final outputs/steps
			if eventType == "interaction.complete" || eventType == "interaction.completed" {
				if flushErr := s.flushPendingCalls(); flushErr != nil {
					return ai.StreamChunk{}, flushErr
				}
				for _, step := range evt.Interaction.Steps {
					if step.Type == "function_call" && step.Name != "" {
						if recErr := s.recordToolCall(ai.ToolCall{
							ID:        step.resolvedID(),
							Name:      step.Name,
							Arguments: step.Arguments,
						}); recErr != nil {
							return ai.StreamChunk{}, recErr
						}
					}
				}
				for _, step := range evt.Interaction.Outputs {
					if step.Type == "function_call" && step.Name != "" {
						if recErr := s.recordToolCall(ai.ToolCall{
							ID:        step.resolvedID(),
							Name:      step.Name,
							Arguments: step.Arguments,
						}); recErr != nil {
							return ai.StreamChunk{}, recErr
						}
					}
				}
			}

			if chunk, ok := s.popQueued(); ok {
				return chunk, nil
			}
		}

		// 2. Fallback: streamGenerateContent candidates payload
		var candPayload candidateChunkPayload
		if jsonErr := json.Unmarshal([]byte(payload), &candPayload); jsonErr == nil {
			if len(candPayload.Candidates) > 0 {
				for _, part := range candPayload.Candidates[0].Content.Parts {
					if part.Text != "" {
						return ai.StreamChunk{
							InteractionID: s.interactionID,
							Delta:         part.Text,
						}, nil
					}
				}
			}
		}

		if errors.Is(err, io.EOF) {
			if flushErr := s.flushPendingCalls(); flushErr != nil {
				return ai.StreamChunk{}, flushErr
			}
			if chunk, ok := s.popQueued(); ok {
				return chunk, nil
			}
			return ai.StreamChunk{}, io.EOF
		}
	}
}

func (e interactionStreamEvent) PendingExists(m map[int]*pendingToolCall) bool {
	_, ok := m[e.Index]
	return ok
}

// Close closes the underlying HTTP response body.
func (s *geminiStream) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
