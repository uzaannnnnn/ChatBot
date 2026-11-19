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
const attachmentColumns = `id, message_id, kind, storage_path, mime_type, width, height, prompt, alt_text, created_at`

// MessageRepository is the PostgreSQL implementation of repository.MessageRepository
// and repository.AttachmentRepository.
type MessageRepository struct {
	db DB
}

var (
	_ repository.MessageRepository    = (*MessageRepository)(nil)
	_ repository.AttachmentRepository = (*MessageRepository)(nil)
)

// NewMessageRepository returns a repository backed by the shared pool.
func NewMessageRepository(db DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// CreateMessage inserts the message and monotonically advances conversations.updated_at in one transaction,
// enforcing that the parent conversation is owned by ownerID.
func (r *MessageRepository) CreateMessage(ctx context.Context, ownerID string, msg domain.NewMessage) (domain.Message, error) {
	return r.CreateMessageWithAttachments(ctx, ownerID, msg, nil)
}

// CreateMessageWithAttachments inserts a message and any associated attachments atomically
// while advancing conversations.updated_at and enforcing conversation ownership.
func (r *MessageRepository) CreateMessageWithAttachments(
	ctx context.Context,
	ownerID string,
	msg domain.NewMessage,
	attachments []domain.NewMessageAttachment,
) (domain.Message, error) {
	const touchConversation = `UPDATE chatbot.conversations
		SET updated_at = GREATEST(updated_at, clock_timestamp())
		WHERE id = $1 AND owner_id = $2`
	const insertMessage = `INSERT INTO chatbot.messages (conversation_id, role, content)
		VALUES ($1, $2, $3)
		RETURNING ` + rawMessageColumns
	const insertAttachmentWithID = `INSERT INTO chatbot.message_attachments
		(id, message_id, kind, storage_path, mime_type, width, height, prompt, alt_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING ` + attachmentColumns
	const insertAttachmentAutoID = `INSERT INTO chatbot.message_attachments
		(message_id, kind, storage_path, mime_type, width, height, prompt, alt_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + attachmentColumns

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
		if err != nil {
			return err
		}

		if len(attachments) > 0 {
			created.Attachments = make([]domain.MessageAttachment, 0, len(attachments))
			for _, att := range attachments {
				var saved domain.MessageAttachment
				if domain.IsValidUUID(att.ID) {
					saved, err = scanAttachment(tx.QueryRow(
						ctx,
						insertAttachmentWithID,
						att.ID,
						created.ID,
						string(att.Kind),
						att.StoragePath,
						att.MIMEType,
						att.Width,
						att.Height,
						att.Prompt,
						att.AltText,
					))
				} else {
					saved, err = scanAttachment(tx.QueryRow(
						ctx,
						insertAttachmentAutoID,
						created.ID,
						string(att.Kind),
						att.StoragePath,
						att.MIMEType,
						att.Width,
						att.Height,
						att.Prompt,
						att.AltText,
					))
				}
				if err != nil {
					return err
				}
				created.Attachments = append(created.Attachments, saved)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			return domain.Message{}, err
		}
		return domain.Message{}, mapError("create message", entityMessage, err)
	}
	return created, nil
}

// ListMessagesByConversationID returns a page of messages in chronological order when owned by ownerID,
// hydrating any linked attachments.
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
		return list, nil
	}

	if err := r.hydrateAttachments(ctx, list); err != nil {
		return nil, mapError("list message attachments", entityMessage, err)
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

// ListAttachmentStoragePathsByConversationID returns all storage paths belonging to messages
// in a conversation owned by ownerID.
func (r *MessageRepository) ListAttachmentStoragePathsByConversationID(ctx context.Context, ownerID, conversationID string) ([]string, error) {
	const q = `SELECT a.storage_path
		FROM chatbot.message_attachments a
		INNER JOIN chatbot.messages m ON m.id = a.message_id
		INNER JOIN chatbot.conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND c.owner_id = $2`

	rows, err := r.db.Query(ctx, q, conversationID, ownerID)
	if err != nil {
		return nil, mapError("list conversation attachment paths", entityMessage, err)
	}
	paths, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
		var p string
		if err := row.Scan(&p); err != nil {
			return "", err
		}
		return p, nil
	})
	if err != nil {
		return nil, mapError("list conversation attachment paths", entityMessage, err)
	}
	return paths, nil
}

func (r *MessageRepository) hydrateAttachments(ctx context.Context, messages []domain.Message) error {
	ids := make([]string, len(messages))
	indexByID := make(map[string]int, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
		indexByID[m.ID] = i
	}

	const q = `SELECT ` + attachmentColumns + `
		FROM chatbot.message_attachments
		WHERE message_id = ANY($1::uuid[])
		ORDER BY created_at ASC, id ASC`

	rows, err := r.db.Query(ctx, q, ids)
	if err != nil {
		return err
	}

	attachments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MessageAttachment, error) {
		return scanAttachment(row)
	})
	if err != nil {
		return err
	}

	for _, att := range attachments {
		if idx, ok := indexByID[att.MessageID]; ok {
			messages[idx].Attachments = append(messages[idx].Attachments, att)
		}
	}
	return nil
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

func scanAttachment(row pgx.Row) (domain.MessageAttachment, error) {
	var (
		a    domain.MessageAttachment
		kind string
	)
	if err := row.Scan(
		&a.ID,
		&a.MessageID,
		&kind,
		&a.StoragePath,
		&a.MIMEType,
		&a.Width,
		&a.Height,
		&a.Prompt,
		&a.AltText,
		&a.CreatedAt,
	); err != nil {
		return domain.MessageAttachment{}, err
	}
	a.Kind = domain.AttachmentKind(kind)
	return a, nil
}
