// Package ai defines provider interfaces and data types for AI chat completions.
package ai

// Message represents a single chat completion message for an AI model.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest holds the parameters for a chat completion request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

// ChatResponse holds the result of a non-streaming chat completion.
type ChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk represents an incremental generation chunk from an AI provider stream.
type StreamChunk struct {
	ID           string `json:"id"`
	Delta        string `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}
