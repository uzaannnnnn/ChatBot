package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type fakeDBPinger struct {
	pingFunc func(ctx context.Context) error
}

func (f *fakeDBPinger) Ping(ctx context.Context) error {
	if f.pingFunc != nil {
		return f.pingFunc(ctx)
	}
	return nil
}

type readinessResponse struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies"`
}

func TestHealthHandler_Health(t *testing.T) {
	t.Parallel()

	// Even when database is nil/unavailable, /health must return 200 OK.
	h := NewHealthHandler(nil, 100*time.Millisecond)
	router := gin.New()
	router.GET("/health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status %q, got %q", "ok", body["status"])
	}
}

func TestHealthHandler_Ready_Success(t *testing.T) {
	t.Parallel()

	pinger := &fakeDBPinger{
		pingFunc: func(ctx context.Context) error {
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				t.Error("expected ping context to have a timeout deadline")
			}
			return nil
		},
	}

	h := NewHealthHandler(pinger, 200*time.Millisecond)
	router := gin.New()
	router.GET("/ready", h.Ready)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if body.Status != "ready" {
		t.Errorf("expected status %q, got %q", "ready", body.Status)
	}
	if body.Dependencies["database"] != "connected" {
		t.Errorf("expected database %q, got %q", "connected", body.Dependencies["database"])
	}
}

func TestHealthHandler_Ready_Failure(t *testing.T) {
	t.Parallel()

	pinger := &fakeDBPinger{
		pingFunc: func(ctx context.Context) error {
			return errors.New("connection refused")
		},
	}

	h := NewHealthHandler(pinger, 200*time.Millisecond)
	router := gin.New()
	router.GET("/ready", h.Ready)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if body.Status != "not_ready" {
		t.Errorf("expected status %q, got %q", "not_ready", body.Status)
	}
	if body.Dependencies["database"] != "unavailable" {
		t.Errorf("expected database %q, got %q", "unavailable", body.Dependencies["database"])
	}
}

func TestHealthHandler_Ready_ContextCancellation(t *testing.T) {
	t.Parallel()

	pingCalled := false
	pinger := &fakeDBPinger{
		pingFunc: func(ctx context.Context) error {
			pingCalled = true
			return ctx.Err()
		},
	}

	h := NewHealthHandler(pinger, 2*time.Second)
	router := gin.New()
	router.GET("/ready", h.Ready)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before request is handled.

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d for canceled context, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if pingCalled {
		t.Errorf("expected Ping not to be invoked when request context is already canceled")
	}

	var body readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if body.Status != "not_ready" {
		t.Errorf("expected status %q, got %q", "not_ready", body.Status)
	}
	if body.Dependencies["database"] != "unavailable" {
		t.Errorf("expected database %q, got %q", "unavailable", body.Dependencies["database"])
	}
}
