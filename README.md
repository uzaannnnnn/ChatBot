# AI Mini Chatbot

Full-stack AI Assistant web application built with **React 19 + TypeScript + Vite + Tailwind CSS + shadcn/ui** and **Go + Gin + pgxpool + Supabase PostgreSQL + Google Gemini**.

## Features

- **Supabase Authentication**: Email/password sign-up, sign-in, sign-out, session persistence, and automatic session refresh.
- **Multi-User Ownership Isolation**: Every conversation and message is scoped to its authenticated `owner_id`; unauthorized requests return `401` and cross-user access returns `404`.
- **Real-Time SSE Streaming**: Incremental assistant responses via `fetch()` + `ReadableStream` Server-Sent Events with mid-stream cancellation and optimistic message ID reconciliation.
- **Conversation Management**: Create, list, rename, and delete conversations from a responsive desktop/mobile sidebar.
- **Rich Message Rendering**: Markdown + GitHub Flavored Markdown (`remark-gfm`) with formatted code blocks and copy support.

## Tech Stack & Architecture

- **Frontend (`/frontend`)**: React 19, TypeScript, Vite 8, Tailwind CSS v4, shadcn/ui (Radix UI), `@supabase/supabase-js`, `react-markdown`, `remark-gfm`, Vitest + React Testing Library, Oxlint, Prettier, Playwright.
- **Backend (`/backend`)**: Go 1.27 (`go 1.27.3`), Gin, `pgx/v5` (`pgxpool`), `golang-migrate`, JWKS-based Supabase JWT verification (`golang-jwt/v5`), token-bucket rate limiting (`golang.org/x/time/rate`), Google Gemini Interactions API (`gemini-3.1-flash-lite`) with an optional Hugging Face Inference adapter.
- **Database**: Supabase PostgreSQL 17 (`chatbot` schema: `chatbot.conversations` and `chatbot.messages`).

Further architecture and operational documentation lives in `/docs`:
- [Architecture](docs/architecture.md)
- [Frontend Architecture](docs/frontend-architecture.md)
- [Database Design](docs/database-design.md)
- [API Specification](docs/api-specification.md)
- [Security & Auth](docs/security.md)
- [Operations & Deployment](docs/operations.md)
- [Gemini Model Benchmark](docs/model-benchmark.md)

## Prerequisites

- **Go** `1.27+`
- **Node.js** `22+` and `npm`
- **Supabase project** (PostgreSQL 17 + Supabase Auth) or local PostgreSQL 17 for integration tests
- **Google Gemini API key** (or Hugging Face token when `AI_PROVIDER=huggingface`)
- **Docker & Docker Compose** (optional, for container builds)

## Installation & Local Development Setup

### 1. Backend

```bash
cd backend
cp .env.example .env
# Edit backend/.env with your DATABASE_URL, SUPABASE_URL, and GEMINI_API_KEY
go mod download
go run ./cmd/migrate up
go run ./cmd/api
```

The backend API listens on `http://localhost:8080` by default (`APP_PORT=8080`).

### 2. Frontend

```bash
cd frontend
cp .env.example .env
# Edit frontend/.env with VITE_API_BASE_URL, VITE_SUPABASE_URL, and VITE_SUPABASE_ANON_KEY
npm install
npm run dev
```

The Vite dev server runs on `http://localhost:5173`. Use `http://localhost:5173` (matching `FRONTEND_ORIGIN`) so CORS allows API requests.

## Environment Variables

Do **not** commit real `.env` files. Only `.env.example` templates are tracked.

### Backend (`backend/.env.example`)

| Variable | Description | Example / Placeholder |
| --- | --- | --- |
| `APP_ENV` | Runtime environment (`development` \| `staging` \| `production`) | `development` |
| `APP_PORT` | HTTP server listen port | `8080` |
| `DATABASE_URL` | Supabase PostgreSQL connection string (`sslmode=require`) | `postgresql://postgres.project_ref:your_password@aws-0-region.pooler.supabase.com:5432/postgres?sslmode=require` |
| `SUPABASE_URL` | Supabase project URL for JWKS JWT verification | `https://your-project-ref.supabase.co` |
| `SUPABASE_JWKS_URL` | Optional explicit JWKS URL override | `${SUPABASE_URL}/auth/v1/.well-known/jwks.json` |
| `SUPABASE_JWT_SECRET` | Optional legacy HS256 secret (JWKS preferred) | *(optional)* |
| `FRONTEND_ORIGIN` | Allowed CORS origin | `http://localhost:5173` |
| `TEST_DATABASE_URL` | Optional PostgreSQL URL for `-tags=integration` tests | `postgresql://postgres:postgres@localhost:5432/chatbot_test?sslmode=disable` |
| `AI_PROVIDER` | Active AI provider (`gemini` \| `huggingface`) | `gemini` |
| `GEMINI_API_KEY` | Google AI Studio API key | `your_gemini_api_key_here` |
| `GEMINI_MODEL_ID` | Gemini model ID | `gemini-3.1-flash-lite` |
| `GEMINI_BASE_URL` | Gemini API base URL | `https://generativelanguage.googleapis.com/v1beta` |
| `AI_MAX_OUTPUT_TOKENS` | Max tokens per response | `2048` |
| `AI_TEMPERATURE` | Sampling temperature | `0.7` |
| `AI_TIMEOUT` | AI request timeout | `60s` |
| `AI_MAX_CONTEXT_MESSAGES` | Max conversation history messages sent to provider | `20` |
| `AI_SYSTEM_PROMPT` | System instruction for the assistant | `You are a helpful, respectful, and concise AI assistant.` |
| `HF_TOKEN` | Optional Hugging Face token (`AI_PROVIDER=huggingface`) | `hf_your_token_here` |
| `HF_MODEL_ID` | Optional Hugging Face model ID | `meta-llama/Llama-3.3-70B-Instruct` |
| `HF_BASE_URL` | Optional Hugging Face router base URL | `https://router.huggingface.co/v1` |
| `HF_PROVIDER` | Optional Hugging Face provider routing | `auto` |

### Frontend (`frontend/.env.example`)

| Variable | Description | Example / Placeholder |
| --- | --- | --- |
| `VITE_API_BASE_URL` | Backend base URL (no trailing slash) | `http://localhost:8080` |
| `VITE_SUPABASE_URL` | Supabase project URL | `https://your-project-ref.supabase.co` |
| `VITE_SUPABASE_ANON_KEY` | Supabase public anon/publishable key (safe for browser bundle) | `your_supabase_anon_key_here` |

## Supabase Setup & Database Migrations

1. **Supabase Auth**: Enable Email/Password authentication in the Supabase Dashboard. Copy your Project URL and public **anon** key into `frontend/.env`, and your Project URL + pooler `DATABASE_URL` into `backend/.env`. Never expose `service_role` keys to Vite or client code.
2. **Embedded SQL Migrations**: Migrations are embedded in `backend/migrations` and executed via `backend/cmd/migrate`:
   - `000001_create_chatbot_schema`: Creates the `chatbot` schema, `chatbot.conversations`, and `chatbot.messages` tables.
   - `000002_add_conversation_ownership`: Adds `owner_id UUID NOT NULL` to `chatbot.conversations` and indexes `(owner_id, updated_at DESC, id DESC)`. Existing legacy rows created prior to authentication are assigned the sentinel nil UUID `'00000000-0000-0000-0000-000000000000'` (keeping them inaccessible to real users rather than assigning them to an arbitrary account), and the column default is immediately dropped afterward.

```bash
cd backend
go run ./cmd/migrate up
go run ./cmd/migrate version
# Rollback if needed:
go run ./cmd/migrate down
```

## Testing

### Backend Unit Tests (No DB Required)

```bash
cd backend
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
```

### Backend PostgreSQL Integration Tests (Requires Running PostgreSQL)

```bash
cd backend
TEST_DATABASE_URL="postgresql://postgres:postgres@localhost:5432/chatbot_test?sslmode=disable" \
  go test -tags=integration -race -count=1 -v ./internal/repository/postgres/
```

### Frontend Unit Tests, Lint, Format & Build (No Live Backend Required)

```bash
cd frontend
npm run format:check
npm run lint
npm test
npm run build
```

### Frontend Playwright E2E Tests

Playwright E2E tests in `frontend/e2e/chat.spec.ts` run against the Vite preview/dev server (`npm run test:e2e`):

```bash
cd frontend
npm run test:e2e
```

## Docker & Docker Compose

`docker-compose.yml` builds production-style backend (`backend/Dockerfile`) and Nginx-served frontend (`frontend/Dockerfile`) containers.

```bash
# Ensure backend/.env exists and frontend build args are exported or passed
docker compose build
docker compose up -d
docker compose ps
```

Health checks:
- Backend container: `GET http://127.0.0.1:8080/health`
- Frontend container: `GET http://127.0.0.1:8080/healthz` (mapped to host `FRONTEND_PORT`, default `5173`)

## API Endpoint Reference

### Public Health Endpoints

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness check (`200 OK`) |
| `GET` | `/ready` | Readiness check verifying PostgreSQL connectivity |

### Authenticated Endpoints (`/api/v1/*`)

All `/api/v1/*` routes require `Authorization: Bearer <supabase_access_token>`.

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/v1/conversations` | Create a new conversation |
| `GET` | `/api/v1/conversations` | List authenticated user's conversations |
| `GET` | `/api/v1/conversations/:id` | Get a single owned conversation |
| `PATCH` | `/api/v1/conversations/:id` | Rename an owned conversation |
| `DELETE` | `/api/v1/conversations/:id` | Delete an owned conversation |
| `GET` | `/api/v1/conversations/:id/messages` | List messages in an owned conversation |
| `POST` | `/api/v1/conversations/:id/messages` | Send message and persist non-streaming reply |
| `POST` | `/api/v1/conversations/:id/messages/stream` | Send message, stream assistant tokens over SSE, and persist on completion |

## Authentication & Security Notes

- **JWKS Verification**: Backend verifies Supabase JWTs asymmetrically via JWKS (`kid` lookup, signature, issuer, audience, expiration, and subject UUID validation).
- **Ownership Isolation**: Repository queries filter by `(id, owner_id)`; missing or non-owned conversations return `404` so resource existence is not leaked across users.
- **Rate Limiting & Limits**: General `/api/v1/*` routes are limited to `5 req/s` (burst `30`), and `POST /messages/stream` is additionally limited to `0.5 req/s` (burst `5`). Request bodies are capped via `MaxBodySize`.
- **Browser Key Hygiene**: Only `VITE_SUPABASE_URL` and `VITE_SUPABASE_ANON_KEY` are exposed to the frontend bundle.

## Known Limitations & Deployment Notes

- **In-Memory Rate Limiter**: Rate limiting uses per-process token buckets; multi-instance horizontal scaling would require a shared store (e.g., Redis).
- **Conversation Selection on Reload**: Reloading the browser preserves the authenticated session and sidebar conversation list, but starts on the empty chat view until the user selects a conversation from the sidebar.
- **Deployment Status**: No public cloud deployment has been performed. `.github/workflows/deploy.yml` is a manual (`workflow_dispatch`) Docker build verification template (`push: false`) until target registry and hosting provider steps are configured.
