package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

const testOwnerID = "11111111-1111-4111-8111-111111111111"

const validID = "123e4567-e89b-12d3-a456-426614174000"

type fakeConversationRepo struct {
	calls     int
	lastTitle string
	lastPage  domain.Page
	err       error
}

func (f *fakeConversationRepo) CreateConversation(_ context.Context, _, title string) (domain.Conversation, error) {
	f.calls++
	f.lastTitle = title
	return domain.Conversation{ID: validID, Title: title}, f.err
}

func (f *fakeConversationRepo) GetConversationByID(_ context.Context, _, id string) (domain.Conversation, error) {
	f.calls++
	return domain.Conversation{ID: id}, f.err
}

func (f *fakeConversationRepo) ListConversations(_ context.Context, _ string, page domain.Page) ([]domain.Conversation, error) {
	f.calls++
	f.lastPage = page
	return nil, f.err
}

func (f *fakeConversationRepo) UpdateConversationTitle(_ context.Context, _, id, title string) (domain.Conversation, error) {
	f.calls++
	f.lastTitle = title
	return domain.Conversation{ID: id, Title: title}, f.err
}

func (f *fakeConversationRepo) DeleteConversation(_ context.Context, _, _ string) error {
	f.calls++
	return f.err
}

type fakeMessageRepo struct {
	calls    int
	last     domain.NewMessage
	lastPage domain.Page
	err      error
}

func (f *fakeMessageRepo) CreateMessage(_ context.Context, _ string, msg domain.NewMessage) (domain.Message, error) {
	f.calls++
	f.last = msg
	return domain.Message{ConversationID: msg.ConversationID, Role: msg.Role, Content: msg.Content}, f.err
}

func (f *fakeMessageRepo) ListMessagesByConversationID(_ context.Context, _, _ string, page domain.Page) ([]domain.Message, error) {
	f.calls++
	f.lastPage = page
	return nil, f.err
}

func (f *fakeMessageRepo) ListRecentMessages(_ context.Context, _, _ string, _ int) ([]domain.Message, error) {
	f.calls++
	return nil, f.err
}

func TestConversationService_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		title     string
		wantTitle string
		wantErr   error
	}{
		{"trims title", "  Trip planning  ", "Trip planning", nil},
		{"blank uses default", "   \t\n", domain.DefaultConversationTitle, nil},
		{"exactly max length", strings.Repeat("é", domain.MaxConversationTitleLength), strings.Repeat("é", domain.MaxConversationTitleLength), nil},
		{"too long", strings.Repeat("a", domain.MaxConversationTitleLength+1), "", domain.ErrInvalidTitle},
		{"invalid utf8", "bad\xff", "", domain.ErrInvalidTitle},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeConversationRepo{}
			svc := NewConversationService(repo)

			_, err := svc.Create(context.Background(), testOwnerID, tc.title)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				if repo.calls != 0 {
					t.Fatal("repository must not be called for invalid input")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastTitle != tc.wantTitle {
				t.Errorf("expected title %q, got %q", tc.wantTitle, repo.lastTitle)
			}
		})
	}
}

func TestConversationService_UpdateTitle(t *testing.T) {
	t.Parallel()

	repo := &fakeConversationRepo{}
	svc := NewConversationService(repo)

	if _, err := svc.UpdateTitle(context.Background(), testOwnerID, validID, "   "); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle for blank title, got %v", err)
	}
	if _, err := svc.UpdateTitle(context.Background(), testOwnerID, "nope", "Title"); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("expected ErrInvalidID, got %v", err)
	}
	if repo.calls != 0 {
		t.Fatal("repository must not be called for invalid input")
	}

	if _, err := svc.UpdateTitle(context.Background(), testOwnerID, validID, "  Renamed "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastTitle != "Renamed" {
		t.Errorf("expected trimmed title %q, got %q", "Renamed", repo.lastTitle)
	}
}

func TestConversationService_InvalidIDs(t *testing.T) {
	t.Parallel()

	repo := &fakeConversationRepo{}
	svc := NewConversationService(repo)

	if _, err := svc.Get(context.Background(), testOwnerID, "1 OR 1=1"); !errors.Is(err, domain.ErrInvalidID) {
		t.Errorf("Get: expected ErrInvalidID, got %v", err)
	}
	if err := svc.Delete(context.Background(), testOwnerID, ""); !errors.Is(err, domain.ErrInvalidID) {
		t.Errorf("Delete: expected ErrInvalidID, got %v", err)
	}
	if repo.calls != 0 {
		t.Fatal("repository must not be called for invalid IDs")
	}
}

func TestConversationService_ListPagination(t *testing.T) {
	t.Parallel()

	repo := &fakeConversationRepo{}
	svc := NewConversationService(repo)

	if _, err := svc.List(context.Background(), testOwnerID, domain.Page{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastPage.Limit != domain.DefaultPageLimit+1 {
		t.Errorf("expected repository probe limit %d, got %d", domain.DefaultPageLimit+1, repo.lastPage.Limit)
	}

	for _, p := range []domain.Page{{Limit: -1}, {Limit: domain.MaxPageLimit + 1}, {Limit: 10, Offset: -5}} {
		if _, err := svc.List(context.Background(), testOwnerID, p); !errors.Is(err, domain.ErrInvalidPagination) {
			t.Errorf("page %+v: expected ErrInvalidPagination, got %v", p, err)
		}
	}
	if repo.calls != 1 {
		t.Errorf("expected exactly one repository call, got %d", repo.calls)
	}
}

func TestConversationService_PropagatesRepositoryErrors(t *testing.T) {
	t.Parallel()

	repo := &fakeConversationRepo{err: domain.ErrConversationNotFound}
	svc := NewConversationService(repo)

	if _, err := svc.Get(context.Background(), testOwnerID, validID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
}

func TestMessageService_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		convID  string
		role    domain.Role
		content string
		wantErr error
	}{
		{"valid user", validID, domain.RoleUser, "Hello", nil},
		{"valid assistant multiline", validID, domain.RoleAssistant, "  Line 1\nLine 2  ", nil},
		{"invalid id", "abc", domain.RoleUser, "Hello", domain.ErrInvalidID},
		{"invalid role", validID, domain.Role("system"), "Hello", domain.ErrInvalidRole},
		{"empty content", validID, domain.RoleUser, "", domain.ErrInvalidContent},
		{"whitespace content", validID, domain.RoleUser, " \t\n ", domain.ErrInvalidContent},
		{"too long content", validID, domain.RoleUser, strings.Repeat("a", domain.MaxMessageContentLength+1), domain.ErrInvalidContent},
		{"invalid utf8", validID, domain.RoleUser, "hi\xff", domain.ErrInvalidContent},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeMessageRepo{}
			svc := NewMessageService(repo)

			_, err := svc.Create(context.Background(), testOwnerID, tc.convID, tc.role, tc.content)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				if repo.calls != 0 {
					t.Fatal("repository must not be called for invalid input")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.last.Content != tc.content {
				t.Errorf("expected content stored verbatim, got %q", repo.last.Content)
			}
		})
	}
}

func TestMessageService_List(t *testing.T) {
	t.Parallel()

	repo := &fakeMessageRepo{}
	svc := NewMessageService(repo)

	if _, err := svc.List(context.Background(), testOwnerID, "bad", domain.Page{}); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("expected ErrInvalidID, got %v", err)
	}
	if _, err := svc.List(context.Background(), testOwnerID, validID, domain.Page{Limit: 1000}); !errors.Is(err, domain.ErrInvalidPagination) {
		t.Fatalf("expected ErrInvalidPagination, got %v", err)
	}
	if _, err := svc.List(context.Background(), testOwnerID, validID, domain.Page{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastPage.Limit != domain.DefaultPageLimit+1 {
		t.Errorf("expected repository probe limit %d, got %d", domain.DefaultPageLimit+1, repo.lastPage.Limit)
	}
}
