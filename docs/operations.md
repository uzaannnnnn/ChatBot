# Operations, Migrations, Backup & Deployment Guide

## 1. Remote Supabase Migration Procedure

Migration `000002_add_conversation_ownership` has been verified on disposable PostgreSQL 17. To apply it to remote Supabase (only when authorized):

```bash
cd backend
# Ensure DATABASE_URL in .env points to Supabase Session Pooler (port 5432, sslmode=require)
go run ./cmd/migrate version
go run ./cmd/migrate up
```

To roll back migration `000002` if needed:
```bash
cd backend
go run ./cmd/migrate down 1
```

### Legacy Pre-Auth Data Strategy (`000002_add_conversation_ownership`)

- **Behavior**: Applying `000002` assigns the synthetic nil UUID `00000000-0000-0000-0000-000000000000` to any existing pre-authentication conversations, then drops the column default.
- **Safety**: No real Supabase user has `sub = 00000000-0000-0000-0000-000000000000`, so legacy rows are never exposed to arbitrary authenticated users. They remain intact in PostgreSQL (and rollback `down 1` restores full pre-migration visibility without data loss).
- **Operator Remediation** (choose one after `000002 up` if legacy rows exist):
  - Claim legacy rows for a specific verified Supabase user UUID:
    ```sql
    UPDATE chatbot.conversations
    SET owner_id = '<verified-supabase-user-uuid>'
    WHERE owner_id = '00000000-0000-0000-0000-000000000000';
    ```
  - Or purge legacy unowned MVP test conversations:
    ```sql
    DELETE FROM chatbot.conversations
    WHERE owner_id = '00000000-0000-0000-0000-000000000000';
    ```

## 2. Observability & Health Checks

- **Liveness (`GET /health`)**: Returns `200 OK` without querying PostgreSQL.
- **Readiness (`GET /ready`)**: Executes a 2-second bounded `pgxpool.Ping` and returns `503 Service Unavailable` if PostgreSQL is unreachable.
- **Request Correlation**: `middleware.RequestID` propagates or generates `X-Request-ID` on all responses and includes `request_id` and `user_id` in structured `slog` JSON logs.
- **Provider Visibility**: Startup logs active AI provider (`gemini` or `huggingface`) and model ID without exposing API keys.

## 3. Backup & Recovery (Supabase PostgreSQL)

- **Daily Backups / PITR**: Enable Supabase automated daily backups (or Point-in-Time Recovery on Pro plans) in Supabase Project Settings -> Database -> Backups.
- **Logical Dump (On-Demand)**:
  ```bash
  pg_dump --schema=chatbot --no-owner --no-privileges "$DATABASE_URL" > chatbot_backup_$(date +%Y%m%d).sql
  ```
- **Restore**:
  ```bash
  psql "$DATABASE_URL" < chatbot_backup_YYYYMMDD.sql
  ```

## 4. Production Deployment Steps (Recommended: Fly.io or Render / Railway)

1. Apply database migrations (`go run ./cmd/migrate up`).
2. Deploy **Backend** (`backend/Dockerfile`) with environment variables:
   - `APP_ENV=production`, `APP_PORT=8080`
   - `DATABASE_URL` (Supabase Session Pooler URL)
   - `SUPABASE_URL` (e.g. `https://<project-ref>.supabase.co`)
   - `FRONTEND_ORIGIN` (production frontend HTTPS origin)
   - `AI_PROVIDER=gemini`, `GEMINI_API_KEY`, `GEMINI_MODEL_ID=gemini-3.1-flash-lite`
3. Deploy **Frontend** (`frontend/Dockerfile`) with build args:
   - `VITE_API_BASE_URL` (production backend HTTPS URL, e.g. `https://chatbot-server-dskr.onrender.com`)
   - `VITE_SITE_URL` (optional explicit production frontend HTTPS origin `https://doubleu-ai.eshace.com`; defaults to `window.location.origin`)
   - `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`
4. Configure **Supabase Dashboard → Authentication → URL Configuration**:
   - **Site URL**: `https://doubleu-ai.eshace.com`
   - **Redirect URLs**:
     - `https://doubleu-ai.eshace.com/**`
     - Development only (local testing): `http://localhost:5173/**` (never set localhost as the production Site URL)
5. Verify `GET /health`, `GET /ready`, registration email confirmation redirect, login flow, and SSE streaming in production.
