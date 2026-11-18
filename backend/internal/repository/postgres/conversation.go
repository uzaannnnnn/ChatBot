package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
)

const conversationColumns = `id, owner_id, title, created_at, updated_at`

// ConversationRepository is the PostgreSQL implementation of repository.ConversationRepository.
type ConversationRepository struct {
	db DB
}

var _ repository.ConversationRepository = (*ConversationRepository)(nil)

// NewConversationRepository returns a repository backed by the shared pool.
func NewConversationRepository(db DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// CreateConversation inserts a new conversation owned by ownerID.
func (r *ConversationRepository) CreateConversation(ctx context.Context, ownerID, title string) (domain.Conversation, error) {
	const q = `INSERT INTO chatbot.conversations (owner_id, title) VALUES ($1, $2) RETURNING ` + conversationColumns

	c, err := scanConversation(r.db.QueryRow(ctx, q, ownerID, title))
	if err != nil {
		return domain.Conversation{}, mapError("create conversation", entityConversation, err)
	}
	return c, nil
}

// GetConversationByID returns a single conversation owned by ownerID.
func (r *ConversationRepository) GetConversationByID(ctx context.Context, ownerID, id string) (domain.Conversation, error) {
	const q = `SELECT ` + conversationColumns + ` FROM chatbot.conversations WHERE id = $1 AND owner_id = $2`

	c, err := scanConversation(r.db.QueryRow(ctx, q, id, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return domain.Conversation{}, mapError("get conversation", entityConversation, err)
	}
	return c, nil
}

// ListConversations returns a page of conversations owned by ownerID ordered by most recent activity.
func (r *ConversationRepository) ListConversations(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, error) {
	if err := page.ValidateQuery(); err != nil {
		return nil, err
	}

	const q = `SELECT ` + conversationColumns + `
		FROM chatbot.conversations
		WHERE owner_id = $1
		ORDER BY updated_at DESC, id DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(ctx, q, ownerID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError("list conversations", entityConversation, err)
	}

	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Conversation, error) {
		return scanConversation(row)
	})
	if err != nil {
		return nil, mapError("list conversations", entityConversation, err)
	}
	return list, nil
}

// UpdateConversationTitle sets a new title and monotonically advances updated_at when owned by ownerID.
func (r *ConversationRepository) UpdateConversationTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error) {
	const q = `UPDATE chatbot.conversations
		SET title = $3, updated_at = GREATEST(updated_at, clock_timestamp())
		WHERE id = $1 AND owner_id = $2
		RETURNING ` + conversationColumns

	c, err := scanConversation(r.db.QueryRow(ctx, q, id, ownerID, title))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return domain.Conversation{}, mapError("update conversation title", entityConversation, err)
	}
	return c, nil
}

// DeleteConversation deletes a conversation owned by ownerID; messages are removed by ON DELETE CASCADE.
func (r *ConversationRepository) DeleteConversation(ctx context.Context, ownerID, id string) error {
	const q = `DELETE FROM chatbot.conversations WHERE id = $1 AND owner_id = $2`

	tag, err := r.db.Exec(ctx, q, id, ownerID)
	if err != nil {
		return mapError("delete conversation", entityConversation, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

func scanConversation(row pgx.Row) (domain.Conversation, error) {
	var c domain.Conversation
	err := row.Scan(&c.ID, &c.OwnerID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
