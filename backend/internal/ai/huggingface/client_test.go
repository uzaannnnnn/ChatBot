package huggingface_test

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
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai/huggingface"
)

const (
	testSecretToken = "hf_secret_abc123_xyz987"
	testModel       = "meta-llama/Llama-3.3-70B-Instruct"
)

func TestClient_TokenNotConfigured(t *testing.T) {
	t.Parallel()

	client := huggingface.NewClient(huggingface.ClientOptions{
		Token: "",
	})

	_, err := client.Chat(context.Background(), ai.ChatRequest{})
	if !errors.Is(err, huggingface.ErrTokenNotConfigured) {
		t.Fatalf("expected ErrTokenNotConfigured, got %v", err)
	}

	_, err = client.StreamChat(context.Background(), ai.ChatRequest{})
	if !errors.Is(err, huggingface.ErrTokenNotConfigured) {
		t.Fatalf("expected ErrTokenNotConfigured on stream, got %v", err)
	}
}

func TestClient_NonStreamingChat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testSecretToken {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"Invalid token"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": "chatcmpl-123",
			"model": "meta-llama/Llama-3.3-70B-Instruct:cheapest",
			"choices": [
				{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "Hello from mock Hugging Face!"
					},
					"finish_reason": "stop"
				}
			]
		}`))
	}))
	defer server.Close()

	client := huggingface.NewClient(huggingface.ClientOptions{
		BaseURL:      server.URL,
		Token:        testSecretToken,
		DefaultModel: testModel,
		Provider:     "cheapest",
	})

	resp, err := client.Chat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content != "Hello from mock Hugging Face!" {
		t.Errorf("expected content %q, got %q", "Hello from mock Hugging Face!", resp.Content)
	}
	if resp.Role != "assistant" {
		t.Errorf("expected role %q, got %q", "assistant", resp.Role)
	}
}

func TestClient_StreamingSuccessAndMultipleChunks(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		// Comment line to verify ping/comment skipping
		fmt.Fprintf(w, ": ping\n\n")
		flusher.Flush()

		// Chunk 1
		fmt.Fprintf(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		// Empty delta chunk (should be skipped)
		fmt.Fprintf(w, "data: {\"id\":\"c2\",\"choices\":[{\"index\":0,\"delta\":{}}]}\n\n")
		flusher.Flush()

		// Chunk 2
		fmt.Fprintf(w, "data: {\"id\":\"c3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world!\"}}]}\n\n")
		flusher.Flush()

		// Finish reason
		fmt.Fprintf(w, "data: {\"id\":\"c4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()

		// Termination
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := huggingface.NewClient(huggingface.ClientOptions{
		BaseURL:      server.URL,
		Token:        testSecretToken,
		DefaultModel: testModel,
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{
		Messages: []ai.Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	defer stream.Close()

	var chunks []string
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		if chunk.Delta != "" {
			chunks = append(chunks, chunk.Delta)
		}
	}

	got := strings.Join(chunks, "")
	if got != "Hello world!" {
		t.Fatalf("expected %q, got %q", "Hello world!", got)
	}
}

func TestClient_ProviderErrorsAndRedaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "unauthorized 401",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Invalid token: ` + testSecretToken + `"}}`,
			wantErr:    huggingface.ErrUnauthorized,
		},
		{
			name:       "rate limit 429",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"Too many requests, try again later"}}`,
			wantErr:    huggingface.ErrRateLimit,
		},
		{
			name:       "model unavailable 503",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error":{"message":"Model is currently loading"}}`,
			wantErr:    huggingface.ErrModelUnavailable,
		},
		{
			name:       "bad request 400",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"Invalid parameters"}}`,
			wantErr:    huggingface.ErrInvalidRequest,
		},
		{
			name:       "internal server error 500",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"Internal server error"}}`,
			wantErr:    huggingface.ErrProviderFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := huggingface.NewClient(huggingface.ClientOptions{
				BaseURL: server.URL,
				Token:   testSecretToken,
			})

			_, err := client.Chat(context.Background(), ai.ChatRequest{})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Chat: expected %v, got %v", tc.wantErr, err)
			}
			if strings.Contains(err.Error(), testSecretToken) {
				t.Errorf("Chat error leaked token: %v", err)
			}

			_, err = client.StreamChat(context.Background(), ai.ChatRequest{})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("StreamChat: expected %v, got %v", tc.wantErr, err)
			}
			if strings.Contains(err.Error(), testSecretToken) {
				t.Errorf("StreamChat error leaked token: %v", err)
			}
		})
	}
}

func TestClient_InvalidJSONChunk(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {not valid json}\n\n")
	}))
	defer server.Close()

	client := huggingface.NewClient(huggingface.ClientOptions{
		BaseURL: server.URL,
		Token:   testSecretToken,
	})

	stream, err := client.StreamChat(context.Background(), ai.ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error on invalid JSON chunk")
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	blocker := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Chunk1\"}}]}\n\n")
			flusher.Flush()
		}
		<-r.Context().Done()
		<-blocker
	}))
	defer server.Close()
	defer close(blocker)

	ctx, cancel := context.WithCancel(context.Background())
	client := huggingface.NewClient(huggingface.ClientOptions{
		BaseURL: server.URL,
		Token:   testSecretToken,
	})

	stream, err := client.StreamChat(ctx, ai.ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	defer stream.Close()

	chunk1, err := stream.Recv()
	if err != nil || chunk1.Delta != "Chunk1" {
		t.Fatalf("expected Chunk1, got %v (err=%v)", chunk1, err)
	}

	cancel() // Cancel context while waiting for next chunk
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
}

func TestClient_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	client := huggingface.NewClient(huggingface.ClientOptions{
		BaseURL: server.URL,
		Token:   testSecretToken,
	})

	_, err := client.Chat(ctx, ai.ChatRequest{})
	if err == nil {
		t.Fatal("expected error on timeout")
	}
}
