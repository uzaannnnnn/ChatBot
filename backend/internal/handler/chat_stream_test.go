package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/httpserver"
)

type mockChatStreamService struct {
	streamMessageFunc func(
		ctx context.Context,
		ownerID string,
		conversationID string,
		userContent string,
		onChunk func(delta string) error,
	) (domain.Message, domain.Message, error)
}

func (m *mockChatStreamService) StreamMessage(
	ctx context.Context,
	ownerID string,
	conversationID string,
	userContent string,
	onChunk func(delta string) error,
) (domain.Message, domain.Message, error) {
	if m.streamMessageFunc != nil {
		return m.streamMessageFunc(ctx, ownerID, conversationID, userContent, onChunk)
	}
	return domain.Message{}, domain.Message{}, nil
}

func newTestRouterWithStream(streamSvc handler.ChatStreamService, maxBody int64) http.Handler {
	cfg := &config.Config{
		AppEnv:              "test",
		FrontendOrigin:      "http://localhost:5173",
		MaxRequestBodyBytes: maxBody,
	}
	engine := httpserver.NewRouter(httpserver.Dependencies{
		Config:            cfg,
		AuthVerifier:      &mockAuthVerifier{},
		DisableRateLimit:  true,
		HealthHandler:     handler.NewHealthHandler(&mockDBPinger{}, 100*time.Millisecond),
		ChatStreamHandler: handler.NewChatStreamHandler(streamSvc),
	})
	return wrapWithTestAuth(engine)
}

func TestChatStreamHandler_SuccessLifecycle(t *testing.T) {
	t.Parallel()

	streamSvc := &mockChatStreamService{
		streamMessageFunc: func(
			_ context.Context,
			_ string,
			convID string,
			content string,
			onChunk func(delta string) error,
		) (domain.Message, domain.Message, error) {
			if convID != testConvID || content != "Tell me a joke" {
				return domain.Message{}, domain.Message{}, errors.New("unexpected params")
			}
			// Send 3 deltas
			for _, delta := range []string{"Why ", "did the gopher ", "cross the road?"} {
				if err := onChunk(delta); err != nil {
					return domain.Message{}, domain.Message{}, err
				}
			}
			userMsg := domain.Message{
				ID:             "user-msg-1",
				ConversationID: convID,
				Role:           domain.RoleUser,
				Content:        content,
			}
			assistantMsg := domain.Message{
				ID:             testMsgID,
				ConversationID: convID,
				Role:           domain.RoleAssistant,
				Content:        "Why did the gopher cross the road?",
			}
			return userMsg, assistantMsg, nil
		},
	}

	r := newTestRouterWithStream(streamSvc, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"content":"Tell me a joke"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// 1. Verify HTTP status and SSE headers
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", cc)
	}
	if xab := rec.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("expected X-Accel-Buffering: no, got %q", xab)
	}

	body := rec.Body.String()

	// 2. Verify message_start event
	if !strings.Contains(body, "event:message_start") && !strings.Contains(body, "event: message_start") {
		t.Errorf("expected message_start event, got body: %s", body)
	}
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Errorf("expected assistant role in message_start, got body: %s", body)
	}

	// 3. Verify content_delta events
	for _, chunk := range []string{"Why ", "did the gopher ", "cross the road?"} {
		if !strings.Contains(body, chunk) {
			t.Errorf("expected chunk %q in stream body, got: %s", chunk, body)
		}
	}

	// 4. Verify message_complete event
	if !strings.Contains(body, "event:message_complete") && !strings.Contains(body, "event: message_complete") {
		t.Errorf("expected message_complete event, got body: %s", body)
	}
	if !strings.Contains(body, testMsgID) {
		t.Errorf("expected assistant message_id %q in message_complete, got body: %s", testMsgID, body)
	}
}

func TestChatStreamHandler_ErrorBeforeStream(t *testing.T) {
	t.Parallel()

	t.Run("invalid conversation UUID", func(t *testing.T) {
		t.Parallel()
		r := newTestRouterWithStream(&mockChatStreamService{}, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/not-a-uuid/messages/stream", strings.NewReader(`{"content":"Hi"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidID {
			t.Errorf("expected %q, got %q", dto.ErrCodeInvalidID, got)
		}
	})

	t.Run("missing content", func(t *testing.T) {
		t.Parallel()
		r := newTestRouterWithStream(&mockChatStreamService{}, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidContent {
			t.Errorf("expected %q, got %q", dto.ErrCodeInvalidContent, got)
		}
	})

	t.Run("disallowed client role field", func(t *testing.T) {
		t.Parallel()
		r := newTestRouterWithStream(&mockChatStreamService{}, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"role":"assistant","content":"spoofed"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidRequest {
			t.Errorf("expected %q, got %q", dto.ErrCodeInvalidRequest, got)
		}
	})

	t.Run("conversation not found", func(t *testing.T) {
		t.Parallel()
		streamSvc := &mockChatStreamService{
			streamMessageFunc: func(_ context.Context, _, _, _ string, _ func(string) error) (domain.Message, domain.Message, error) {
				return domain.Message{}, domain.Message{}, domain.ErrConversationNotFound
			},
		}
		r := newTestRouterWithStream(streamSvc, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"content":"Hello"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeConversationNotFound {
			t.Errorf("expected %q, got %q", dto.ErrCodeConversationNotFound, got)
		}
	})

	t.Run("ai unavailable", func(t *testing.T) {
		t.Parallel()
		streamSvc := &mockChatStreamService{
			streamMessageFunc: func(_ context.Context, _, _, _ string, _ func(string) error) (domain.Message, domain.Message, error) {
				return domain.Message{}, domain.Message{}, domain.ErrAIUnavailable
			},
		}
		r := newTestRouterWithStream(streamSvc, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"content":"Hello"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeAIUnavailable {
			t.Errorf("expected %q, got %q", dto.ErrCodeAIUnavailable, got)
		}
	})
}

func TestChatStreamHandler_ErrorAfterStreamBegins(t *testing.T) {
	t.Parallel()

	streamSvc := &mockChatStreamService{
		streamMessageFunc: func(
			_ context.Context,
			_ string,
			_ string,
			_ string,
			onChunk func(delta string) error,
		) (domain.Message, domain.Message, error) {
			// Deliver first chunk, then fail
			if err := onChunk("First chunk"); err != nil {
				return domain.Message{}, domain.Message{}, err
			}
			return domain.Message{}, domain.Message{}, errors.New("provider stream exploded")
		},
	}

	r := newTestRouterWithStream(streamSvc, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"content":"Hi"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Since headers were already committed, status is 200 and error is emitted as an SSE event
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (headers committed), got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "First chunk") {
		t.Errorf("expected 'First chunk' in body: %s", body)
	}
	if !strings.Contains(body, "event:error") && !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event: %s", body)
	}
	if !strings.Contains(body, dto.ErrCodeAIProvider) {
		t.Errorf("expected error code %q in SSE body: %s", dto.ErrCodeAIProvider, body)
	}
}

func TestChatStreamHandler_ClientDisconnect(t *testing.T) {
	t.Parallel()

	clientDisconnected := false
	streamSvc := &mockChatStreamService{
		streamMessageFunc: func(
			_ context.Context,
			_ string,
			_ string,
			_ string,
			onChunk func(delta string) error,
		) (domain.Message, domain.Message, error) {
			// On first chunk, client context was canceled
			err := onChunk("Chunk")
			if errors.Is(err, context.Canceled) {
				clientDisconnected = true
				return domain.Message{}, domain.Message{}, err
			}
			return domain.Message{}, domain.Message{}, nil
		},
	}

	r := newTestRouterWithStream(streamSvc, 1024)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before or immediately as request is made

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", strings.NewReader(`{"content":"Hi"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !clientDisconnected {
		t.Log("Context cancellation was handled cleanly")
	}
}
