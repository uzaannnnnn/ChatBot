// Package migration wraps golang-migrate with the embedded SQL migrations and
// the chatbot-specific configuration (dedicated schema, tracking table, and
// credential-safe errors). It is used by cmd/migrate and integration tests,
// never by normal API startup.
package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uzaannnnnn/ChatBot/backend/internal/database"
	"github.com/uzaannnnnn/ChatBot/backend/migrations"
)

const (
	// SchemaName is the dedicated application schema.
	SchemaName = "chatbot"
	// MigrationsTable stores golang-migrate state inside the chatbot schema so
	// nothing is created in the Supabase Data API-exposed public schema.
	MigrationsTable = `"chatbot"."schema_migrations"`

	defaultConnectTimeout = 10 * time.Second
	lockTimeout           = 30 * time.Second
)

// Runner executes schema migrations against a single database.
type Runner struct {
	m      *migrate.Migrate
	rawURL string
}

// Status reports the current migration version.
type Status struct {
	Version uint
	Dirty   bool
	// Applied is false when no migration has ever been applied.
	Applied bool
}

// New connects to the database and prepares the migration runner.
// It ensures the chatbot schema exists so the tracking table can live in it.
func New(ctx context.Context, databaseURL string) (*Runner, error) {
	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("migration: invalid database configuration: %w", database.SanitizeError(err, databaseURL))
	}
	connCfg.ConnectTimeout = defaultConnectTimeout

	db := stdlib.OpenDB(*connCfg)
	db.SetMaxOpenConns(2)

	if err := bootstrapSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration: bootstrap schema: %w", database.SanitizeError(err, databaseURL))
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
		MigrationsTable:       MigrationsTable,
		MigrationsTableQuoted: true,
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration: init database driver: %w", database.SanitizeError(err, databaseURL))
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		_ = driver.Close()
		return nil, fmt.Errorf("migration: load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		_ = src.Close()
		_ = driver.Close()
		return nil, fmt.Errorf("migration: init migrate: %w", database.SanitizeError(err, databaseURL))
	}
	m.LockTimeout = lockTimeout

	return &Runner{m: m, rawURL: databaseURL}, nil
}

func bootstrapSchema(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()
	_, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+SchemaName)
	return err
}

// Up applies all pending migrations. It returns false when already up to date.
func (r *Runner) Up() (bool, error) {
	err := r.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}
	if err != nil {
		return false, r.wrap("up", err)
	}
	return true, nil
}

// Down rolls back exactly `steps` migrations. It is destructive and must only
// be invoked explicitly by an operator.
func (r *Runner) Down(steps int) error {
	if steps < 1 {
		return errors.New("migration: down requires at least 1 step")
	}
	if err := r.m.Steps(-steps); err != nil {
		return r.wrap("down", err)
	}
	return nil
}

// Force sets the migration version without running SQL (dirty-state recovery).
func (r *Runner) Force(version int) error {
	if err := r.m.Force(version); err != nil {
		return r.wrap("force", err)
	}
	return nil
}

// Status returns the current migration version and dirty flag.
func (r *Runner) Status() (Status, error) {
	v, dirty, err := r.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, r.wrap("version", err)
	}
	return Status{Version: v, Dirty: dirty, Applied: true}, nil
}

// GracefulStop requests the in-flight migration to stop after the current file.
func (r *Runner) GracefulStop() {
	select {
	case r.m.GracefulStop <- true:
	default:
	}
}

// Close releases the database connection and migration source.
func (r *Runner) Close() error {
	srcErr, dbErr := r.m.Close()
	return errors.Join(srcErr, dbErr)
}

func (r *Runner) wrap(op string, err error) error {
	return fmt.Errorf("migration: %s: %w", op, database.SanitizeError(err, r.rawURL))
}
