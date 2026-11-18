package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter enforces per-key (authenticated user ID or client IP) token-bucket rate limits.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
}

// NewRateLimiter creates a RateLimiter allowing r requests per second with burst b.
func NewRateLimiter(r rate.Limit, burst int) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     r,
		burst:    burst,
	}
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if v, ok := rl.visitors[key]; ok {
		v.lastSeen = now
		return v.limiter
	}

	// Opportunistic stale entry eviction + hard cap to guarantee bounded memory
	const maxVisitors = 10000
	if len(rl.visitors) > 1000 {
		cutoff := now.Add(-10 * time.Minute)
		for k, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, k)
			}
		}
	}
	if len(rl.visitors) >= maxVisitors {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range rl.visitors {
			if oldestKey == "" || v.lastSeen.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastSeen
			}
		}
		if oldestKey != "" {
			delete(rl.visitors, oldestKey)
		}
	}

	limiter := rate.NewLimiter(rl.rate, rl.burst)
	rl.visitors[key] = &visitor{
		limiter:  limiter,
		lastSeen: now,
	}
	return limiter
}

// Middleware returns a Gin handler that enforces the rate limit before downstream handlers commit SSE headers.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if uid, ok := UserID(c); ok {
			key = "user:" + uid
		}

		if !rl.getLimiter(key).Allow() {
			c.Header("Retry-After", "10")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, dto.ErrorResponse{
				Error: dto.ErrorDetail{
					Code:    dto.ErrCodeRateLimited,
					Message: "Too many requests. Please slow down.",
				},
			})
			return
		}

		c.Next()
	}
}
