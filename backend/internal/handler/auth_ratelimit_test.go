package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/httpserver"
)

func TestAuthAndSecurityHeaders(t *testing.T) {
	t.Parallel()

	r := newTestRouter(&mockConversationService{}, &mockMessageService{}, 1024)

	t.Run("missing token returns 401 unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		req.Header.Set("X-Test-No-Auth", "1")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
		errResp := decodeError(t, rec)
		if errResp.Error.Code != dto.ErrCodeUnauthorized {
			t.Errorf("expected code %q, got %q", dto.ErrCodeUnauthorized, errResp.Error.Code)
		}
	})

	t.Run("invalid token returns 401 unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer expired-or-invalid-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("security headers and correlation ID are present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("X-Test-No-Auth", "1")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected /health 200 without auth, got %d", rec.Code)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("missing nosniff header")
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("missing X-Frame-Options DENY header")
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("missing X-Request-ID header")
		}
	})
}

func TestRateLimiterEnforcesLimitOnStreamRoute(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		AppEnv:              "test",
		FrontendOrigin:      "http://localhost:5173",
		MaxRequestBodyBytes: 1024,
	}
	engine := httpserver.NewRouter(httpserver.Dependencies{
		Config:            cfg,
		AuthVerifier:      &mockAuthVerifier{},
		DisableRateLimit:  false,
		HealthHandler:     handler.NewHealthHandler(&mockDBPinger{}, 100*time.Millisecond),
		ChatStreamHandler: handler.NewChatStreamHandler(&mockChatStreamService{}),
	})

	// Stream route burst is 5; 6th immediate request must return 429 Too Many Requests
	var lastStatus int
	for i := 0; i < 7; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+testConvID+"/messages/stream", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		lastStatus = rec.Code
		if lastStatus == http.StatusTooManyRequests {
			break
		}
	}

	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 rate_limited after burst, got %d", lastStatus)
	}
}
