// Package dto defines HTTP request and response data transfer objects.
package dto

import (
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

// Stable machine-readable error codes returned by the API.
const (
	ErrCodeInvalidRequest       = "invalid_request"
	ErrCodeInvalidJSON          = "invalid_json"
	ErrCodePayloadTooLarge      = "payload_too_large"
	ErrCodeInvalidID            = "invalid_id"
	ErrCodeInvalidTitle         = "invalid_title"
	ErrCodeInvalidContent       = "invalid_content"
	ErrCodeInvalidPagination    = "invalid_pagination"
	ErrCodeConversationNotFound = "conversation_not_found"
	ErrCodeRouteNotFound        = "route_not_found"
	ErrCodeMethodNotAllowed     = "method_not_allowed"
	ErrCodeAIUnavailable        = "ai_unavailable"
	ErrCodeAIProvider           = "ai_provider_error"
	ErrCodeUnauthorized         = "unauthorized"
	ErrCodeRateLimited          = "rate_limited"
	ErrCodeInternalError        = "internal_error"
)

// CreateConversationRequest is the payload for POST /api/v1/conversations.
type CreateConversationRequest struct {
	Title *string `json:"title"`
}

// UpdateConversationRequest is the payload for PATCH /api/v1/conversations/:id.
type UpdateConversationRequest struct {
	Title *string `json:"title" binding:"required"`
}

// CreateMessageRequest is the payload for POST /api/v1/conversations/:id/messages.
// Role is intentionally absent and assigned server-side as "user"; any client-supplied
// "role" field is rejected by DisallowUnknownFields().
type CreateMessageRequest struct {
	Content *string `json:"content" binding:"required"`
}

// ConversationResponse is the serialized representation of a conversation.
type ConversationResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MessageResponse is the serialized representation of a message.
type MessageResponse struct {
	ID             string      `json:"id"`
	ConversationID string      `json:"conversation_id"`
	Role           domain.Role `json:"role"`
	Content        string      `json:"content"`
	CreatedAt      time.Time   `json:"created_at"`
}

// PaginationMeta describes offset pagination metadata on list endpoints.
type PaginationMeta struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// SingleResponse wraps a single resource response in a "data" envelope.
type SingleResponse[T any] struct {
	Data T `json:"data"`
}

// ListResponse wraps a collection of resources and pagination metadata.
type ListResponse[T any] struct {
	Data       []T            `json:"data"`
	Pagination PaginationMeta `json:"pagination"`
}

// ErrorDetail describes a structured API error.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse wraps an ErrorDetail in an "error" envelope.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ToConversationResponse converts a domain.Conversation to a DTO with UTC timestamps.
func ToConversationResponse(c domain.Conversation) ConversationResponse {
	return ConversationResponse{
		ID:        c.ID,
		Title:     c.Title,
		CreatedAt: c.CreatedAt.UTC(),
		UpdatedAt: c.UpdatedAt.UTC(),
	}
}

// ToConversationResponses converts a slice of domain.Conversation to DTOs,
// always returning a non-nil slice so JSON marshaling produces "[]" rather than "null".
func ToConversationResponses(items []domain.Conversation) []ConversationResponse {
	out := make([]ConversationResponse, 0, len(items))
	for _, item := range items {
		out = append(out, ToConversationResponse(item))
	}
	return out
}

// ToMessageResponse converts a domain.Message to a DTO with a UTC timestamp.
func ToMessageResponse(m domain.Message) MessageResponse {
	return MessageResponse{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		Role:           m.Role,
		Content:        m.Content,
		CreatedAt:      m.CreatedAt.UTC(),
	}
}

// ToMessageResponses converts a slice of domain.Message to DTOs,
// always returning a non-nil slice so JSON marshaling produces "[]" rather than "null".
func ToMessageResponses(items []domain.Message) []MessageResponse {
	out := make([]MessageResponse, 0, len(items))
	for _, item := range items {
		out = append(out, ToMessageResponse(item))
	}
	return out
}
