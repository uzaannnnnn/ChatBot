package ai

import (
	"context"
	"io"
)

// Stream defines the interface for consuming streaming completion chunks.
type Stream interface {
	// Recv returns the next chunk in the stream.
	// When the stream terminates normally, Recv returns io.EOF.
	Recv() (StreamChunk, error)
	io.Closer
}

// Provider defines the contract for interacting with conversational AI backends.
type Provider interface {
	// Chat executes a non-streaming completion request.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// StreamChat initiates a streaming completion request.
	StreamChat(ctx context.Context, req ChatRequest) (Stream, error)
}
