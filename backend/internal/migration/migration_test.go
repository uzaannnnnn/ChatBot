package migration

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/uzaannnnnn/ChatBot/backend/migrations"
)

func TestEmbeddedMigrationsArePaired(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded migration files")
	}

	ups, downs := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		switch {
		case strings.HasSuffix(f, ".up.sql"):
			ups[strings.TrimSuffix(f, ".up.sql")] = true
		case strings.HasSuffix(f, ".down.sql"):
			downs[strings.TrimSuffix(f, ".down.sql")] = true
		default:
			t.Errorf("unexpected migration file name %q", f)
		}
	}
	for name := range ups {
		if !downs[name] {
			t.Errorf("migration %q has no down file", name)
		}
	}
	for name := range downs {
		if !ups[name] {
			t.Errorf("migration %q has no up file", name)
		}
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("golang-migrate could not parse embedded migrations: %v", err)
	}
	defer src.Close()

	first, err := src.First()
	if err != nil {
		t.Fatalf("expected a first migration: %v", err)
	}
	if first != 1 {
		t.Errorf("expected first migration version 1, got %d", first)
	}
}

func TestInitialMigrationTargetsChatbotSchema(t *testing.T) {
	t.Parallel()

	up, err := fs.ReadFile(migrations.FS, "000001_create_chatbot_schema.up.sql")
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	sql := string(up)
	for _, want := range []string{
		"CREATE TABLE chatbot.conversations",
		"CREATE TABLE chatbot.messages",
		"ON DELETE CASCADE",
		"CONSTRAINT messages_role_chk",
		"CONSTRAINT messages_content_not_blank_chk",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("up migration missing %q", want)
		}
	}
	if strings.Contains(sql, "public.") {
		t.Error("up migration must not create objects in the public schema")
	}

	down, err := fs.ReadFile(migrations.FS, "000001_create_chatbot_schema.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	d := string(down)
	msgIdx := strings.Index(d, "DROP TABLE IF EXISTS chatbot.messages")
	convIdx := strings.Index(d, "DROP TABLE IF EXISTS chatbot.conversations")
	if msgIdx < 0 || convIdx < 0 || msgIdx > convIdx {
		t.Error("down migration must drop messages before conversations")
	}
}
