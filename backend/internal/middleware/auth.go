package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/auth"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
)

const contextUserIDKey = "auth_user_id"

// Auth validates the Supabase JWT Bearer token using verifier and sets the authenticated user ID.
func Auth(verifier auth.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, dto.ErrorResponse{
				Error: dto.ErrorDetail{
					Code:    dto.ErrCodeInternalError,
					Message: "Authentication service is not configured",
				},
			})
			return
		}

		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if header == "" || len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse{
				Error: dto.ErrorDetail{
					Code:    dto.ErrCodeUnauthorized,
					Message: "Authentication required",
				},
			})
			return
		}

		token := strings.TrimSpace(header[7:])
		userID, err := verifier.VerifyToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse{
				Error: dto.ErrorDetail{
					Code:    dto.ErrCodeUnauthorized,
					Message: "Invalid or expired authentication token",
				},
			})
			return
		}

		c.Set(contextUserIDKey, userID)
		c.Next()
	}
}

// UserID returns the authenticated user's UUID from the Gin context.
func UserID(c *gin.Context) (string, bool) {
	val, ok := c.Get(contextUserIDKey)
	if !ok {
		return "", false
	}
	id, ok := val.(string)
	return id, ok && id != ""
}

// SetUserIDForTest injects a user ID into Gin context for unit tests.
func SetUserIDForTest(c *gin.Context, userID string) {
	c.Set(contextUserIDKey, userID)
}
