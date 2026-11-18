package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
)

func TestClient_RequestSerialization(t *testing.T) {
	var capturedBody string
	var capturedHeader http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header
		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBody = string(bodyBytes)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"text\":\"Hello\",\"type\":\"text\"}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL:      server.URL,
		APIKey:       "secret-test-key-12345",
		DefaultModel: "gemini-3.5-flash",
	})

	req := ai.ChatRequest{
		Model: "gemini-3.5-flash",
		Messages: []ai.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
		MaxTokens:   500,
		Temperature: 0.7,
	}

	stream, err := client.StreamChat(context.Background(), req)
	if err != nil {
		t.Fatalf("StreamChat unexpected error: %v", err)
	}
	defer stream.Close()

	if capturedHeader.Get("x-goog-api-key") != "secret-test-key-12345" {
		t.Errorf("expected x-goog-api-key header, got %q", capturedHeader.Get("x-goog-api-key"))
	}
	if capturedHeader.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", capturedHeader.Get("Content-Type"))
	}

	if !strings.Contains(capturedBody, `"system_instruction":"You are a helpful assistant."`) {
		t.Errorf("expected system_instruction in body, got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `User: Hello`) || !strings.Contains(capturedBody, `Assistant: Hi there!`) || !strings.Contains(capturedBody, `User: How are you?`) {
		t.Errorf("expected multi-turn dialog in input, got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"temperature":0.7`) || !strings.Contains(capturedBody, `"max_output_tokens":500`) {
		t.Errorf("expected generation_config in body, got: %s", capturedBody)
	}

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv unexpected error: %v", err)
	}
	if chunk.Delta != "Hello" {
		t.Errorf("expected delta 'Hello', got %q", chunk.Delta)
	}

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF on finish, got %v", err)
	}
}

func TestClient_SSEResponseParsing_FallbackCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Streaming \"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"content.\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("StreamChat unexpected error: %v", err)
	}
	defer stream.Close()

	c1, err := stream.Recv()
	if err != nil || c1.Delta != "Streaming " {
		t.Errorf("chunk 1 mismatch: delta=%q, err=%v", c1.Delta, err)
	}

	c2, err := stream.Recv()
	if err != nil || c2.Delta != "content." {
		t.Errorf("chunk 2 mismatch: delta=%q, err=%v", c2.Delta, err)
	}

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestClient_ErrorMappingAndRedaction(t *testing.T) {
	testKey := "super-secret-gemini-key-9999"

	tests := []struct {
		name        string
		status      int
		body        string
		expectedErr error
	}{
		{
			name:        "401 unauthorized",
			status:      http.StatusUnauthorized,
			body:        fmt.Sprintf(`{"error":{"code":401,"message":"API key %s is invalid","status":"UNAUTHENTICATED"}}`, testKey),
			expectedErr: ErrUnauthorized,
		},
		{
			name:        "403 forbidden",
			status:      http.StatusForbidden,
			body:        `{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`,
			expectedErr: ErrUnauthorized,
		},
		{
			name:        "429 rate limit",
			status:      http.StatusTooManyRequests,
			body:        `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			expectedErr: ErrRateLimit,
		},
		{
			name:        "400 bad request",
			status:      http.StatusBadRequest,
			body:        `{"error":{"code":400,"message":"Invalid argument provided","status":"INVALID_ARGUMENT"}}`,
			expectedErr: ErrInvalidRequest,
		},
		{
			name:        "503 model unavailable / high demand",
			status:      http.StatusServiceUnavailable,
			body:        `{"error":{"code":"service_unavailable","message":"gemini-3.5-flash is currently experiencing high demand"}}`,
			expectedErr: ErrModelUnavailable,
		},
		{
			name:        "500 provider failure",
			status:      http.StatusInternalServerError,
			body:        `{"error":{"code":500,"message":"Internal backend error"}}`,
			expectedErr: ErrProviderFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewClient(ClientOptions{
				BaseURL: server.URL,
				APIKey:  testKey,
			})

			_, err := client.StreamChat(context.Background(), ai.ChatRequest{
				Messages: []ai.Message{{Role: "user", Content: "hi"}},
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("expected error wrapping %v, got %v", tc.expectedErr, err)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("CRITICAL: error leaked secret API key: %s", err.Error())
			}
		})
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	hang := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if ok {
			_, _ = w.Write([]byte("event: step.delta\ndata: {\"index\":1,\"delta\":{\"text\":\"start\",\"type\":\"text\"}}\n\n"))
			flusher.Flush()
		}
		<-hang
	}))
	defer func() {
		close(hang)
		server.Close()
	}()

	client := NewClient(ClientOptions{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.StreamChat(ctx, ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil || chunk.Delta != "start" {
		t.Fatalf("first chunk failed: chunk=%v, err=%v", chunk, err)
	}

	cancel()

	done := make(chan error, 1)
	go func() {
		_, recvErr := stream.Recv()
		done <- recvErr
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected error on cancelled stream, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("stream.Recv did not return promptly after context cancellation")
	}
}

func TestClient_MissingKeyReturnsError(t *testing.T) {
	client := NewClient(ClientOptions{
		APIKey: "",
	})

	_, err := client.StreamChat(context.Background(), ai.ChatRequest{})
	if !errors.Is(err, ErrKeyNotConfigured) {
		t.Errorf("expected ErrKeyNotConfigured, got %v", err)
	}

	_, err = client.Chat(context.Background(), ai.ChatRequest{})
	if !errors.Is(err, ErrKeyNotConfigured) {
		t.Errorf("expected ErrKeyNotConfigured, got %v", err)
	}
}
