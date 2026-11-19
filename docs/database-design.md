# Database Design — AI Mini Chatbot

## 1. Overview

The AI Mini Chatbot persists all application state inside a dedicated PostgreSQL schema named `chatbot` on Supabase PostgreSQL 17 (accessed via the Supabase Session Pooler over TLS).

Key architectural rules:
- **Dedicated Schema**: All tables, indexes, constraints, and migration tracking (`chatbot.schema_migrations`) reside in `chatbot`, never in `public`.
- **Supabase Data API Isolation**: `anon` and `authenticated` roles have all privileges revoked on schema `chatbot` so the schema is never exposed via PostgREST / Supabase Data API.
- **No ORM**: Queries are written in parameterized SQL and executed through `pgx/v5` (`pgxpool`).
- **Explicit Migrations**: Schema changes are versioned with `golang-migrate` (`backend/migrations`) and executed via `backend/cmd/migrate`. Migrations never run automatically during API server startup.

---

## 2. Entity-Relationship Diagram (ERD)

```mermaid
erDiagram
    CONVERSATIONS ||--o{ MESSAGES : "has many (ON DELETE CASCADE)"

    CONVERSATIONS {
        UUID id PK "DEFAULT gen_random_uuid()"
        VARCHAR_200 title "NOT NULL DEFAULT 'New conversation'"
        TIMESTAMPTZ created_at "NOT NULL DEFAULT NOW()"
        TIMESTAMPTZ updated_at "NOT NULL DEFAULT NOW()"
    }

    MESSAGES {
        UUID id PK "DEFAULT gen_random_uuid()"
        UUID conversation_id FK "NOT NULL REFERENCES chatbot.conversations(id) ON DELETE CASCADE"
        VARCHAR_20 role "NOT NULL CHECK (role IN ('user', 'assistant'))"
        TEXT content "NOT NULL CHECK (content ~ '[^[:space:]]')"
        TIMESTAMPTZ created_at "NOT NULL DEFAULT NOW()"
    }
```

---

## 3. Schema & Table Specifications

### Schema: `chatbot`

| Object | Type | Purpose |
|---|---|---|
| `chatbot.conversations` | Table | Stores chat threads and metadata |
| `chatbot.messages` | Table | Stores ordered user and assistant messages |
| `chatbot.schema_migrations` | Table | Tracks applied `golang-migrate` schema versions |

### Table: `chatbot.conversations`

| Column | Type | Nullable | Default | Description |
|---|---|---|---|---|
| `id` | `UUID` | `NOT NULL` | `gen_random_uuid()` | Primary key |
| `title` | `VARCHAR(200)` | `NOT NULL` | `'New conversation'` | Conversation display title (1–200 non-blank characters) |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | `NOW()` | Creation timestamp |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL` | `NOW()` | Last activity timestamp (updated on title change or message insert) |

**Named Constraints:**
- `conversations_pkey`: `PRIMARY KEY (id)`
- `conversations_title_not_blank_chk`: `CHECK (title ~ '[^[:space:]]')`
- `conversations_updated_at_chk`: `CHECK (updated_at >= created_at)`

**Indexes:**
- `conversations_pkey` on `(id)`
- `conversations_updated_at_id_idx` on `(updated_at DESC, id DESC)` — supports deterministic listing ordered by most recent activity.

---

### Table: `chatbot.messages`

| Column | Type | Nullable | Default | Description |
|---|---|---|---|---|
| `id` | `UUID` | `NOT NULL` | `gen_random_uuid()` | Primary key |
| `conversation_id` | `UUID` | `NOT NULL` | — | Foreign key to `chatbot.conversations(id)` |
| `role` | `VARCHAR(20)` | `NOT NULL` | — | Author role (`'user'` or `'assistant'`) |
| `content` | `TEXT` | `NOT NULL` | — | Message body (must contain non-whitespace characters) |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL` | `NOW()` | Creation timestamp |

**Named Constraints:**
- `messages_pkey`: `PRIMARY KEY (id)`
- `messages_conversation_id_fkey`: `FOREIGN KEY (conversation_id) REFERENCES chatbot.conversations(id) ON DELETE CASCADE`
- `messages_role_chk`: `CHECK (role IN ('user', 'assistant'))`
- `messages_content_not_blank_chk`: `CHECK (content ~ '[^[:space:]]')`

**Indexes:**
- `messages_pkey` on `(id)`
- `messages_conversation_id_created_at_id_idx` on `(conversation_id, created_at, id)` — supports deterministic chronological message retrieval (`WHERE conversation_id = $1 ORDER BY created_at ASC, id ASC`) and foreign-key cascade deletes.

---

## 4. Transactional Invariants

1. **Atomic Message Insertion & Monotonic Activity Touch**:
   `MessageRepository.CreateMessage` runs inside a `pgx` transaction:
   - Executes `UPDATE chatbot.conversations SET updated_at = GREATEST(updated_at, clock_timestamp()) WHERE id = $1` (verifies existence, locks the parent row, and monotonically advances `updated_at` even when concurrent transactions acquire the row lock out of `BEGIN` order).
   - Executes `INSERT INTO chatbot.messages (conversation_id, role, content) VALUES ($1, $2, $3) RETURNING ...`.
   - Any constraint violation rolls back both statements atomically.
   - `ConversationRepository.UpdateConversationTitle` likewise uses `SET title = $2, updated_at = GREATEST(updated_at, clock_timestamp())`.
2. **Cascade Deletion**:
   Deleting a row from `chatbot.conversations` automatically removes all child rows in `chatbot.messages` via `ON DELETE CASCADE`.

---

## 5. Environment Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `APP_ENV` | No | `development` | Runtime environment (`development`, `staging`, `production`) |
| `APP_PORT` | No | `8080` | HTTP server listen port |
| `DATABASE_URL` | **Yes** | — | Supabase Session Pooler PostgreSQL connection string (`sslmode=require`) |
| `FRONTEND_ORIGIN` | No | `http://localhost:5173` | Allowed CORS origin |
| `TEST_DATABASE_URL` | For integration tests | — | Dedicated PostgreSQL URL for `-tags=integration` tests |

---

## 6. Running Migrations & Safe Rollback Procedure

Migrations are managed by `backend/cmd/migrate`. They are **never** executed automatically when starting `cmd/api`.

### Apply Pending Migrations (`up`)

```bash
cd backend
go run ./cmd/migrate up
```

### Check Current Migration Version (`version`)

```bash
cd backend
go run ./cmd/migrate version
```

### Target a Different Environment Variable (e.g., `TEST_DATABASE_URL`)

```bash
cd backend
go run ./cmd/migrate -database-env TEST_DATABASE_URL up
```

### Safe Rollback Procedure (`down`)

Rolling back migrations drops tables and permanently deletes data. `cmd/migrate` enforces two safety guards on `down`:
1. `-steps N` (`N >= 1`) is required so a full wipe cannot happen by accident.
2. `-confirm` is required to acknowledge data loss.

Before rolling back in any shared or persistent environment:
1. Take a logical backup of the `chatbot` schema (`pg_dump --schema=chatbot`).
2. Verify current version with `go run ./cmd/migrate version`.
3. Execute a single-step rollback:

```bash
cd backend
go run ./cmd/migrate down -steps 1 -confirm
```

### Dirty State Recovery (`force`)

If a migration fails mid-execution and marks `chatbot.schema_migrations.dirty = true`:
1. Inspect the database state manually.
2. Force the version to the last known clean version:

```bash
cd backend
go run ./cmd/migrate force -version 1 -confirm
```

---

## 7. Integration Testing Instructions

### Unit Tests (No Database Required)

```bash
cd backend
go test -count=1 ./...
go test -race -count=1 ./...
```

### PostgreSQL Integration Tests

Integration tests are gated by the `integration` build tag in `internal/repository/postgres/integration_test.go`.
- They **require** `TEST_DATABASE_URL` and fail immediately if it is unset (never silently skipping).
- They only run idempotent `migrate up` (never `down`) and clean up the specific conversation/message UUIDs created during each test.
- By default, `integration_test.go` refuses to run if `TEST_DATABASE_URL` equals `DATABASE_URL` unless `ALLOW_SHARED_TEST_DB=1` is explicitly set.

```bash
cd backend
TEST_DATABASE_URL="postgres://user:pass@localhost:5432/chatbot_test?sslmode=disable" \
  go test -tags=integration -race -count=1 -v ./internal/repository/postgres/...
```


---

## Migration 000003 — `chatbot.message_attachments`

Added in DOUBLEU AI v2.0 (`backend/migrations/000003_create_message_attachments.up.sql` / `.down.sql`):

- Stores metadata only (`id`, `message_id`, `kind`, `storage_path`, `mime_type`, `width`, `height`, `prompt`, `alt_text`, `created_at`).
- Binary image bytes are stored exclusively in private Supabase Storage, never as Base64 in PostgreSQL.
- `message_id` references `chatbot.messages(id) ON DELETE CASCADE`.
- Indexed on `(message_id, created_at, id)` for fast history hydration.
- `anon` and `authenticated` Supabase Data API roles are revoked from `chatbot.message_attachments`.
