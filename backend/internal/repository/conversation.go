// Package repository defines persistence contracts for the chatbot domain.
// Implementations live in subpackages (for example repository/postgres) and
// must translate storage errors into domain errors.
package repository

import (
	"context"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

// ConversationRepository persists conversations scoped by ownerID.
type ConversationRepository interface {
	// CreateConversation inserts a conversation owned by ownerID with the given title.
	CreateConversation(ctx context.Context, ownerID, title string) (domain.Conversation, error)
	// GetConversationByID returns domain.ErrConversationNotFound when absent or owned by another user.
	GetConversationByID(ctx context.Context, ownerID, id string) (domain.Conversation, error)
	// ListConversations returns conversations owned by ownerID ordered by updated_at DESC, id DESC.
	ListConversations(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, error)
	// UpdateConversationTitle sets the title and refreshes updated_at when owned by ownerID.
	UpdateConversationTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error)
	// DeleteConversation removes the conversation owned by ownerID and, via ON DELETE CASCADE, its messages.
	DeleteConversation(ctx context.Context, ownerID, id string) error
}
