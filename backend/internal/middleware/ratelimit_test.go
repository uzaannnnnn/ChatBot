package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func TestRateLimiter_PerUserIsolationAndConcurrency(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rl := NewRateLimiter(rate.Limit(1), 2)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			SetUserIDForTest(c, uid)
		}
		c.Next()
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// User A exhausts burst of 2 and gets 429 on 3rd
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Test-User", "user-a")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("user-a request %d expected 200, got %d", i+1, rec.Code)
		}
	}

	reqA3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqA3.Header.Set("X-Test-User", "user-a")
	recA3 := httptest.NewRecorder()
	r.ServeHTTP(recA3, reqA3)
	if recA3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for user-a, got %d", recA3.Code)
	}
	if recA3.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429")
	}

	// User B is unaffected
	reqB := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqB.Header.Set("X-Test-User", "user-b")
	recB := httptest.NewRecorder()
	r.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("expected 200 for user-b, got %d", recB.Code)
	}

	// Concurrent safety under -race
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("X-Test-User", fmt.Sprintf("concurrent-%d", idx%5))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()
}

func TestRateLimiter_BoundedMemoryAndStaleEviction(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(rate.Limit(10), 10)
	rl.mu.Lock()
	for i := 0; i < 1005; i++ {
		rl.visitors[fmt.Sprintf("stale-%d", i)] = &visitor{
			limiter:  rate.NewLimiter(10, 10),
			lastSeen: time.Now().Add(-30 * time.Minute),
		}
	}
	rl.mu.Unlock()

	rl.getLimiter("fresh-user")

	rl.mu.Lock()
	count := len(rl.visitors)
	rl.mu.Unlock()

	if count != 1 {
		t.Errorf("expected stale visitors to be evicted leaving 1 entry, got %d", count)
	}
}
