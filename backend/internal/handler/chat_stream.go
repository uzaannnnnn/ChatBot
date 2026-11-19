package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/service"
)

// ChatStreamService defines the conversation streaming business operations.
type ChatStreamService interface {
	StreamMessage(
		ctx context.Context,
		ownerID string,
		conversationID string,
		userContent string,
		onChunk func(delta string) error,
	) (domain.Message, domain.Message, error)
}

// ChatEventStreamService extends ChatStreamService with structured tool status
// and attachment SSE callbacks.
type ChatEventStreamService interface {
	StreamMessageWithEvents(
		ctx context.Context,
		ownerID string,
		conversationID string,
		userContent string,
		callbacks service.StreamCallbacks,
	) (domain.Message, domain.Message, error)
}

// ChatStreamHandler handles Server-Sent Events (SSE) AI generation streaming.
type ChatStreamHandler struct {
	chatSvc ChatStreamService
}

// NewChatStreamHandler constructs a ChatStreamHandler with the provided ChatStreamService.
func NewChatStreamHandler(chatSvc ChatStreamService) *ChatStreamHandler {
	return &ChatStreamHandler{chatSvc: chatSvc}
}

// Stream handles POST /api/v1/conversations/:id/messages/stream.
func (h *ChatStreamHandler) Stream(c *gin.Context) {
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

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondError(c, http.StatusInternalServerError, dto.ErrCodeInternalError, "Streaming is not supported by the server")
		return
	}

	headersCommitted := false
	ensureHeaders := func() {
		if !headersCommitted {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
			c.Writer.WriteHeader(http.StatusOK)
			headersCommitted = true

			c.SSEvent("message_start", gin.H{
				"conversation_id": conversationID,
				"role":            "assistant",
			})
			flusher.Flush()
		}
	}

	onDelta := func(delta string) error {
		ensureHeaders()
		c.SSEvent("content_delta", gin.H{
			"delta": delta,
		})
		flusher.Flush()
		return c.Request.Context().Err()
	}

	onToolStatus := func(evt service.ToolStatusEvent) error {
		ensureHeaders()
		payload := gin.H{
			"tool":   evt.Tool,
			"status": evt.Status,
		}
		if evt.Message != "" {
			payload["message"] = evt.Message
		}
		c.SSEvent("tool_status", payload)
		flusher.Flush()
		return c.Request.Context().Err()
	}

	onAttachment := func(att domain.MessageAttachment) error {
		ensureHeaders()
		c.SSEvent("attachment", dto.ToAttachmentResponse(att))
		flusher.Flush()
		return c.Request.Context().Err()
	}

	var (
		assistantMsg domain.Message
		err          error
	)
	if eventSvc, ok := h.chatSvc.(ChatEventStreamService); ok {
		_, assistantMsg, err = eventSvc.StreamMessageWithEvents(
			c.Request.Context(),
			ownerID,
			conversationID,
			*req.Content,
			service.StreamCallbacks{
				OnDelta:      onDelta,
				OnToolStatus: onToolStatus,
				OnAttachment: onAttachment,
			},
		)
	} else {
		_, assistantMsg, err = h.chatSvc.StreamMessage(
			c.Request.Context(),
			ownerID,
			conversationID,
			*req.Content,
			onDelta,
		)
	}

	if err != nil {
		if headersCommitted {
			// Headers already sent (200 OK): emit an SSE error event if client is still connected.
			if c.Request.Context().Err() == nil {
				c.SSEvent("error", gin.H{
					"code":    dto.ErrCodeAIProvider,
					"message": "AI generation interrupted or failed",
				})
				flusher.Flush()
			}
			return
		}

		// Headers not yet sent: respond with a standard JSON error envelope.
		respondDomainError(c, err)
		return
	}

	// Successful generation: ensure headers are present and send message_complete
	ensureHeaders()
	c.SSEvent("message_complete", gin.H{
		"status":     "completed",
		"message_id": assistantMsg.ID,
	})
	flusher.Flush()
}
