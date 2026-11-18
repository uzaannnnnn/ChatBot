# AI Mini Chatbot

Production-ready full-stack AI Assistant built with **React 19 + TypeScript + Vite + Tailwind CSS + shadcn/ui** and **Go + Gin + pgxpool + Supabase PostgreSQL + Google Gemini**.

## Architecture

- **Frontend (`/frontend`)**: React 19, TypeScript, Tailwind CSS v4, shadcn/ui, Supabase Auth (`@supabase/supabase-js`), custom `fetch()` + `ReadableStream` SSE client, Vitest + RTL, Playwright.
- **Backend (`/backend`)**: Go 1.27, Gin, pgxpool, `golang-migrate`, Supabase JWT JWKS authentication, token-bucket rate limiting, Google Gemini Interactions API (`gemini-3.1-flash-lite`) with optional Hugging Face adapter.
- **Database**: Supabase PostgreSQL 17 (`chatbot` schema with `owner_id` ownership isolation).

See detailed docs in `/docs`:
- [Architecture](docs/architecture.md)
- [Frontend Architecture](docs/frontend-architecture.md)
- [Security & Auth](docs/security.md)
- [API Specification](docs/api-specification.md)
- [Operations & Deployment](docs/operations.md)

## Quick Start (Local Development)

### 1. Backend
```bash
cd backend
cp .env.example .env
# Edit .env with DATABASE_URL, SUPABASE_URL, and GEMINI_API_KEY
go run ./cmd/migrate up
go run ./cmd/api
```

### 2. Frontend
```bash
cd frontend
cp .env.example .env
# Edit .env with VITE_API_BASE_URL, VITE_SUPABASE_URL, VITE_SUPABASE_ANON_KEY
npm install
npm run dev
```

### 3. Docker Compose (Production Simulation)
```bash
docker compose build
docker compose up -d
docker compose ps
```
