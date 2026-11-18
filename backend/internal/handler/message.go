package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
)

// MessageService defines the message business operations required by HTTP handlers.
type MessageService interface {
	Create(ctx context.Context, ownerID, conversationID string, role domain.Role, content string) (domain.Message, error)
	ListPage(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, bool, error)
}

// MessageHandler handles HTTP requests for conversation messages.
type MessageHandler struct {
	svc MessageService
}

// NewMessageHandler constructs a MessageHandler with the provided MessageService.
func NewMessageHandler(svc MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// List handles GET /api/v1/conversations/:id/messages.
func (h *MessageHandler) List(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	conversationID := c.Param("id")
	if !domain.IsValidUUID(conversationID) {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
		return
	}

	page, ok := parsePageQuery(c)
	if !ok {
		return
	}

	items, hasMore, err := h.svc.ListPage(c.Request.Context(), ownerID, conversationID, page)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ListResponse[dto.MessageResponse]{
		Data: dto.ToMessageResponses(items),
		Pagination: dto.PaginationMeta{
			Limit:   page.Limit,
			Offset:  page.Offset,
			HasMore: hasMore,
		},
	})
}

// Create handles POST /api/v1/conversations/:id/messages.
// Role is strictly assigned server-side as domain.RoleUser; clients cannot supply a role.
func (h *MessageHandler) Create(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	conversationID := c.Param("id")
	if !domain.IsValidUUID(conversationID) {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
		return
	}

	var req dto.CreateMessageRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	if req.Content == nil {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidContent, "Message content is required")
		return
	}

	msg, err := h.svc.Create(c.Request.Context(), ownerID, conversationID, domain.RoleUser, *req.Content)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.SingleResponse[dto.MessageResponse]{
		Data: dto.ToMessageResponse(msg),
	})
}
