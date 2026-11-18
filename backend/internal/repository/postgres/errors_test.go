package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

func TestMapError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ent  entity
		err  error
		want error
	}{
		{"nil", entityConversation, nil, nil},
		{"fk violation", entityMessage, &pgconn.PgError{Code: codeForeignKeyViolation}, domain.ErrConversationNotFound},
		{"invalid uuid text", entityConversation, &pgconn.PgError{Code: codeInvalidTextRepresent}, domain.ErrInvalidID},
		{"role check", entityMessage, &pgconn.PgError{Code: codeCheckViolation, ConstraintName: constraintRoleCheck}, domain.ErrInvalidRole},
		{"content check", entityMessage, &pgconn.PgError{Code: codeCheckViolation, ConstraintName: constraintContentNotBlank}, domain.ErrInvalidContent},
		{"title check", entityConversation, &pgconn.PgError{Code: codeCheckViolation, ConstraintName: constraintTitleNotBlank}, domain.ErrInvalidTitle},
		{"title too long", entityConversation, &pgconn.PgError{Code: codeStringDataTruncation}, domain.ErrInvalidTitle},
		{"role too long", entityMessage, &pgconn.PgError{Code: codeStringDataTruncation}, domain.ErrInvalidRole},
		{"wrapped fk violation", entityMessage, fmt.Errorf("wrap: %w", &pgconn.PgError{Code: codeForeignKeyViolation}), domain.ErrConversationNotFound},
		{"context canceled", entityConversation, context.Canceled, context.Canceled},
		{"deadline exceeded", entityConversation, context.DeadlineExceeded, context.DeadlineExceeded},
		{"unknown pg error", entityConversation, &pgconn.PgError{Code: "XX000", Message: "boom"}, domain.ErrInternal},
		{"unknown error", entityConversation, errors.New("network down"), domain.ErrInternal},
	}

	for _, tc := range tests {
		got := mapError("op", tc.ent, tc.err)
		if tc.want == nil {
			if got != nil {
				t.Errorf("%s: expected nil, got %v", tc.name, got)
			}
			continue
		}
		if !errors.Is(got, tc.want) {
			t.Errorf("%s: expected errors.Is(%v), got %v", tc.name, tc.want, got)
		}
	}
}

func TestStorageErrorHidesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("failed to connect to `user=postgres.secretref database=postgres`")
	err := mapError("get conversation", entityConversation, cause)

	if strings.Contains(err.Error(), "secretref") {
		t.Fatalf("storage error leaked raw cause: %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected cause to remain reachable via errors.Is for diagnostics")
	}
}
