// Command migrate applies or inspects the chatbot database schema.
//
// Migrations are never run by the API server. Operators run this command
// explicitly:
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate version
//	go run ./cmd/migrate down -steps 1 -confirm     # DESTRUCTIVE
//	go run ./cmd/migrate force -version 1 -confirm  # dirty-state recovery
//
// Use -database-env to target a different variable, for example the test database:
//
//	go run ./cmd/migrate -database-env TEST_DATABASE_URL up
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/migration"
)

const usage = `Usage: migrate [global flags] <command> [command flags]

Commands:
  up                          Apply all pending migrations.
  version                     Print the current migration version.
  down -steps N -confirm      Roll back N migrations. DESTRUCTIVE: deletes data.
  force -version V -confirm   Set the version without running SQL (dirty recovery).

Global flags:
`

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger, os.Stderr); err != nil {
		logger.Error("migration command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger, stderr io.Writer) error {
	global := flag.NewFlagSet("migrate", flag.ContinueOnError)
	global.SetOutput(stderr)
	dbEnv := global.String("database-env", "DATABASE_URL", "environment variable holding the PostgreSQL connection string")
	envFile := global.String("env-file", ".env", "optional .env file to load (missing file is ignored)")
	global.Usage = func() {
		fmt.Fprint(stderr, usage)
		global.PrintDefaults()
	}
	if err := global.Parse(args); err != nil {
		return err
	}
	if global.NArg() < 1 {
		global.Usage()
		return errors.New("missing command")
	}
	command, cmdArgs := global.Arg(0), global.Args()[1:]

	cmdFlags := flag.NewFlagSet(command, flag.ContinueOnError)
	cmdFlags.SetOutput(stderr)
	steps := cmdFlags.Int("steps", 0, "number of migrations to roll back (down)")
	version := cmdFlags.Int("version", -1, "version to force (force)")
	confirm := cmdFlags.Bool("confirm", false, "confirm a destructive or state-altering operation")
	if err := cmdFlags.Parse(cmdArgs); err != nil {
		return err
	}

	switch command {
	case "up", "version":
	case "down":
		if *steps < 1 {
			return errors.New("down requires -steps N (N >= 1); rolling back all migrations at once is not supported")
		}
		if !*confirm {
			return errors.New("down is destructive and permanently deletes data; re-run with -confirm after taking a backup")
		}
	case "force":
		if *version < 0 {
			return errors.New("force requires -version V (V >= 0)")
		}
		if !*confirm {
			return errors.New("force alters migration state without running SQL; re-run with -confirm")
		}
	default:
		global.Usage()
		return fmt.Errorf("unknown command %q", command)
	}

	if *envFile != "" {
		_ = godotenv.Load(*envFile)
	}

	databaseURL := strings.TrimSpace(os.Getenv(*dbEnv))
	if databaseURL == "" {
		return fmt.Errorf("%s is not set", *dbEnv)
	}
	if err := config.ValidateDatabaseURL(databaseURL); err != nil {
		return fmt.Errorf("%s: %w", *dbEnv, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner, err := migration.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := runner.Close(); cerr != nil {
			logger.Warn("closing migration runner", "error", cerr)
		}
	}()

	go func() {
		<-ctx.Done()
		runner.GracefulStop()
	}()

	log := logger.With("database_env", *dbEnv, "command", command)

	switch command {
	case "up":
		applied, err := runner.Up()
		if err != nil {
			return err
		}
		if !applied {
			log.Info("no pending migrations; schema is up to date")
		} else {
			log.Info("migrations applied")
		}
	case "down":
		log.Warn("rolling back migrations", "steps", *steps)
		if err := runner.Down(*steps); err != nil {
			return err
		}
		log.Info("rollback complete", "steps", *steps)
	case "force":
		if err := runner.Force(*version); err != nil {
			return err
		}
		log.Info("migration version forced", "version", *version)
	}

	status, err := runner.Status()
	if err != nil {
		return err
	}
	log.Info("migration status", "version", status.Version, "dirty", status.Dirty, "applied", status.Applied)
	return nil
}
