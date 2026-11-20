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

// ContainsPseudoToolJSON reports whether plain model text contains an unsupported
// internal pseudo-tool JSON payload (such as dalle.text2im blocks) rather than normal prose.
func ContainsPseudoToolJSON(text string) bool {
	_, found := SanitizeAssistantText(text)
	return found
}

// SanitizeAssistantText removes internal pseudo-tool JSON envelopes (such as
// {"action":"dalle.text2im",...} or {"tool":"generate_image",...} with optional
// markdown code fences) while preserving normal prose and legitimate JSON examples.
func SanitizeAssistantText(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return text, false
	}

	var (
		out      strings.Builder
		foundAny bool
		i        int
		n        = len(text)
	)

	for i < n {
		// 1. Check fenced markdown block containing a pseudo-tool JSON object
		if strings.HasPrefix(text[i:], "```") {
			if endIdx, isPseudo := matchFencedPseudoTool(text, i); isPseudo {
				foundAny = true
				i = endIdx
				continue
			}
		}

		// 2. Check standalone or inline balanced JSON object {...}
		if text[i] == '{' {
			if endIdx, ok := findBalancedJSONEnd(text, i); ok {
				candidate := text[i:endIdx]
				if isPseudoToolJSONObject(candidate) {
					foundAny = true
					i = endIdx
					continue
				}
			}
		}

		out.WriteByte(text[i])
		i++
	}

	if !foundAny {
		return text, false
	}

	cleaned := collapseExtraBlankLines(out.String())
	return cleaned, true
}

func matchFencedPseudoTool(text string, start int) (int, bool) {
	newlineIdx := strings.IndexByte(text[start:], '\n')
	if newlineIdx == -1 {
		return start, false
	}
	bodyStart := start + newlineIdx + 1
	closeRel := strings.Index(text[bodyStart:], "```")
	if closeRel == -1 {
		return start, false
	}
	bodyEnd := bodyStart + closeRel
	inner := strings.TrimSpace(text[bodyStart:bodyEnd])
	if !isPseudoToolJSONObject(inner) {
		return start, false
	}
	end := bodyEnd + 3
	for end < len(text) && (text[end] == '\r' || text[end] == '\n') {
		end++
	}
	return end, true
}

func findBalancedJSONEnd(s string, start int) (int, bool) {
	if start >= len(s) || s[start] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func isPseudoToolJSONObject(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return false
	}

	if action, ok := obj["action"].(string); ok {
		actionLower := strings.ToLower(strings.TrimSpace(action))
		if strings.Contains(actionLower, "dalle") || strings.Contains(actionLower, "text2im") || actionLower == "generate_image" {
			return true
		}
	}

	if tool, ok := obj["tool"].(string); ok {
		toolLower := strings.ToLower(strings.TrimSpace(tool))
		if toolLower == "generate_image" || strings.Contains(toolLower, "dalle") || strings.Contains(toolLower, "text2im") {
			return true
		}
	}

	// Catch internal ReAct-style {"thought": "...", "action_input": {"prompt": "..."}}
	if _, hasActionInput := obj["action_input"]; hasActionInput {
		if _, hasThought := obj["thought"]; hasThought {
			return true
		}
		if _, hasAction := obj["action"]; hasAction {
			return true
		}
	}

	return false
}

func collapseExtraBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blankRun := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
