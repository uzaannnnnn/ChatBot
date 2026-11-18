package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/auth"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/httpserver"
)

const testConvID = "123e4567-e89b-12d3-a456-426614174000"
const testMsgID = "987e6543-e21b-12d3-a456-426614174111"

type mockConversationService struct {
	createFunc      func(ctx context.Context, ownerID, title string) (domain.Conversation, error)
	getFunc         func(ctx context.Context, ownerID, id string) (domain.Conversation, error)
	listPageFunc    func(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, bool, error)
	updateTitleFunc func(ctx context.Context, ownerID, id, title string) (domain.Conversation, error)
	deleteFunc      func(ctx context.Context, ownerID, id string) error
}

func (m *mockConversationService) Create(ctx context.Context, ownerID, title string) (domain.Conversation, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, ownerID, title)
	}
	return domain.Conversation{}, nil
}

func (m *mockConversationService) Get(ctx context.Context, ownerID, id string) (domain.Conversation, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, ownerID, id)
	}
	return domain.Conversation{}, nil
}

func (m *mockConversationService) ListPage(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, bool, error) {
	if m.listPageFunc != nil {
		return m.listPageFunc(ctx, ownerID, page)
	}
	return nil, false, nil
}

func (m *mockConversationService) UpdateTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error) {
	if m.updateTitleFunc != nil {
		return m.updateTitleFunc(ctx, ownerID, id, title)
	}
	return domain.Conversation{}, nil
}

func (m *mockConversationService) Delete(ctx context.Context, ownerID, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, ownerID, id)
	}
	return nil
}

type mockMessageService struct {
	createFunc   func(ctx context.Context, ownerID, conversationID string, role domain.Role, content string) (domain.Message, error)
	listPageFunc func(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, bool, error)
}

func (m *mockMessageService) Create(ctx context.Context, ownerID, conversationID string, role domain.Role, content string) (domain.Message, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, ownerID, conversationID, role, content)
	}
	return domain.Message{}, nil
}

func (m *mockMessageService) ListPage(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, bool, error) {
	if m.listPageFunc != nil {
		return m.listPageFunc(ctx, ownerID, conversationID, page)
	}
	return nil, false, nil
}

type mockDBPinger struct {
	err error
}

func (m *mockDBPinger) Ping(_ context.Context) error {
	return m.err
}

const testUserID = "11111111-1111-4111-8111-111111111111"

type mockAuthVerifier struct{}

func (m *mockAuthVerifier) VerifyToken(_ context.Context, token string) (string, error) {
	if token == "valid-token" {
		return testUserID, nil
	}
	if token == "user-b-token" {
		return "22222222-2222-4222-8222-222222222222", nil
	}
	return "", auth.ErrInvalidToken
}

func wrapWithTestAuth(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-No-Auth") == "" && r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer valid-token")
		}
		inner.ServeHTTP(w, r)
	})
}

func newTestRouter(convSvc handler.ConversationService, msgSvc handler.MessageService, maxBody int64) http.Handler {
	cfg := &config.Config{
		AppEnv:              "test",
		FrontendOrigin:      "http://localhost:5173",
		MaxRequestBodyBytes: maxBody,
	}
	engine := httpserver.NewRouter(httpserver.Dependencies{
		Config:              cfg,
		AuthVerifier:        &mockAuthVerifier{},
		DisableRateLimit:    true,
		HealthHandler:       handler.NewHealthHandler(&mockDBPinger{}, 100*time.Millisecond),
		ConversationHandler: handler.NewConversationHandler(convSvc),
		MessageHandler:      handler.NewMessageHandler(msgSvc),
	})
	return wrapWithTestAuth(engine)
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) dto.ErrorResponse {
	t.Helper()
	var resp dto.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode ErrorResponse (%q): %v", rec.Body.String(), err)
	}
	return resp
}

func TestConversationHandler_Create(t *testing.T) {
	t.Parallel()

	fixedTime := time.Date(2026, 11, 17, 13, 0, 0, 0, time.FixedZone("ICT", 7*3600))

	t.Run("success with title and UTC conversion", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			createFunc: func(_ context.Context, _ string, title string) (domain.Conversation, error) {
				if title != "My Chat" {
					t.Errorf("expected title %q, got %q", "My Chat", title)
				}
				return domain.Conversation{
					ID:        testConvID,
					Title:     title,
					CreatedAt: fixedTime,
					UpdatedAt: fixedTime,
				}, nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{"title":"My Chat"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"2026-11-17T06:00:00Z"`) {
			t.Errorf("expected UTC timestamp ending in Z, got %s", rec.Body.String())
		}
	})

	t.Run("success with empty body uses service default", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			createFunc: func(_ context.Context, _ string, title string) (domain.Conversation, error) {
				if title != "" {
					t.Errorf("expected empty title passed to service, got %q", title)
				}
				return domain.Conversation{
					ID:        testConvID,
					Title:     domain.DefaultConversationTitle,
					CreatedAt: fixedTime.UTC(),
					UpdatedAt: fixedTime.UTC(),
				}, nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid title over 200 chars", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			createFunc: func(_ context.Context, _, _ string) (domain.Conversation, error) {
				return domain.Conversation{}, domain.ErrInvalidTitle
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 4096)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{"title":"too long"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		errResp := decodeError(t, rec)
		if errResp.Error.Code != dto.ErrCodeInvalidTitle {
			t.Errorf("expected error code %q, got %q", dto.ErrCodeInvalidTitle, errResp.Error.Code)
		}
	})
}

func TestConversationHandler_List(t *testing.T) {
	t.Parallel()

	t.Run("default pagination and has_more", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			listPageFunc: func(_ context.Context, _ string, page domain.Page) ([]domain.Conversation, bool, error) {
				if page.Limit != 2 || page.Offset != 4 {
					t.Errorf("expected limit=2 offset=4, got %+v", page)
				}
				return []domain.Conversation{
					{ID: testConvID, Title: "Chat 1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				}, true, nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?limit=2&offset=4", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp dto.ListResponse[dto.ConversationResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Data) != 1 || resp.Pagination.Limit != 2 || resp.Pagination.Offset != 4 || !resp.Pagination.HasMore {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("invalid pagination query parameters", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

		for _, query := range []string{"?limit=0", "?limit=101", "?limit=abc", "?offset=-1", "?offset=xyz", "?limit="} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations"+query, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("query %s: expected 400, got %d", query, rec.Code)
			}
			errResp := decodeError(t, rec)
			if errResp.Error.Code != dto.ErrCodeInvalidPagination {
				t.Errorf("query %s: expected code %q, got %q", query, dto.ErrCodeInvalidPagination, errResp.Error.Code)
			}
		}
	})
}

func TestConversationHandler_GetUpdateDelete(t *testing.T) {
	t.Parallel()

	t.Run("get success", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			getFunc: func(_ context.Context, _ string, id string) (domain.Conversation, error) {
				return domain.Conversation{ID: id, Title: "Found"}, nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+testConvID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("get invalid UUID returns 400", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/not-a-uuid", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidID {
			t.Errorf("expected %q, got %q", dto.ErrCodeInvalidID, got)
		}
	})

	t.Run("get not found returns 404", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			getFunc: func(_ context.Context, _, _ string) (domain.Conversation, error) {
				return domain.Conversation{}, domain.ErrConversationNotFound
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+testConvID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeConversationNotFound {
			t.Errorf("expected %q, got %q", dto.ErrCodeConversationNotFound, got)
		}
	})

	t.Run("update title success", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			updateTitleFunc: func(_ context.Context, _ string, id, title string) (domain.Conversation, error) {
				return domain.Conversation{ID: id, Title: title}, nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+testConvID, strings.NewReader(`{"title":"Updated"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("update title missing or blank rejected", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			updateTitleFunc: func(_ context.Context, _, _, _ string) (domain.Conversation, error) {
				return domain.Conversation{}, domain.ErrInvalidTitle
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		for _, body := range []string{`{}`, `{"title":"   "}`} {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+testConvID, strings.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %s: expected 400, got %d", body, rec.Code)
			}
			if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidTitle {
				t.Errorf("body %s: expected %q, got %q", body, dto.ErrCodeInvalidTitle, got)
			}
		}
	})

	t.Run("delete success returns 204 and not found returns 404", func(t *testing.T) {
		t.Parallel()
		deleted := false
		convSvc := &mockConversationService{
			deleteFunc: func(_ context.Context, _, _ string) error {
				if deleted {
					return domain.ErrConversationNotFound
				}
				deleted = true
				return nil
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req1 := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/"+testConvID, nil)
		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec1.Code)
		}

		req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/"+testConvID, nil)
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec2.Code)
		}
	})
}

func TestMessageHandler_CreateAndList(t *testing.T) {
	t.Parallel()

	t.Run("create user message assigns role user server-side", func(t *testing.T) {
		t.Parallel()
		msgSvc := &mockMessageService{
			createFunc: func(_ context.Context, _, convID string, role domain.Role, content string) (domain.Message, error) {
				if role != domain.RoleUser {
					t.Fatalf("expected server-assigned role %q, got %q", domain.RoleUser, role)
				}
				return domain.Message{
					ID:             testMsgID,
					ConversationID: convID,
					Role:           role,
					Content:        content,
					CreatedAt:      time.Now().UTC(),
				}, nil
			},
		}
		r := newTestRouter(&mockConversationService{}, msgSvc, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages", strings.NewReader(`{"content":"Hello!"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp dto.SingleResponse[dto.MessageResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Data.Role != domain.RoleUser || resp.Data.Content != "Hello!" {
			t.Errorf("unexpected message response: %+v", resp.Data)
		}
	})

	t.Run("reject client-supplied role field", func(t *testing.T) {
		t.Parallel()
		called := false
		msgSvc := &mockMessageService{
			createFunc: func(_ context.Context, _, _ string, _ domain.Role, _ string) (domain.Message, error) {
				called = true
				return domain.Message{}, nil
			},
		}
		r := newTestRouter(&mockConversationService{}, msgSvc, 1024)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages", strings.NewReader(`{"role":"assistant","content":"spoofed"}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when client sends role field, got %d", rec.Code)
		}
		if called {
			t.Fatal("service must not be called when client sends disallowed role field")
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidRequest {
			t.Errorf("expected %q, got %q", dto.ErrCodeInvalidRequest, got)
		}
	})

	t.Run("reject missing or blank content", func(t *testing.T) {
		t.Parallel()
		msgSvc := &mockMessageService{
			createFunc: func(_ context.Context, _, _ string, _ domain.Role, _ string) (domain.Message, error) {
				return domain.Message{}, domain.ErrInvalidContent
			},
		}
		r := newTestRouter(&mockConversationService{}, msgSvc, 1024)

		for _, body := range []string{`{}`, `{"content":"   "}`} {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages", strings.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %s: expected 400, got %d", body, rec.Code)
			}
			if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidContent {
				t.Errorf("body %s: expected %q, got %q", body, dto.ErrCodeInvalidContent, got)
			}
		}
	})

	t.Run("invalid conversation UUID and not found", func(t *testing.T) {
		t.Parallel()
		msgSvc := &mockMessageService{
			createFunc: func(_ context.Context, _, _ string, _ domain.Role, _ string) (domain.Message, error) {
				return domain.Message{}, domain.ErrConversationNotFound
			},
			listPageFunc: func(_ context.Context, _, _ string, _ domain.Page) ([]domain.Message, bool, error) {
				return nil, false, domain.ErrConversationNotFound
			},
		}
		r := newTestRouter(&mockConversationService{}, msgSvc, 1024)

		reqBadID := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/bad-uuid/messages", strings.NewReader(`{"content":"hi"}`))
		recBadID := httptest.NewRecorder()
		r.ServeHTTP(recBadID, reqBadID)
		if recBadID.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", recBadID.Code)
		}

		reqMissing := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages", strings.NewReader(`{"content":"hi"}`))
		recMissing := httptest.NewRecorder()
		r.ServeHTTP(recMissing, reqMissing)
		if recMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", recMissing.Code)
		}

		reqListMissing := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+testConvID+"/messages", nil)
		recListMissing := httptest.NewRecorder()
		r.ServeHTTP(recListMissing, reqListMissing)
		if recListMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", recListMissing.Code)
		}
	})

	t.Run("list messages empty history returns empty array", func(t *testing.T) {
		t.Parallel()
		msgSvc := &mockMessageService{
			listPageFunc: func(_ context.Context, _, _ string, _ domain.Page) ([]domain.Message, bool, error) {
				return nil, false, nil
			},
		}
		r := newTestRouter(&mockConversationService{}, msgSvc, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+testConvID+"/messages", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"data":[]`) {
			t.Errorf("expected empty JSON array [], got %s", rec.Body.String())
		}
	})
}

func TestGeneralAPI_SafetyAndErrors(t *testing.T) {
	t.Parallel()

	t.Run("malformed JSON and trailing tokens", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

		for _, body := range []string{`{bad json`, `{"title":"a"}{"title":"b"}`} {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %q: expected 400, got %d", body, rec.Code)
			}
			if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInvalidJSON {
				t.Errorf("body %q: expected %q, got %q", body, dto.ErrCodeInvalidJSON, got)
			}
		}
	})

	t.Run("oversized payload returns 413", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 64)

		largeBody := `{"content":"` + strings.Repeat("x", 128) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages", bytes.NewBufferString(largeBody))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodePayloadTooLarge {
			t.Errorf("expected %q, got %q", dto.ErrCodePayloadTooLarge, got)
		}
	})

	t.Run("internal error masks details", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			getFunc: func(_ context.Context, _, _ string) (domain.Conversation, error) {
				return domain.Conversation{}, errors.New("pq: fatal database crash on host secret.supabase.co")
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+testConvID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret.supabase.co") {
			t.Fatalf("response leaked internal error details: %s", rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != dto.ErrCodeInternalError {
			t.Errorf("expected %q, got %q", dto.ErrCodeInternalError, got)
		}
	})

	t.Run("context cancellation returns 503", func(t *testing.T) {
		t.Parallel()
		convSvc := &mockConversationService{
			getFunc: func(ctx context.Context, _, _ string) (domain.Conversation, error) {
				return domain.Conversation{}, ctx.Err()
			},
		}
		r := newTestRouter(convSvc, &mockMessageService{}, 1024)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/conversations/"+testConvID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
	})

	t.Run("unsupported method returns 405 and unknown route returns 404", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

		reqMethod := httptest.NewRequest(http.MethodPut, "/api/v1/conversations/"+testConvID, nil)
		recMethod := httptest.NewRecorder()
		r.ServeHTTP(recMethod, reqMethod)
		if recMethod.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", recMethod.Code)
		}
		if got := decodeError(t, recMethod).Error.Code; got != dto.ErrCodeMethodNotAllowed {
			t.Errorf("expected %q, got %q", dto.ErrCodeMethodNotAllowed, got)
		}

		reqRoute := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
		recRoute := httptest.NewRecorder()
		r.ServeHTTP(recRoute, reqRoute)
		if recRoute.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", recRoute.Code)
		}
		if got := decodeError(t, recRoute).Error.Code; got != dto.ErrCodeRouteNotFound {
			t.Errorf("expected %q, got %q", dto.ErrCodeRouteNotFound, got)
		}
	})

	t.Run("health and ready routes remain intact", func(t *testing.T) {
		t.Parallel()
		r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

		for _, path := range []string{"/health", "/ready"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", path, rec.Code)
			}
		}
	})
}
