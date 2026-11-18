package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
)

// MessageService manages message rules.
type MessageService struct {
	repo repository.MessageRepository
}

// NewMessageService constructs a MessageService.
func NewMessageService(repo repository.MessageRepository) *MessageService {
	return &MessageService{repo: repo}
}

// Create validates and stores a message in a conversation owned by ownerID.
func (s *MessageService) Create(ctx context.Context, ownerID, conversationID string, role domain.Role, content string) (domain.Message, error) {
	if !domain.IsValidUUID(ownerID) {
		return domain.Message{}, domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(conversationID) {
		return domain.Message{}, domain.ErrInvalidID
	}
	if !role.Valid() {
		return domain.Message{}, domain.ErrInvalidRole
	}
	if err := validateContent(content); err != nil {
		return domain.Message{}, err
	}
	return s.repo.CreateMessage(ctx, ownerID, domain.NewMessage{
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
	})
}

// List returns messages of a conversation owned by ownerID in chronological order.
func (s *MessageService) List(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, error) {
	items, _, err := s.ListPage(ctx, ownerID, conversationID, page)
	return items, err
}

// ListPage returns messages of a conversation owned by ownerID along with pagination flag.
func (s *MessageService) ListPage(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, bool, error) {
	if !domain.IsValidUUID(ownerID) {
		return nil, false, domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(conversationID) {
		return nil, false, domain.ErrInvalidID
	}
	page = normalizePage(page)
	if err := page.Validate(); err != nil {
		return nil, false, err
	}
	queryPage := domain.Page{
		Limit:  page.Limit + 1,
		Offset: page.Offset,
	}
	items, err := s.repo.ListMessagesByConversationID(ctx, ownerID, conversationID, queryPage)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	return items, hasMore, nil
}

func validateContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return domain.ErrInvalidContent
	}
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) > domain.MaxMessageContentLength {
		return domain.ErrInvalidContent
	}
	return nil
}
