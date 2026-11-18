package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
)

const messageColumns = `m.id, m.conversation_id, m.role, m.content, m.created_at`
const rawMessageColumns = `id, conversation_id, role, content, created_at`

// MessageRepository is the PostgreSQL implementation of repository.MessageRepository.
type MessageRepository struct {
	db DB
}

var _ repository.MessageRepository = (*MessageRepository)(nil)

// NewMessageRepository returns a repository backed by the shared pool.
func NewMessageRepository(db DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// CreateMessage inserts the message and monotonically advances conversations.updated_at in one transaction,
// enforcing that the parent conversation is owned by ownerID.
func (r *MessageRepository) CreateMessage(ctx context.Context, ownerID string, msg domain.NewMessage) (domain.Message, error) {
	const touchConversation = `UPDATE chatbot.conversations
		SET updated_at = GREATEST(updated_at, clock_timestamp())
		WHERE id = $1 AND owner_id = $2`
	const insertMessage = `INSERT INTO chatbot.messages (conversation_id, role, content)
		VALUES ($1, $2, $3)
		RETURNING ` + rawMessageColumns

	var created domain.Message
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, touchConversation, msg.ConversationID, ownerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConversationNotFound
		}

		created, err = scanMessage(tx.QueryRow(ctx, insertMessage, msg.ConversationID, string(msg.Role), msg.Content))
		return err
	})
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			return domain.Message{}, err
		}
		return domain.Message{}, mapError("create message", entityMessage, err)
	}
	return created, nil
}

// ListMessagesByConversationID returns a page of messages in chronological order when owned by ownerID.
func (r *MessageRepository) ListMessagesByConversationID(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, error) {
	if err := page.ValidateQuery(); err != nil {
		return nil, err
	}

	const q = `SELECT ` + messageColumns + `
		FROM chatbot.messages m
		INNER JOIN chatbot.conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND c.owner_id = $2
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, q, conversationID, ownerID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapError("list messages", entityMessage, err)
	}

	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Message, error) {
		return scanMessage(row)
	})
	if err != nil {
		return nil, mapError("list messages", entityMessage, err)
	}

	if len(list) == 0 {
		const existsQ = `SELECT EXISTS (SELECT 1 FROM chatbot.conversations WHERE id = $1 AND owner_id = $2)`
		var exists bool
		if err := r.db.QueryRow(ctx, existsQ, conversationID, ownerID).Scan(&exists); err != nil {
			return nil, mapError("list messages", entityMessage, err)
		}
		if !exists {
			return nil, domain.ErrConversationNotFound
		}
	}
	return list, nil
}

// ListRecentMessages returns the most recent limit messages for a conversation owned by ownerID,
// ordered chronologically (created_at ASC, id ASC).
func (r *MessageRepository) ListRecentMessages(ctx context.Context, ownerID, conversationID string, limit int) ([]domain.Message, error) {
	if limit < 1 {
		limit = domain.DefaultPageLimit
	}
	if limit > domain.MaxPageLimit {
		limit = domain.MaxPageLimit
	}

	const q = `SELECT ` + rawMessageColumns + `
		FROM (
			SELECT ` + messageColumns + `
			FROM chatbot.messages m
			INNER JOIN chatbot.conversations c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1 AND c.owner_id = $2
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $3
		) sub
		ORDER BY created_at ASC, id ASC`

	rows, err := r.db.Query(ctx, q, conversationID, ownerID, limit)
	if err != nil {
		return nil, mapError("list recent messages", entityMessage, err)
	}

	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Message, error) {
		return scanMessage(row)
	})
	if err != nil {
		return nil, mapError("list recent messages", entityMessage, err)
	}

	if len(list) == 0 {
		const existsQ = `SELECT EXISTS (SELECT 1 FROM chatbot.conversations WHERE id = $1 AND owner_id = $2)`
		var exists bool
		if err := r.db.QueryRow(ctx, existsQ, conversationID, ownerID).Scan(&exists); err != nil {
			return nil, mapError("list recent messages", entityMessage, err)
		}
		if !exists {
			return nil, domain.ErrConversationNotFound
		}
	}
	return list, nil
}

func scanMessage(row pgx.Row) (domain.Message, error) {
	var (
		m    domain.Message
		role string
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &role, &m.Content, &m.CreatedAt); err != nil {
		return domain.Message{}, err
	}
	m.Role = domain.Role(role)
	return m, nil
}
