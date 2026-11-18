package middleware

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// HeaderRequestID is the HTTP header used for request correlation.
	HeaderRequestID     = "X-Request-ID"
	contextRequestIDKey = "request_id"
)

// RequestID propagates an incoming X-Request-ID (if safe/bounded) or generates a UUID v4.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := strings.TrimSpace(c.GetHeader(HeaderRequestID))
		if reqID == "" || len(reqID) > 64 || strings.ContainsAny(reqID, "\r\n") {
			reqID = newUUIDv4()
		}
		c.Set(contextRequestIDKey, reqID)
		c.Writer.Header().Set(HeaderRequestID, reqID)
		c.Next()
	}
}

// GetRequestID retrieves the correlation ID from the Gin context.
func GetRequestID(c *gin.Context) string {
	if val, ok := c.Get(contextRequestIDKey); ok {
		if id, ok := val.(string); ok {
			return id
		}
	}
	return ""
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
