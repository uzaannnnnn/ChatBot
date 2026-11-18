# Security & Authentication Architecture

## 1. Authentication Flow (Supabase Auth + JWT JWKS)

1. Users sign up or sign in on the frontend via `@supabase/supabase-js` (`VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`).
2. Supabase Auth issues a signed JWT access token (`aud: "authenticated"`, `sub: "<user-uuid>"`, `iss: "${SUPABASE_URL}/auth/v1"`).
3. Frontend attaches `Authorization: Bearer <access_token>` to all `/api/v1/*` REST and SSE requests.
4. Backend `middleware.Auth` verifies every request via `auth.SupabaseVerifier`:
   - Fetches and caches public keys from `${SUPABASE_URL}/auth/v1/.well-known/jwks.json` (supports `ES256`/`ES384`/`ES512` and `RS256`/`RS384`/`RS512`).
   - Validates signature, `exp`, `iss`, `aud`, and UUID-formatted `sub`.
   - Rejects missing, expired, or forged tokens with `401 Unauthorized`.

## 2. Conversation Ownership & Authorization

- Migration `000002_add_conversation_ownership` adds `owner_id UUID NOT NULL` to `chatbot.conversations` with composite index `idx_conversations_owner_updated_at (owner_id, updated_at DESC, id DESC)`.
- User identity (`ownerID`) is derived strictly from verified JWT `sub` claims in `middleware.UserID(c)`. Client-supplied user IDs are never accepted.
- Every repository query (`GetConversationByID`, `ListConversations`, `UpdateConversationTitle`, `DeleteConversation`, `CreateMessage`, `ListMessagesByConversationID`, `ListRecentMessages`) filters by `owner_id = $ownerID`.
- Accessing another user's conversation returns `404 Not Found` (`conversation_not_found`), preventing ID enumeration.

## 3. Hardening Controls

- **Rate Limiting**: Token-bucket rate limiter per authenticated `userID` (or IP) on `/api/v1` (5 req/s, burst 30) and stricter limit on `POST /messages/stream` (0.5 req/s, burst 5) executed before SSE headers commit.
- **Security Headers**: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`.
- **Body Limits**: `1 MiB` max request body via `http.MaxBytesReader`.
- **Secret Redaction**: `Config.String()`, `database.SanitizeError`, `gemini.sanitizeError`, and `huggingface.sanitizeError` scrub `DATABASE_URL`, `GEMINI_API_KEY`, `HF_TOKEN`, and `SUPABASE_JWT_SECRET` from all logs and error responses.
- **Non-Root Containers**: Backend runs as UID `10001` (`appuser`); frontend runs as unprivileged `nginx` user.
