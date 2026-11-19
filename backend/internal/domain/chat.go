// Package domain defines the core chatbot entities, validation rules, and
// errors shared by the service and repository layers. It has no dependency on
// HTTP, SQL, or pgx.
package domain

import (
	"errors"
	"time"
)

const (
	// DefaultConversationTitle is used when a conversation is created without a title.
	DefaultConversationTitle = "New conversation"
	// MaxConversationTitleLength is the maximum title length in characters (runes).
	MaxConversationTitleLength = 200
	// MaxMessageContentLength is the maximum message content length in characters (runes).
	MaxMessageContentLength = 32000
)

// Domain errors. Repositories translate storage errors into these values so
// callers never depend on raw database errors.
var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrInvalidID            = errors.New("invalid identifier")
	ErrInvalidTitle         = errors.New("invalid conversation title")
	ErrInvalidRole          = errors.New("invalid message role")
	ErrInvalidContent       = errors.New("invalid message content")
	ErrInvalidPagination    = errors.New("invalid pagination parameters")
	ErrAIUnavailable        = errors.New("ai provider is not available or not configured")
	ErrAIProvider           = errors.New("ai provider failure")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrInternal             = errors.New("internal storage error")
)

// Role identifies the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Valid reports whether r is a supported message role.
func (r Role) Valid() bool {
	return r == RoleUser || r == RoleAssistant
}

// Conversation is a chat session containing ordered messages.
type Conversation struct {
	ID        string
	OwnerID   string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AttachmentKind identifies the category of a message attachment.
type AttachmentKind string

const (
	AttachmentKindImage AttachmentKind = "image"
)

// MessageAttachment represents metadata for an attachment linked to a message.
type MessageAttachment struct {
	ID          string
	MessageID   string
	Kind        AttachmentKind
	StoragePath string
	URL         string
	MIMEType    string
	Width       int
	Height      int
	Prompt      string
	AltText     string
	CreatedAt   time.Time
}

// NewMessageAttachment holds the fields required to persist an attachment.
type NewMessageAttachment struct {
	ID          string
	Kind        AttachmentKind
	StoragePath string
	MIMEType    string
	Width       int
	Height      int
	Prompt      string
	AltText     string
}

// Message is a single conversation entry authored by the user or the assistant.
type Message struct {
	ID             string
	ConversationID string
	Role           Role
	Content        string
	Attachments    []MessageAttachment
	CreatedAt      time.Time
}

// NewMessage holds the fields required to persist a message.
type NewMessage struct {
	ConversationID string
	Role           Role
	Content        string
}

// IsValidUUID reports whether s is a canonical 36-character hyphenated UUID.
func IsValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
