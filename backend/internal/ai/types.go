// Package ai defines provider interfaces and data types for AI chat completions,
// structured tool calling, and multimodal assistant orchestration.
package ai

import (
	"encoding/json"
	"strings"
)

// Message represents a single chat completion message for an AI model.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolDefinition declares a server-side tool that a model may invoke via structured function calling.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall represents a structured function call emitted by the AI provider.
// Plain text JSON emitted by a model is never treated as a ToolCall.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult represents the backend-controlled output of executing a ToolCall.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Result  any    `json:"result"`
	IsError bool   `json:"is_error,omitempty"`
}

// ChatRequest holds the parameters for a chat completion request.
type ChatRequest struct {
	Model                 string           `json:"model"`
	Messages              []Message        `json:"messages"`
	Tools                 []ToolDefinition `json:"tools,omitempty"`
	PreviousInteractionID string           `json:"previous_interaction_id,omitempty"`
	ToolResults           []ToolResult     `json:"tool_results,omitempty"`
	MaxTokens             int              `json:"max_tokens,omitempty"`
	Temperature           float64          `json:"temperature,omitempty"`
	Stream                bool             `json:"stream"`
}

// ChatResponse holds the result of a non-streaming chat completion.
type ChatResponse struct {
	ID        string     `json:"id"`
	Model     string     `json:"model"`
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// StreamChunk represents an incremental generation chunk from an AI provider stream.
type StreamChunk struct {
	ID            string     `json:"id"`
	InteractionID string     `json:"interaction_id,omitempty"`
	Delta         string     `json:"delta"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
	FinishReason  string     `json:"finish_reason,omitempty"`
}

// ContainsPseudoToolJSON reports whether plain model text appears to be an unsupported
// pseudo-tool JSON payload (such as OpenAI-style dalle.text2im blocks) rather than normal prose.
func ContainsPseudoToolJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, `"dalle.text2im"`) || strings.Contains(lower, `"action_input"`) && strings.Contains(lower, `"dalle`) {
		return true
	}
	// Strip optional markdown code fences to check raw JSON object payloads
	unwrapped := trimmed
	if strings.HasPrefix(unwrapped, "```") {
		if idx := strings.Index(unwrapped, "\n"); idx != -1 {
			unwrapped = strings.TrimSpace(unwrapped[idx+1:])
		}
		unwrapped = strings.TrimSpace(strings.TrimSuffix(unwrapped, "```"))
	}
	if strings.HasPrefix(unwrapped, "{") && strings.HasSuffix(unwrapped, "}") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(unwrapped), &obj); err == nil {
			if action, ok := obj["action"].(string); ok {
				actionLower := strings.ToLower(strings.TrimSpace(action))
				if strings.Contains(actionLower, "dalle") || strings.Contains(actionLower, "text2im") || actionLower == "generate_image" {
					return true
				}
			}
			if tool, ok := obj["tool"].(string); ok {
				toolLower := strings.ToLower(strings.TrimSpace(tool))
				if toolLower == "generate_image" || strings.Contains(toolLower, "dalle") {
					return true
				}
			}
		}
	}
	return false
}
