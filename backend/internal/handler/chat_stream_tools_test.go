package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/service"
)

type stubEventStreamSvc struct{}

func (s *stubEventStreamSvc) StreamMessage(
	ctx context.Context,
	ownerID, conversationID, userContent string,
	onChunk func(delta string) error,
) (domain.Message, domain.Message, error) {
	return domain.Message{}, domain.Message{}, nil
}

func (s *stubEventStreamSvc) StreamMessageWithEvents(
	ctx context.Context,
	ownerID, conversationID, userContent string,
	callbacks service.StreamCallbacks,
) (domain.Message, domain.Message, error) {
	if err := callbacks.OnToolStatus(service.ToolStatusEvent{Tool: "generate_image", Status: "running"}); err != nil {
		return domain.Message{}, domain.Message{}, err
	}
	if err := callbacks.OnToolStatus(service.ToolStatusEvent{Tool: "generate_image", Status: "completed"}); err != nil {
		return domain.Message{}, domain.Message{}, err
	}
	if err := callbacks.OnAttachment(domain.MessageAttachment{
		ID:       "33333333-3333-3333-3333-333333333333",
		Kind:     domain.AttachmentKindImage,
		URL:      "https://storage.example/signed/img.jpg",
		MIMEType: "image/jpeg",
		AltText:  "A golden retriever on a beach",
		Width:    1024,
		Height:   1024,
	}); err != nil {
		return domain.Message{}, domain.Message{}, err
	}
	if err := callbacks.OnDelta("Here is your golden retriever!"); err != nil {
		return domain.Message{}, domain.Message{}, err
	}
	return domain.Message{}, domain.Message{ID: "44444444-4444-4444-4444-444444444444"}, nil
}

func TestChatStreamHandler_ToolAndAttachmentEventSequence(t *testing.T) {
	r := newTestRouterWithStream(&stubEventStreamSvc{}, 1<<20)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/conversations/22222222-2222-2222-2222-222222222222/messages/stream",
		bytes.NewBufferString(`{"content":"Draw a golden retriever"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	expectedOrder := []string{
		"event:message_start",
		"event:tool_status",
		`"status":"running"`,
		`"status":"completed"`,
		"event:attachment",
		`"url":"https://storage.example/signed/img.jpg"`,
		"event:content_delta",
		"event:message_complete",
	}

	lastIdx := -1
	for _, token := range expectedOrder {
		idx := strings.Index(body, token)
		if idx == -1 {
			t.Fatalf("missing expected SSE token %q in body:\n%s", token, body)
		}
		if idx < lastIdx {
			t.Errorf("SSE token %q appeared out of order in body:\n%s", token, body)
		}
		lastIdx = idx
	}
	if strings.Contains(body, "data:image/") {
		t.Errorf("SSE stream must never include base64 data URIs")
	}
}
