package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const defaultPingTimeout = 2 * time.Second

// DBPinger abstracts database readiness verification so the handler can be tested without a live database.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler handles liveness and readiness HTTP probes.
type HealthHandler struct {
	db          DBPinger
	pingTimeout time.Duration
}

// NewHealthHandler constructs a HealthHandler with the provided database pinger and timeout.
func NewHealthHandler(db DBPinger, pingTimeout time.Duration) *HealthHandler {
	if pingTimeout <= 0 {
		pingTimeout = defaultPingTimeout
	}
	return &HealthHandler{
		db:          db,
		pingTimeout: pingTimeout,
	}
}

// Health responds with HTTP 200 if the HTTP server process is alive.
// It intentionally does not check PostgreSQL availability.
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// Ready verifies that downstream dependencies (PostgreSQL) are reachable within a short timeout.
func (h *HealthHandler) Ready(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"dependencies": gin.H{
				"database": "unavailable",
			},
		})
		return
	}

	reqCtx := c.Request.Context()
	if err := reqCtx.Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"dependencies": gin.H{
				"database": "unavailable",
			},
		})
		return
	}

	pingCtx, cancel := context.WithTimeout(reqCtx, h.pingTimeout)
	defer cancel()

	if err := h.db.Ping(pingCtx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"dependencies": gin.H{
				"database": "unavailable",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
		"dependencies": gin.H{
			"database": "connected",
		},
	})
}
