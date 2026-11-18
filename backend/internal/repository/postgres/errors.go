// Package postgres implements the repository interfaces using pgx v5.
// All queries are parameterized and target the dedicated "chatbot" schema.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

// DB is the subset of *pgxpool.Pool used by the repositories. Accepting an
// interface lets callers inject the single application pool.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PostgreSQL SQLSTATE codes mapped to domain errors.
const (
	codeForeignKeyViolation   = "23503"
	codeCheckViolation        = "23514"
	codeStringDataTruncation  = "22001"
	codeInvalidTextRepresent  = "22P02"
	constraintRoleCheck       = "messages_role_chk"
	constraintContentNotBlank = "messages_content_not_blank_chk"
	constraintTitleNotBlank   = "conversations_title_not_blank_chk"
)

// entity selects the domain error used for value-too-long violations.
type entity int

const (
	entityConversation entity = iota
	entityMessage
)

// storageError hides raw database details from Error() while keeping the
// cause reachable through errors.Is / errors.As for diagnostics.
type storageError struct {
	op    string
	cause error
}

func (e *storageError) Error() string {
	return fmt.Sprintf("repository: %s: %s", e.op, domain.ErrInternal.Error())
}

func (e *storageError) Unwrap() []error {
	return []error{domain.ErrInternal, e.cause}
}

// mapError translates pgx/PostgreSQL errors into domain errors.
func mapError(op string, ent entity, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("repository: %s: %w", op, err)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case codeForeignKeyViolation:
			return domain.ErrConversationNotFound
		case codeInvalidTextRepresent:
			return domain.ErrInvalidID
		case codeCheckViolation:
			switch pgErr.ConstraintName {
			case constraintRoleCheck:
				return domain.ErrInvalidRole
			case constraintContentNotBlank:
				return domain.ErrInvalidContent
			case constraintTitleNotBlank:
				return domain.ErrInvalidTitle
			}
		case codeStringDataTruncation:
			if ent == entityMessage {
				return domain.ErrInvalidRole
			}
			return domain.ErrInvalidTitle
		}
	}

	return &storageError{op: op, cause: err}
}
