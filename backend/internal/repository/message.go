package repository

import (
	"context"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

// MessageRepository persists conversation messages scoped by conversation ownership.
type MessageRepository interface {
	// CreateMessage inserts a message and refreshes the parent conversation's
	// updated_at atomically. Returns domain.ErrConversationNotFound when the
	// conversation does not exist or is not owned by ownerID.
	CreateMessage(ctx context.Context, ownerID string, msg domain.NewMessage) (domain.Message, error)
	// ListMessagesByConversationID returns messages ordered by created_at ASC, id ASC.
	// Returns domain.ErrConversationNotFound when the conversation does not exist or is not owned by ownerID.
	ListMessagesByConversationID(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, error)
	// ListRecentMessages returns the most recent limit messages for a conversation owned by ownerID,
	// ordered chronologically (created_at ASC, id ASC).
	// Returns domain.ErrConversationNotFound when the conversation does not exist or is not owned by ownerID.
	ListRecentMessages(ctx context.Context, ownerID, conversationID string, limit int) ([]domain.Message, error)
}
