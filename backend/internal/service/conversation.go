// Package service contains application business rules. Services validate and
// normalize input, then coordinate repositories. They are independent of HTTP
// and SQL so handlers and storage can evolve separately.
package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
)

// ConversationService manages conversation lifecycle rules.
type ConversationService struct {
	repo repository.ConversationRepository
}

// NewConversationService constructs a ConversationService.
func NewConversationService(repo repository.ConversationRepository) *ConversationService {
	return &ConversationService{repo: repo}
}

// Create creates a conversation owned by ownerID. A blank title falls back to domain.DefaultConversationTitle.
func (s *ConversationService) Create(ctx context.Context, ownerID, title string) (domain.Conversation, error) {
	if !domain.IsValidUUID(ownerID) {
		return domain.Conversation{}, domain.ErrUnauthorized
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = domain.DefaultConversationTitle
	}
	if err := validateTitle(title); err != nil {
		return domain.Conversation{}, err
	}
	return s.repo.CreateConversation(ctx, ownerID, title)
}

// Get returns a conversation by ID when owned by ownerID.
func (s *ConversationService) Get(ctx context.Context, ownerID, id string) (domain.Conversation, error) {
	if !domain.IsValidUUID(ownerID) {
		return domain.Conversation{}, domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(id) {
		return domain.Conversation{}, domain.ErrInvalidID
	}
	return s.repo.GetConversationByID(ctx, ownerID, id)
}

// List returns conversations owned by ownerID ordered by most recent activity.
func (s *ConversationService) List(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, error) {
	items, _, err := s.ListPage(ctx, ownerID, page)
	return items, err
}

// ListPage returns conversations owned by ownerID along with whether additional rows exist beyond the page.
func (s *ConversationService) ListPage(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, bool, error) {
	if !domain.IsValidUUID(ownerID) {
		return nil, false, domain.ErrUnauthorized
	}
	page = normalizePage(page)
	if err := page.Validate(); err != nil {
		return nil, false, err
	}
	queryPage := domain.Page{
		Limit:  page.Limit + 1,
		Offset: page.Offset,
	}
	items, err := s.repo.ListConversations(ctx, ownerID, queryPage)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(items) > page.Limit
	if hasMore {
		items = items[:page.Limit]
	}
	return items, hasMore, nil
}

// UpdateTitle renames a conversation owned by ownerID. Blank titles are rejected.
func (s *ConversationService) UpdateTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error) {
	if !domain.IsValidUUID(ownerID) {
		return domain.Conversation{}, domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(id) {
		return domain.Conversation{}, domain.ErrInvalidID
	}
	title = strings.TrimSpace(title)
	if err := validateTitle(title); err != nil {
		return domain.Conversation{}, err
	}
	return s.repo.UpdateConversationTitle(ctx, ownerID, id, title)
}

// Delete removes a conversation owned by ownerID and all of its messages.
func (s *ConversationService) Delete(ctx context.Context, ownerID, id string) error {
	if !domain.IsValidUUID(ownerID) {
		return domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(id) {
		return domain.ErrInvalidID
	}
	return s.repo.DeleteConversation(ctx, ownerID, id)
}

func validateTitle(title string) error {
	if title == "" || utf8.RuneCountInString(title) > domain.MaxConversationTitleLength {
		return domain.ErrInvalidTitle
	}
	if !utf8.ValidString(title) {
		return domain.ErrInvalidTitle
	}
	return nil
}

func normalizePage(p domain.Page) domain.Page {
	if p.Limit == 0 {
		p.Limit = domain.DefaultPageLimit
	}
	return p
}
