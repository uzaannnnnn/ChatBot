//go:build integration

// Integration tests for the PostgreSQL repositories.
//
// They run only with the "integration" build tag and REQUIRE TEST_DATABASE_URL.
// If the variable is missing the test binary fails loudly instead of skipping,
// so a run can never silently report success without touching a database.
//
//	TEST_DATABASE_URL=postgres://... go test -tags=integration -count=1 ./internal/repository/postgres/...
//
// Safety:
//   - Only "migrate up" is executed (idempotent). Rollback is never run.
//   - Every test deletes the conversations it created (messages cascade).
//   - The suite refuses to run when TEST_DATABASE_URL equals DATABASE_URL.
package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/auth"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/database"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/httpserver"
	"github.com/uzaannnnnn/ChatBot/backend/internal/migration"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository/postgres"
	"github.com/uzaannnnnn/ChatBot/backend/internal/service"
)

const (
	testOwnerA = "11111111-1111-4111-8111-111111111111"
	testOwnerB = "22222222-2222-4222-8222-222222222222"
)

type integrationAuthVerifier struct{}

func (v *integrationAuthVerifier) VerifyToken(_ context.Context, token string) (string, error) {
	if token == "token-a" {
		return testOwnerA, nil
	}
	if token == "token-b" {
		return testOwnerB, nil
	}
	return "", auth.ErrInvalidToken
}

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		fmt.Fprintln(os.Stderr, "FAIL: TEST_DATABASE_URL is required for integration tests (see docs/database-design.md)")
		return 1
	}
	if prod := strings.TrimSpace(os.Getenv("DATABASE_URL")); prod != "" && prod == url && os.Getenv("ALLOW_SHARED_TEST_DB") != "1" {
		fmt.Fprintln(os.Stderr, "FAIL: TEST_DATABASE_URL must not equal DATABASE_URL unless ALLOW_SHARED_TEST_DB=1 is explicitly set")
		return 1
	}
	if err := config.ValidateDatabaseURL(url); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runner, err := migration.New(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		return 1
	}
	if _, err := runner.Up(); err != nil {
		_ = runner.Close()
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		return 1
	}
	_ = runner.Close()

	pool, err := database.NewPostgresPool(ctx, &config.Config{
		DatabaseURL:       url,
		DBMaxConns:        4,
		DBMinConns:        0,
		DBMaxConnLifetime: config.DefaultDBMaxConnLifetime,
		DBMaxConnIdleTime: config.DefaultDBMaxConnIdleTime,
		DBConnectTimeout:  10 * time.Second,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		return 1
	}
	defer pool.Close()
	testPool = pool

	return m.Run()
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func randomUUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// createConversation creates a conversation and registers cleanup.
func createConversation(t *testing.T, repo *postgres.ConversationRepository, title string) domain.Conversation {
	t.Helper()
	c, err := repo.CreateConversation(testCtx(t), testOwnerA, title)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = testPool.Exec(ctx, `DELETE FROM chatbot.conversations WHERE id = $1`, c.ID)
	})
	return c
}

func countMessages(t *testing.T, conversationID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(testCtx(t), `SELECT count(*) FROM chatbot.messages WHERE conversation_id = $1`, conversationID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return n
}

func TestSchema_ObjectsAndConstraints(t *testing.T) {
	ctx := testCtx(t)

	var tables int
	err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'chatbot' AND table_name IN ('conversations', 'messages', 'schema_migrations')`).Scan(&tables)
	if err != nil || tables != 3 {
		t.Fatalf("expected 3 chatbot tables, got %d (err=%v)", tables, err)
	}

	var publicTables int
	err = testPool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name IN ('conversations', 'messages', 'schema_migrations')`).Scan(&publicTables)
	if err != nil || publicTables != 0 {
		t.Fatalf("expected no chatbot tables in public schema, got %d (err=%v)", publicTables, err)
	}

	wantConstraints := []string{
		"conversations_pkey", "conversations_title_not_blank_chk", "conversations_updated_at_chk",
		"messages_pkey", "messages_conversation_id_fkey", "messages_role_chk", "messages_content_not_blank_chk",
	}
	rows, err := testPool.Query(ctx, `
		SELECT con.conname FROM pg_constraint con
		JOIN pg_namespace n ON n.oid = con.connamespace
		WHERE n.nspname = 'chatbot'`)
	if err != nil {
		t.Fatalf("query constraints: %v", err)
	}
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan constraint: %v", err)
		}
		got[name] = true
	}
	rows.Close()
	for _, c := range wantConstraints {
		if !got[c] {
			t.Errorf("missing constraint %s", c)
		}
	}

	var fkDelete string
	err = testPool.QueryRow(ctx, `
		SELECT confdeltype::text FROM pg_constraint
		WHERE conname = 'messages_conversation_id_fkey'`).Scan(&fkDelete)
	if err != nil || fkDelete != "c" {
		t.Errorf("expected ON DELETE CASCADE (c), got %q (err=%v)", fkDelete, err)
	}

	for _, idx := range []string{"conversations_updated_at_id_idx", "messages_conversation_id_created_at_id_idx"} {
		var exists bool
		if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'chatbot' AND indexname = $1)`, idx).Scan(&exists); err != nil || !exists {
			t.Errorf("missing index %s (err=%v)", idx, err)
		}
	}
}

func TestSchema_DefaultTitle(t *testing.T) {
	ctx := testCtx(t)

	var id, title string
	if err := testPool.QueryRow(ctx, `INSERT INTO chatbot.conversations (owner_id) VALUES ($1) RETURNING id, title`, testOwnerA).Scan(&id, &title); err != nil {
		t.Fatalf("insert default conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chatbot.conversations WHERE id = $1`, id)
	})
	if title != domain.DefaultConversationTitle {
		t.Errorf("expected default title %q, got %q", domain.DefaultConversationTitle, title)
	}
}

func TestConversation_CreateAndGet(t *testing.T) {
	repo := postgres.NewConversationRepository(testPool)
	created := createConversation(t, repo, "Integration: create and get")

	if !domain.IsValidUUID(created.ID) {
		t.Fatalf("expected generated UUID, got %q", created.ID)
	}
	if created.CreatedAt.IsZero() || !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Errorf("expected created_at == updated_at on insert, got %v / %v", created.CreatedAt, created.UpdatedAt)
	}

	got, err := repo.GetConversationByID(testCtx(t), testOwnerA, created.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.ID != created.ID || got.Title != "Integration: create and get" {
		t.Errorf("unexpected conversation: %+v", got)
	}
}

func TestConversation_TitleConstraints(t *testing.T) {
	repo := postgres.NewConversationRepository(testPool)

	if _, err := repo.CreateConversation(testCtx(t), testOwnerA, strings.Repeat("a", 201)); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Errorf("expected ErrInvalidTitle for 201-char title, got %v", err)
	}
	if _, err := repo.CreateConversation(testCtx(t), testOwnerA, "   "); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Errorf("expected ErrInvalidTitle for blank title, got %v", err)
	}

	max := createConversation(t, repo, strings.Repeat("é", 200))
	if len([]rune(max.Title)) != 200 {
		t.Errorf("expected 200-character title to be accepted, got %d runes", len([]rune(max.Title)))
	}
}

func TestConversation_ListOrdering(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)

	a := createConversation(t, convRepo, "Integration: list A")
	b := createConversation(t, convRepo, "Integration: list B")
	c := createConversation(t, convRepo, "Integration: list C")

	// New activity on A moves it to the top.
	if _, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: a.ID, Role: domain.RoleUser, Content: "bump"}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	list, err := convRepo.ListConversations(testCtx(t), testOwnerA, domain.Page{Limit: domain.MaxPageLimit})
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	order := relativeOrder(list, a.ID, b.ID, c.ID)
	want := []string{a.ID, c.ID, b.ID}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("expected order A, C, B by updated_at DESC; got %v", order)
	}

	// Tie-break: identical updated_at must order by id DESC.
	if _, err := testPool.Exec(testCtx(t), `
		UPDATE chatbot.conversations SET updated_at = NOW() + interval '1 minute'
		WHERE id = ANY($1::uuid[])`, []string{a.ID, b.ID, c.ID}); err != nil {
		t.Fatalf("equalize updated_at: %v", err)
	}
	list, err = convRepo.ListConversations(testCtx(t), testOwnerA, domain.Page{Limit: domain.MaxPageLimit})
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	order = relativeOrder(list, a.ID, b.ID, c.ID)
	want = []string{a.ID, b.ID, c.ID}
	sort.Sort(sort.Reverse(sort.StringSlice(want)))
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("expected id DESC tie-break %v, got %v", want, order)
	}

	// Pagination returns disjoint pages.
	first, err := convRepo.ListConversations(testCtx(t), testOwnerA, domain.Page{Limit: 1, Offset: 0})
	if err != nil || len(first) != 1 {
		t.Fatalf("first page: len=%d err=%v", len(first), err)
	}
	second, err := convRepo.ListConversations(testCtx(t), testOwnerA, domain.Page{Limit: 1, Offset: 1})
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID {
		t.Fatalf("second page must differ from first: %v / %v (err=%v)", first, second, err)
	}
}

func relativeOrder(list []domain.Conversation, ids ...string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var order []string
	for _, c := range list {
		if want[c.ID] {
			order = append(order, c.ID)
		}
	}
	return order
}

func TestConversation_InvalidPagination(t *testing.T) {
	repo := postgres.NewConversationRepository(testPool)
	for _, p := range []domain.Page{{Limit: 0}, {Limit: domain.MaxPageLimit + 2}, {Limit: 5, Offset: -1}} {
		if _, err := repo.ListConversations(testCtx(t), testOwnerA, p); !errors.Is(err, domain.ErrInvalidPagination) {
			t.Errorf("page %+v: expected ErrInvalidPagination, got %v", p, err)
		}
	}
}

func TestConversation_UpdateTitle(t *testing.T) {
	repo := postgres.NewConversationRepository(testPool)
	created := createConversation(t, repo, "Integration: before rename")

	time.Sleep(10 * time.Millisecond)
	updated, err := repo.UpdateConversationTitle(testCtx(t), testOwnerA, created.ID, "Integration: after rename")
	if err != nil {
		t.Fatalf("update title: %v", err)
	}
	if updated.Title != "Integration: after rename" {
		t.Errorf("expected new title, got %q", updated.Title)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("expected updated_at to advance: before=%v after=%v", created.UpdatedAt, updated.UpdatedAt)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("created_at must not change: before=%v after=%v", created.CreatedAt, updated.CreatedAt)
	}

	if _, err := repo.UpdateConversationTitle(testCtx(t), testOwnerA, randomUUID(t), "x"); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound for missing conversation, got %v", err)
	}
	if _, err := repo.UpdateConversationTitle(testCtx(t), testOwnerA, created.ID, strings.Repeat("b", 201)); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Errorf("expected ErrInvalidTitle for too-long title, got %v", err)
	}
}

func TestConversation_DeleteCascadesMessages(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: delete cascade")

	for _, content := range []string{"one", "two", "three"} {
		if _, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: conv.ID, Role: domain.RoleUser, Content: content}); err != nil {
			t.Fatalf("create message: %v", err)
		}
	}
	if n := countMessages(t, conv.ID); n != 3 {
		t.Fatalf("expected 3 messages before delete, got %d", n)
	}

	if err := convRepo.DeleteConversation(testCtx(t), testOwnerA, conv.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	if _, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, conv.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound after delete, got %v", err)
	}
	if n := countMessages(t, conv.ID); n != 0 {
		t.Errorf("expected messages to cascade-delete, %d remain", n)
	}
	if err := convRepo.DeleteConversation(testCtx(t), testOwnerA, conv.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on second delete, got %v", err)
	}
}

func TestMessage_CreateAndListOrdering(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: messages")

	inputs := []domain.NewMessage{
		{ConversationID: conv.ID, Role: domain.RoleUser, Content: "Hello"},
		{ConversationID: conv.ID, Role: domain.RoleAssistant, Content: "Hi! How can I help?\n\n- item"},
		{ConversationID: conv.ID, Role: domain.RoleUser, Content: "  Tell me a joke  "},
	}
	for _, in := range inputs {
		m, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, in)
		if err != nil {
			t.Fatalf("create message: %v", err)
		}
		if !domain.IsValidUUID(m.ID) || m.ConversationID != conv.ID || m.Role != in.Role || m.Content != in.Content || m.CreatedAt.IsZero() {
			t.Fatalf("unexpected created message: %+v", m)
		}
	}

	list, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(list) != len(inputs) {
		t.Fatalf("expected %d messages, got %d", len(inputs), len(list))
	}
	for i, m := range list {
		if m.Content != inputs[i].Content || m.Role != inputs[i].Role {
			t.Errorf("position %d: expected %q/%s, got %q/%s", i, inputs[i].Content, inputs[i].Role, m.Content, m.Role)
		}
	}

	page, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 1, Offset: 1})
	if err != nil || len(page) != 1 || page[0].ID != list[1].ID {
		t.Errorf("expected second message on page offset=1, got %v (err=%v)", page, err)
	}

	// Tie-break: identical created_at must order by id ASC.
	tieConv := createConversation(t, convRepo, "Integration: message tie-break")
	var ids []string
	for i := 0; i < 3; i++ {
		var id string
		err := testPool.QueryRow(testCtx(t), `
			INSERT INTO chatbot.messages (conversation_id, role, content, created_at)
			VALUES ($1, 'user', $2, '2026-01-01T00:00:00Z') RETURNING id`, tieConv.ID, fmt.Sprintf("tie %d", i)).Scan(&id)
		if err != nil {
			t.Fatalf("insert tie message: %v", err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tied, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, tieConv.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("list tie messages: %v", err)
	}
	for i := range ids {
		if tied[i].ID != ids[i] {
			t.Fatalf("expected id ASC tie-break %v, got position %d = %s", ids, i, tied[i].ID)
		}
	}
}

func TestMessage_EmptyConversationReturnsEmptyList(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: empty")

	list, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("expected no error for existing empty conversation, got %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d messages", len(list))
	}
}

func TestMessage_InvalidRoleRejectedAtomically(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: invalid role")

	for _, role := range []domain.Role{"system", "", "USER", domain.Role(strings.Repeat("x", 30))} {
		_, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: conv.ID, Role: role, Content: "hello"})
		if !errors.Is(err, domain.ErrInvalidRole) {
			t.Errorf("role %q: expected ErrInvalidRole, got %v", role, err)
		}
	}

	after, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, conv.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if !after.UpdatedAt.Equal(conv.UpdatedAt) {
		t.Errorf("failed insert must roll back updated_at change: before=%v after=%v", conv.UpdatedAt, after.UpdatedAt)
	}
	if n := countMessages(t, conv.ID); n != 0 {
		t.Errorf("expected no messages after rejected inserts, got %d", n)
	}
}

func TestMessage_EmptyContentRejected(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: empty content")

	for _, content := range []string{"", "   ", "\n\t \r\n"} {
		_, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: conv.ID, Role: domain.RoleUser, Content: content})
		if !errors.Is(err, domain.ErrInvalidContent) {
			t.Errorf("content %q: expected ErrInvalidContent, got %v", content, err)
		}
	}
	if n := countMessages(t, conv.ID); n != 0 {
		t.Errorf("expected no messages after rejected inserts, got %d", n)
	}
}

func TestMissingConversationBehavior(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	missing := randomUUID(t)

	if _, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, missing); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("get: expected ErrConversationNotFound, got %v", err)
	}
	if err := convRepo.DeleteConversation(testCtx(t), testOwnerA, missing); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("delete: expected ErrConversationNotFound, got %v", err)
	}
	if _, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: missing, Role: domain.RoleUser, Content: "hi"}); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("create message: expected ErrConversationNotFound, got %v", err)
	}
	if _, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, missing, domain.Page{Limit: 10}); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("list messages: expected ErrConversationNotFound, got %v", err)
	}
	if _, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, "not-a-uuid"); !errors.Is(err, domain.ErrInvalidID) {
		t.Errorf("malformed id: expected ErrInvalidID, got %v", err)
	}
}

func TestMessage_InsertRefreshesConversationUpdatedAt(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: updated_at")

	time.Sleep(10 * time.Millisecond)
	msg, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{ConversationID: conv.ID, Role: domain.RoleUser, Content: "touch"})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	after, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, conv.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if !after.UpdatedAt.After(conv.UpdatedAt) {
		t.Errorf("expected updated_at to advance after message insert: before=%v after=%v", conv.UpdatedAt, after.UpdatedAt)
	}
	if after.UpdatedAt.Before(msg.CreatedAt) {
		t.Errorf("expected updated_at (%v) >= message created_at (%v)", after.UpdatedAt, msg.CreatedAt)
	}
}

func TestMessage_ConcurrentCreateAndMonotonicUpdatedAt(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: concurrency")

	// 1. Verify concurrent message inserts do not lose messages and updated_at advances monotonically.
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			role := domain.RoleUser
			if idx%2 == 1 {
				role = domain.RoleAssistant
			}
			_, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{
				ConversationID: conv.ID,
				Role:           role,
				Content:        fmt.Sprintf("concurrent message %02d", idx),
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent CreateMessage error: %v", err)
	}

	msgs, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: domain.MaxPageLimit})
	if err != nil {
		t.Fatalf("list concurrent messages: %v", err)
	}
	if len(msgs) != workers {
		t.Fatalf("expected %d messages after concurrent inserts, got %d", workers, len(msgs))
	}

	afterConcurrent, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, conv.ID)
	if err != nil {
		t.Fatalf("get conversation after concurrent inserts: %v", err)
	}
	if !afterConcurrent.UpdatedAt.After(conv.UpdatedAt) {
		t.Errorf("expected updated_at to advance after concurrent inserts: before=%v after=%v", conv.UpdatedAt, afterConcurrent.UpdatedAt)
	}

	// 2. Verify GREATEST(updated_at, clock_timestamp()) prevents updated_at from moving backwards
	// even if updated_at already holds a timestamp ahead of the current transaction's NOW().
	futureTime := time.Now().UTC().Add(1 * time.Hour).Truncate(time.Microsecond)
	if _, err := testPool.Exec(testCtx(t), `UPDATE chatbot.conversations SET updated_at = $2 WHERE id = $1`, conv.ID, futureTime); err != nil {
		t.Fatalf("seed future updated_at: %v", err)
	}

	if _, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{
		ConversationID: conv.ID,
		Role:           domain.RoleUser,
		Content:        "after future timestamp",
	}); err != nil {
		t.Fatalf("CreateMessage after future timestamp: %v", err)
	}

	afterMsg, err := convRepo.GetConversationByID(testCtx(t), testOwnerA, conv.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if afterMsg.UpdatedAt.Before(futureTime) {
		t.Errorf("CreateMessage regressed updated_at: got %v, want >= %v", afterMsg.UpdatedAt, futureTime)
	}

	afterRename, err := convRepo.UpdateConversationTitle(testCtx(t), testOwnerA, conv.ID, "Integration: renamed after future")
	if err != nil {
		t.Fatalf("UpdateConversationTitle after future timestamp: %v", err)
	}
	if afterRename.UpdatedAt.Before(futureTime) {
		t.Errorf("UpdateConversationTitle regressed updated_at: got %v, want >= %v", afterRename.UpdatedAt, futureTime)
	}
}

func TestServices_EndToEnd(t *testing.T) {
	convSvc := service.NewConversationService(postgres.NewConversationRepository(testPool))
	msgSvc := service.NewMessageService(postgres.NewMessageRepository(testPool))

	conv, err := convSvc.Create(testCtx(t), testOwnerA, "   ")
	if err != nil {
		t.Fatalf("create via service: %v", err)
	}
	t.Cleanup(func() { _ = convSvc.Delete(context.Background(), testOwnerA, conv.ID) })
	if conv.Title != domain.DefaultConversationTitle {
		t.Errorf("expected default title, got %q", conv.Title)
	}

	if _, err := msgSvc.Create(testCtx(t), testOwnerA, conv.ID, domain.RoleUser, "What is Go?"); err != nil {
		t.Fatalf("create message via service: %v", err)
	}
	if _, err := msgSvc.Create(testCtx(t), testOwnerA, conv.ID, domain.RoleAssistant, "Go is a programming language."); err != nil {
		t.Fatalf("create message via service: %v", err)
	}
	msgs, err := msgSvc.List(testCtx(t), testOwnerA, conv.ID, domain.Page{})
	if err != nil || len(msgs) != 2 {
		t.Fatalf("list via service: len=%d err=%v", len(msgs), err)
	}
	if msgs[0].Role != domain.RoleUser || msgs[1].Role != domain.RoleAssistant {
		t.Errorf("unexpected roles order: %s, %s", msgs[0].Role, msgs[1].Role)
	}
}

func TestHTTPAPI_IntegrationLifecycle(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	convSvc := service.NewConversationService(convRepo)
	msgSvc := service.NewMessageService(msgRepo)

	router := httpserver.NewRouter(httpserver.Dependencies{
		AuthVerifier:     &integrationAuthVerifier{},
		DisableRateLimit: true,
		Config: &config.Config{
			AppEnv:              "test",
			FrontendOrigin:      "http://localhost:5173",
			MaxRequestBodyBytes: config.DefaultMaxRequestBodyBytes,
		},
		HealthHandler:       handler.NewHealthHandler(testPool, 2*time.Second),
		ConversationHandler: handler.NewConversationHandler(convSvc),
		MessageHandler:      handler.NewMessageHandler(msgSvc),
	})

	// 1. POST /api/v1/conversations (default title)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{}`))
	reqCreate.Header.Set("Content-Type", "application/json")
	recCreate := httptest.NewRecorder()
	reqCreate.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create conversation: expected 201, got %d: %s", recCreate.Code, recCreate.Body.String())
	}
	var createdConv dto.SingleResponse[dto.ConversationResponse]
	if err := json.Unmarshal(recCreate.Body.Bytes(), &createdConv); err != nil {
		t.Fatalf("unmarshal created conversation: %v", err)
	}
	convID := createdConv.Data.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chatbot.conversations WHERE id = $1`, convID)
	})
	if createdConv.Data.Title != domain.DefaultConversationTitle {
		t.Errorf("expected default title %q, got %q", domain.DefaultConversationTitle, createdConv.Data.Title)
	}

	// 2. PATCH /api/v1/conversations/:id
	reqPatch := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+convID, strings.NewReader(`{"title":"Sprint 3 API Test"}`))
	reqPatch.Header.Set("Content-Type", "application/json")
	recPatch := httptest.NewRecorder()
	reqPatch.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recPatch, reqPatch)
	if recPatch.Code != http.StatusOK {
		t.Fatalf("patch conversation: expected 200, got %d: %s", recPatch.Code, recPatch.Body.String())
	}
	var patchedConv dto.SingleResponse[dto.ConversationResponse]
	if err := json.Unmarshal(recPatch.Body.Bytes(), &patchedConv); err != nil {
		t.Fatalf("unmarshal patched conversation: %v", err)
	}
	if patchedConv.Data.Title != "Sprint 3 API Test" {
		t.Errorf("expected updated title, got %q", patchedConv.Data.Title)
	}

	// 3. GET /api/v1/conversations/:id/messages (empty history)
	reqEmptyMsgs := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", nil)
	recEmptyMsgs := httptest.NewRecorder()
	reqEmptyMsgs.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recEmptyMsgs, reqEmptyMsgs)
	if recEmptyMsgs.Code != http.StatusOK {
		t.Fatalf("list empty messages: expected 200, got %d", recEmptyMsgs.Code)
	}
	var emptyMsgList dto.ListResponse[dto.MessageResponse]
	if err := json.Unmarshal(recEmptyMsgs.Body.Bytes(), &emptyMsgList); err != nil {
		t.Fatalf("unmarshal empty messages: %v", err)
	}
	if len(emptyMsgList.Data) != 0 || emptyMsgList.Pagination.HasMore {
		t.Errorf("expected 0 messages and has_more=false, got %+v", emptyMsgList)
	}

	// 4. POST /api/v1/conversations/:id/messages (create 2 user messages)
	for _, text := range []string{"First user message", "Second user message"} {
		reqMsg := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", strings.NewReader(fmt.Sprintf(`{"content":%q}`, text)))
		reqMsg.Header.Set("Content-Type", "application/json")
		recMsg := httptest.NewRecorder()
		reqMsg.Header.Set("Authorization", "Bearer token-a")
		router.ServeHTTP(recMsg, reqMsg)
		if recMsg.Code != http.StatusCreated {
			t.Fatalf("post message %q: expected 201, got %d: %s", text, recMsg.Code, recMsg.Body.String())
		}
		var createdMsg dto.SingleResponse[dto.MessageResponse]
		if err := json.Unmarshal(recMsg.Body.Bytes(), &createdMsg); err != nil {
			t.Fatalf("unmarshal created message: %v", err)
		}
		if createdMsg.Data.Role != domain.RoleUser || createdMsg.Data.Content != text {
			t.Errorf("unexpected created message: %+v", createdMsg.Data)
		}
	}

	// 5. GET /api/v1/conversations/:id/messages?limit=1&offset=0 (verify has_more=true)
	reqPage1 := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID+"/messages?limit=1&offset=0", nil)
	recPage1 := httptest.NewRecorder()
	reqPage1.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recPage1, reqPage1)
	if recPage1.Code != http.StatusOK {
		t.Fatalf("list messages page 1: expected 200, got %d", recPage1.Code)
	}
	var page1 dto.ListResponse[dto.MessageResponse]
	if err := json.Unmarshal(recPage1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("unmarshal page 1: %v", err)
	}
	if len(page1.Data) != 1 || page1.Data[0].Content != "First user message" || !page1.Pagination.HasMore {
		t.Errorf("unexpected page 1: %+v", page1)
	}

	// 6. GET /api/v1/conversations/:id/messages?limit=1&offset=1 (verify has_more=false)
	reqPage2 := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID+"/messages?limit=1&offset=1", nil)
	recPage2 := httptest.NewRecorder()
	reqPage2.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recPage2, reqPage2)
	if recPage2.Code != http.StatusOK {
		t.Fatalf("list messages page 2: expected 200, got %d", recPage2.Code)
	}
	var page2 dto.ListResponse[dto.MessageResponse]
	if err := json.Unmarshal(recPage2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("unmarshal page 2: %v", err)
	}
	if len(page2.Data) != 1 || page2.Data[0].Content != "Second user message" || page2.Pagination.HasMore {
		t.Errorf("unexpected page 2: %+v", page2)
	}

	// 7. DELETE /api/v1/conversations/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/"+convID, nil)
	recDel := httptest.NewRecorder()
	reqDel.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("delete conversation: expected 204, got %d", recDel.Code)
	}

	// 8. GET /api/v1/conversations/:id returns 404 after delete
	reqGetDeleted := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID, nil)
	recGetDeleted := httptest.NewRecorder()
	reqGetDeleted.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(recGetDeleted, reqGetDeleted)
	if recGetDeleted.Code != http.StatusNotFound {
		t.Fatalf("get deleted conversation: expected 404, got %d", recGetDeleted.Code)
	}
}

func TestMessage_ListRecentMessages(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: recent messages")

	for i := 1; i <= 5; i++ {
		_, err := msgRepo.CreateMessage(testCtx(t), testOwnerA, domain.NewMessage{
			ConversationID: conv.ID,
			Role:           domain.RoleUser,
			Content:        fmt.Sprintf("Message %d", i),
		})
		if err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	recent, err := msgRepo.ListRecentMessages(testCtx(t), testOwnerA, conv.ID, 3)
	if err != nil {
		t.Fatalf("ListRecentMessages: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent messages, got %d", len(recent))
	}
	// Chronological order: Message 3, Message 4, Message 5
	expected := []string{"Message 3", "Message 4", "Message 5"}
	for i, want := range expected {
		if recent[i].Content != want {
			t.Errorf("pos %d: expected %q, got %q", i, want, recent[i].Content)
		}
	}
}

type integrationMockStream struct {
	chunks []string
	idx    int
	err    error
}

func (s *integrationMockStream) Recv() (ai.StreamChunk, error) {
	if s.err != nil {
		return ai.StreamChunk{}, s.err
	}
	if s.idx >= len(s.chunks) {
		return ai.StreamChunk{}, io.EOF
	}
	chunk := s.chunks[s.idx]
	s.idx++
	return ai.StreamChunk{Delta: chunk}, nil
}

func (s *integrationMockStream) Close() error {
	return nil
}

type integrationMockAIProvider struct {
	chunks []string
	err    error
}

func (p *integrationMockAIProvider) StreamChat(_ context.Context, _ ai.ChatRequest) (ai.Stream, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &integrationMockStream{chunks: p.chunks}, nil
}

func (p *integrationMockAIProvider) Chat(_ context.Context, _ ai.ChatRequest) (ai.ChatResponse, error) {
	return ai.ChatResponse{}, nil
}

func TestChatService_IntegrationStreamPersistence(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	conv := createConversation(t, convRepo, "Integration: stream persistence")

	aiMock := &integrationMockAIProvider{
		chunks: []string{"Go ", "is ", "great!"},
	}
	chatSvc := service.NewChatService(convRepo, msgRepo, aiMock, service.ChatServiceConfig{
		ModelID:        "test-model",
		MaxContextMsgs: 10,
	})

	// 1. Successful stream generation
	userMsg, assistantMsg, err := chatSvc.StreamMessage(testCtx(t), testOwnerA, conv.ID, "Why Go?", nil)
	if err != nil {
		t.Fatalf("StreamMessage failed: %v", err)
	}
	if userMsg.Content != "Why Go?" || assistantMsg.Content != "Go is great!" {
		t.Errorf("unexpected messages: user=%+v, assistant=%+v", userMsg, assistantMsg)
	}

	msgs, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 10})
	if err != nil || len(msgs) != 2 {
		t.Fatalf("expected 2 messages in DB, got %d (err=%v)", len(msgs), err)
	}
	if msgs[0].Role != domain.RoleUser || msgs[1].Role != domain.RoleAssistant {
		t.Errorf("wrong roles order: %v, %v", msgs[0].Role, msgs[1].Role)
	}

	// 2. Provider failure: user message persisted, assistant NOT persisted
	aiFailing := &integrationMockAIProvider{
		err: errors.New("huggingface provider error 503"),
	}
	chatSvcFailing := service.NewChatService(convRepo, msgRepo, aiFailing, service.ChatServiceConfig{})

	_, _, err = chatSvcFailing.StreamMessage(testCtx(t), testOwnerA, conv.ID, "Second prompt that fails", nil)
	if err == nil {
		t.Fatal("expected provider failure, got nil")
	}

	msgsAfterFail, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 10})
	if err != nil || len(msgsAfterFail) != 3 { // 2 previous + 1 failed user message
		t.Fatalf("expected 3 messages in DB (user message persisted, assistant NOT persisted), got %d (err=%v)", len(msgsAfterFail), err)
	}
	if msgsAfterFail[2].Content != "Second prompt that fails" || msgsAfterFail[2].Role != domain.RoleUser {
		t.Errorf("expected 3rd message to be the user prompt, got %+v", msgsAfterFail[2])
	}
}

func TestHTTPAPI_IntegrationSSEStream(t *testing.T) {
	convRepo := postgres.NewConversationRepository(testPool)
	msgRepo := postgres.NewMessageRepository(testPool)
	convSvc := service.NewConversationService(convRepo)
	msgSvc := service.NewMessageService(msgRepo)

	aiMock := &integrationMockAIProvider{
		chunks: []string{"Streaming ", "response ", "from ", "integration test!"},
	}
	chatSvc := service.NewChatService(convRepo, msgRepo, aiMock, service.ChatServiceConfig{
		ModelID: "test-model",
	})

	router := httpserver.NewRouter(httpserver.Dependencies{
		AuthVerifier:     &integrationAuthVerifier{},
		DisableRateLimit: true,
		Config: &config.Config{
			AppEnv:              "test",
			FrontendOrigin:      "http://localhost:5173",
			MaxRequestBodyBytes: config.DefaultMaxRequestBodyBytes,
		},
		HealthHandler:       handler.NewHealthHandler(testPool, 2*time.Second),
		ConversationHandler: handler.NewConversationHandler(convSvc),
		MessageHandler:      handler.NewMessageHandler(msgSvc),
		ChatStreamHandler:   handler.NewChatStreamHandler(chatSvc),
	})

	conv := createConversation(t, convRepo, "Integration: HTTP SSE")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conv.ID+"/messages/stream", strings.NewReader(`{"content":"Hello from SSE test"}`))
	req.Header.Set("Authorization", "Bearer token-a")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token-a")
	rec := httptest.NewRecorder()
	req.Header.Set("Authorization", "Bearer token-a")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	body := rec.Body.String()
	for _, expectedToken := range []string{"message_start", "Streaming ", "response ", "integration test!", "message_complete"} {
		if !strings.Contains(body, expectedToken) {
			t.Errorf("expected %q in SSE stream output, got: %s", expectedToken, body)
		}
	}

	// Verify both user and assistant messages are now in the DB
	storedMsgs, err := msgRepo.ListMessagesByConversationID(testCtx(t), testOwnerA, conv.ID, domain.Page{Limit: 10})
	if err != nil || len(storedMsgs) != 2 {
		t.Fatalf("expected 2 messages in DB after stream, got %d (err=%v)", len(storedMsgs), err)
	}
	if storedMsgs[0].Content != "Hello from SSE test" || storedMsgs[0].Role != domain.RoleUser {
		t.Errorf("unexpected user message in DB: %+v", storedMsgs[0])
	}
	if storedMsgs[1].Content != "Streaming response from integration test!" || storedMsgs[1].Role != domain.RoleAssistant {
		t.Errorf("unexpected assistant message in DB: %+v", storedMsgs[1])
	}
}

func TestOwnership_UserIsolation(t *testing.T) {
	pool := testPool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	convRepo := postgres.NewConversationRepository(pool)
	msgRepo := postgres.NewMessageRepository(pool)

	convA, err := convRepo.CreateConversation(ctx, testOwnerA, "User A Secret Chat")
	if err != nil {
		t.Fatalf("create convA: %v", err)
	}
	_, err = msgRepo.CreateMessage(ctx, testOwnerA, domain.NewMessage{
		ConversationID: convA.ID,
		Role:           domain.RoleUser,
		Content:        "Confidential prompt A",
	})
	if err != nil {
		t.Fatalf("create msgA: %v", err)
	}

	// User B cannot get, list, rename, delete, or read/write messages in User A conversation
	if _, err := convRepo.GetConversationByID(ctx, testOwnerB, convA.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on Get by User B, got %v", err)
	}
	listB, err := convRepo.ListConversations(ctx, testOwnerB, domain.Page{Limit: 20, Offset: 0})
	if err != nil || len(listB) != 0 {
		t.Errorf("expected empty conversation list for User B, got len=%d err=%v", len(listB), err)
	}
	if _, err := convRepo.UpdateConversationTitle(ctx, testOwnerB, convA.ID, "Hacked"); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on UpdateTitle by User B, got %v", err)
	}
	if err := convRepo.DeleteConversation(ctx, testOwnerB, convA.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on Delete by User B, got %v", err)
	}
	if _, err := msgRepo.ListMessagesByConversationID(ctx, testOwnerB, convA.ID, domain.Page{Limit: 20, Offset: 0}); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on ListMessages by User B, got %v", err)
	}
	if _, err := msgRepo.ListRecentMessages(ctx, testOwnerB, convA.ID, 10); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on ListRecentMessages by User B, got %v", err)
	}
	if _, err := msgRepo.CreateMessage(ctx, testOwnerB, domain.NewMessage{
		ConversationID: convA.ID,
		Role:           domain.RoleUser,
		Content:        "Injected by User B",
	}); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("expected ErrConversationNotFound on CreateMessage by User B, got %v", err)
	}

	// Verify User A conversation and message remain intact
	gotA, err := convRepo.GetConversationByID(ctx, testOwnerA, convA.ID)
	if err != nil || gotA.Title != "User A Secret Chat" {
		t.Fatalf("User A conversation corrupted: got=%+v err=%v", gotA, err)
	}
}
