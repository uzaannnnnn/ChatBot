package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/middleware"
)

func requireUserID(c *gin.Context) (string, bool) {
	uid, ok := middleware.UserID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, dto.ErrCodeUnauthorized, "Authentication required")
		return "", false
	}
	return uid, true
}

// respondError writes a consistent JSON error envelope.
func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, dto.ErrorResponse{
		Error: dto.ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

// respondDomainError translates domain and context errors into stable HTTP responses
// without leaking storage or internal details.
func respondDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrConversationNotFound):
		respondError(c, http.StatusNotFound, dto.ErrCodeConversationNotFound, "Conversation not found")
	case errors.Is(err, domain.ErrInvalidID):
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
	case errors.Is(err, domain.ErrInvalidTitle):
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidTitle, "Title must be between 1 and 200 characters and not blank")
	case errors.Is(err, domain.ErrInvalidContent):
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidContent, "Message content must not be empty or exceed maximum length")
	case errors.Is(err, domain.ErrInvalidRole):
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidRequest, "Invalid message role")
	case errors.Is(err, domain.ErrInvalidPagination):
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidPagination, "Invalid pagination parameters")
	case errors.Is(err, domain.ErrAIUnavailable):
		respondError(c, http.StatusServiceUnavailable, dto.ErrCodeAIUnavailable, "AI service is unavailable or not configured")
	case errors.Is(err, domain.ErrAIProvider):
		respondError(c, http.StatusBadGateway, dto.ErrCodeAIProvider, "AI provider request failed")
	case errors.Is(err, domain.ErrUnauthorized):
		respondError(c, http.StatusUnauthorized, dto.ErrCodeUnauthorized, "Authentication required")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		respondError(c, http.StatusServiceUnavailable, dto.ErrCodeInternalError, "Request was canceled or timed out")
	default:
		respondError(c, http.StatusInternalServerError, dto.ErrCodeInternalError, "An internal error occurred")
	}
}

// decodeStrictJSON decodes a JSON request body strictly:
//   - Disallows unknown fields (e.g., client-supplied "role").
//   - Rejects trailing JSON tokens after the root object.
//   - Translates http.MaxBytesError to HTTP 413 Payload Too Large.
//   - Optionally allows an empty body when allowEmpty is true.
func decodeStrictJSON(c *gin.Context, dst any, allowEmpty bool) bool {
	if c.Request.Body == nil {
		if allowEmpty {
			return true
		}
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidJSON, "Request body is required")
		return false
	}

	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondError(c, http.StatusRequestEntityTooLarge, dto.ErrCodePayloadTooLarge, "Request body exceeds maximum allowed size")
			return false
		}
		if errors.Is(err, io.EOF) {
			if allowEmpty {
				return true
			}
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidJSON, "Request body is required")
			return false
		}
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidRequest, "Request contains unknown or disallowed field")
			return false
		}
		var valErrs validator.ValidationErrors
		if errors.As(err, &valErrs) {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidRequest, "Invalid request fields")
			return false
		}
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidJSON, "Malformed JSON request body")
		return false
	}

	// Ensure no trailing tokens follow the JSON value.
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondError(c, http.StatusRequestEntityTooLarge, dto.ErrCodePayloadTooLarge, "Request body exceeds maximum allowed size")
			return false
		}
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidJSON, "Request body must contain a single JSON object")
		return false
	}

	return true
}

// parsePageQuery extracts and validates "limit" and "offset" query parameters.
func parsePageQuery(c *gin.Context) (domain.Page, bool) {
	page := domain.Page{
		Limit:  domain.DefaultPageLimit,
		Offset: 0,
	}

	if rawLimit, ok := c.GetQuery("limit"); ok {
		trimmed := strings.TrimSpace(rawLimit)
		if trimmed == "" {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidPagination, "Invalid limit parameter")
			return domain.Page{}, false
		}
		limit, err := strconv.Atoi(trimmed)
		if err != nil {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidPagination, "Invalid limit parameter")
			return domain.Page{}, false
		}
		page.Limit = limit
	}

	if rawOffset, ok := c.GetQuery("offset"); ok {
		trimmed := strings.TrimSpace(rawOffset)
		if trimmed == "" {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidPagination, "Invalid offset parameter")
			return domain.Page{}, false
		}
		offset, err := strconv.Atoi(trimmed)
		if err != nil {
			respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidPagination, "Invalid offset parameter")
			return domain.Page{}, false
		}
		page.Offset = offset
	}

	if err := page.Validate(); err != nil {
		respondDomainError(c, err)
		return domain.Page{}, false
	}

	return page, true
}
