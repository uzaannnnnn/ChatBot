package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS returns a Gin middleware that enforces an explicit origin allowlist.
// Wildcard origins ("*") are never emitted when credentials are supported.
func CORS(allowedOrigin string) gin.HandlerFunc {
	normalizedAllowed := strings.TrimSpace(allowedOrigin)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Writer.Header().Add("Vary", "Origin")
		}

		if origin != "" && normalizedAllowed != "" && normalizedAllowed != "*" && origin == normalizedAllowed {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", normalizedAllowed)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Request-ID")
			h.Set("Access-Control-Expose-Headers", "X-Request-ID")
			h.Set("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			if origin != "" && origin != normalizedAllowed {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// MaxBodySize limits the size of incoming request bodies to protect the server from memory exhaustion.
func MaxBodySize(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
