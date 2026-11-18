package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// These tests cover the safety guards only; they fail before any database connection.
func TestRunSafetyGuards(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("MIGRATE_TEST_UNSET_URL", "")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no command", []string{}, "missing command"},
		{"unknown command", []string{"drop"}, "unknown command"},
		{"down without steps", []string{"down", "-confirm"}, "-steps"},
		{"down without confirm", []string{"down", "-steps", "1"}, "-confirm"},
		{"force without version", []string{"force", "-confirm"}, "-version"},
		{"force without confirm", []string{"force", "-version", "1"}, "-confirm"},
		{"missing database url", []string{"-env-file", "", "-database-env", "MIGRATE_TEST_UNSET_URL", "up"}, "is not set"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args, logger, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRunRejectsInvalidURLWithoutLeakingIt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("MIGRATE_TEST_BAD_URL", "mysql://user:topsecret@localhost/db")

	err := run([]string{"-env-file", "", "-database-env", "MIGRATE_TEST_BAD_URL", "up"}, logger, io.Discard)
	if err == nil {
		t.Fatal("expected invalid URL error")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaked credentials: %v", err)
	}
}
