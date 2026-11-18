package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
)

// ConversationService defines the conversation business operations required by HTTP handlers.
type ConversationService interface {
	Create(ctx context.Context, ownerID, title string) (domain.Conversation, error)
	Get(ctx context.Context, ownerID, id string) (domain.Conversation, error)
	ListPage(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, bool, error)
	UpdateTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error)
	Delete(ctx context.Context, ownerID, id string) error
}

// ConversationHandler handles HTTP requests for conversation management.
type ConversationHandler struct {
	svc ConversationService
}

// NewConversationHandler constructs a ConversationHandler with the provided ConversationService.
func NewConversationHandler(svc ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// Create handles POST /api/v1/conversations.
func (h *ConversationHandler) Create(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req dto.CreateConversationRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}

	title := ""
	if req.Title != nil {
		title = *req.Title
	}

	conv, err := h.svc.Create(c.Request.Context(), ownerID, title)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.SingleResponse[dto.ConversationResponse]{
		Data: dto.ToConversationResponse(conv),
	})
}

// List handles GET /api/v1/conversations.
func (h *ConversationHandler) List(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	page, ok := parsePageQuery(c)
	if !ok {
		return
	}

	items, hasMore, err := h.svc.ListPage(c.Request.Context(), ownerID, page)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ListResponse[dto.ConversationResponse]{
		Data: dto.ToConversationResponses(items),
		Pagination: dto.PaginationMeta{
			Limit:   page.Limit,
			Offset:  page.Offset,
			HasMore: hasMore,
		},
	})
}

// Get handles GET /api/v1/conversations/:id.
func (h *ConversationHandler) Get(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if !domain.IsValidUUID(id) {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
		return
	}

	conv, err := h.svc.Get(c.Request.Context(), ownerID, id)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.SingleResponse[dto.ConversationResponse]{
		Data: dto.ToConversationResponse(conv),
	})
}

// UpdateTitle handles PATCH /api/v1/conversations/:id.
func (h *ConversationHandler) UpdateTitle(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if !domain.IsValidUUID(id) {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
		return
	}

	var req dto.UpdateConversationRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	if req.Title == nil {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidTitle, "Title is required")
		return
	}

	conv, err := h.svc.UpdateTitle(c.Request.Context(), ownerID, id, *req.Title)
	if err != nil {
		respondDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.SingleResponse[dto.ConversationResponse]{
		Data: dto.ToConversationResponse(conv),
	})
}

// Delete handles DELETE /api/v1/conversations/:id.
func (h *ConversationHandler) Delete(c *gin.Context) {
	ownerID, ok := requireUserID(c)
	if !ok {
		return
	}

	id := c.Param("id")
	if !domain.IsValidUUID(id) {
		respondError(c, http.StatusBadRequest, dto.ErrCodeInvalidID, "Invalid conversation identifier")
		return
	}

	if err := h.svc.Delete(c.Request.Context(), ownerID, id); err != nil {
		respondDomainError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
