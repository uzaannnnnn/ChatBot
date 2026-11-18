# API Specification — AI Mini Chatbot

## 1. Overview

- **Base URL**: `http://localhost:8080`
- **API Prefix**: `/api/v1`
- **Content-Type**: `application/json; charset=utf-8`
- **Max Request Body Size**: `1 MiB` (`1,048,576` bytes)
- **Timestamps**: RFC 3339 in UTC (`YYYY-MM-DDTHH:MM:SS.ffffffZ`)
- **Authentication**: None (MVP is intended for private/trusted deployment)

---

## 2. Standard Response Envelopes

### Single Resource (`200 OK` / `201 Created`)

```json
{
  "data": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "New conversation",
    "created_at": "2026-11-17T13:00:00Z",
    "updated_at": "2026-11-17T13:00:00Z"
  }
}
```

### Paginated Collection (`200 OK`)

```json
{
  "data": [],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

### Error Envelope (`4xx` / `5xx`)

```json
{
  "error": {
    "code": "conversation_not_found",
    "message": "Conversation not found"
  }
}
```

---

## 3. Pagination Behavior

List endpoints (`GET /api/v1/conversations` and `GET /api/v1/conversations/:id/messages`) accept offset pagination query parameters:

| Parameter | Type | Default | Constraints | Description |
|---|---|---|---|---|
| `limit` | integer | `20` | `1 <= limit <= 100` | Maximum number of items to return |
| `offset` | integer | `0` | `offset >= 0` | Number of items to skip |

- `pagination.has_more` is `true` when at least one additional record exists beyond `offset + limit`.
- `data` is always serialized as a JSON array (`[]`), never `null`.
- Invalid `limit` or `offset` values return `400 Bad Request` with error code `invalid_pagination`.

---

## 4. Error Code Reference

| Error Code | HTTP Status | Description |
|---|---|---|
| `invalid_json` | `400 Bad Request` | Malformed JSON syntax, missing required JSON body, or trailing tokens |
| `invalid_request` | `400 Bad Request` | Unknown/disallowed JSON field (e.g., client-supplied `role`) |
| `invalid_id` | `400 Bad Request` | Path `:id` parameter is not a valid 36-character UUID |
| `invalid_title` | `400 Bad Request` | Title is missing on `PATCH`, blank/whitespace-only, or exceeds 200 characters |
| `invalid_content` | `400 Bad Request` | Message content is missing, empty/whitespace-only, or exceeds 32,000 characters |
| `invalid_pagination` | `400 Bad Request` | `limit` outside `1..100`, negative `offset`, or non-integer query value |
| `conversation_not_found` | `404 Not Found` | Conversation UUID does not exist |
| `route_not_found` | `404 Not Found` | Requested URL path does not match any registered route |
| `method_not_allowed` | `405 Method Not Allowed` | HTTP method is not supported on the matched route |
| `payload_too_large` | `413 Payload Too Large` | Request body exceeds `1 MiB` |
| `ai_unavailable` | `503 Service Unavailable` | AI provider is unconfigured, disabled, or failed to initialize |
| `ai_provider_error` | `502 Bad Gateway` / `503` | AI provider returned an upstream error, rate limit, or failed stream |
| `internal_error` | `500` / `503` | Unexpected internal error or canceled/timed-out request context |

---

## 5. Health & Readiness Endpoints

### `GET /health`
Liveness probe (does not query PostgreSQL).

**Response `200 OK`:**
```json
{
  "status": "ok"
}
```

### `GET /ready`
Readiness probe (verifies PostgreSQL connectivity with a 2-second timeout).

**Response `200 OK`:**
```json
{
  "status": "ready",
  "dependencies": {
    "database": "connected"
  }
}
```

**Response `503 Service Unavailable`:**
```json
{
  "status": "not_ready",
  "dependencies": {
    "database": "unavailable"
  }
}
```

---

## 6. Conversation Endpoints

### 6.1 Create Conversation — `POST /api/v1/conversations`

Creates a new conversation. The request body or `title` field may be omitted or blank, in which case the default title `"New conversation"` is used.

**Request:**
```json
{
  "title": "Project planning"
}
```

**Response `201 Created`:**
```json
{
  "data": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "Project planning",
    "created_at": "2026-11-17T13:00:00Z",
    "updated_at": "2026-11-17T13:00:00Z"
  }
}
```

---

### 6.2 List Conversations — `GET /api/v1/conversations`

Returns conversations ordered deterministically by `updated_at DESC, id DESC`.

**Example Request:**
`GET /api/v1/conversations?limit=20&offset=0`

**Response `200 OK`:**
```json
{
  "data": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "title": "Project planning",
      "created_at": "2026-11-17T13:00:00Z",
      "updated_at": "2026-11-17T13:05:00Z"
    }
  ],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

---

### 6.3 Get Conversation — `GET /api/v1/conversations/:id`

Retrieves a single conversation by UUID.

**Response `200 OK`:**
```json
{
  "data": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "Project planning",
    "created_at": "2026-11-17T13:00:00Z",
    "updated_at": "2026-11-17T13:05:00Z"
  }
}
```

---

### 6.4 Rename Conversation — `PATCH /api/v1/conversations/:id`

Updates the conversation title (1–200 non-blank characters) and advances `updated_at`.

**Request:**
```json
{
  "title": "Updated title"
}
```

**Response `200 OK`:**
```json
{
  "data": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "Updated title",
    "created_at": "2026-11-17T13:00:00Z",
    "updated_at": "2026-11-17T13:10:00Z"
  }
}
```

---

### 6.5 Delete Conversation — `DELETE /api/v1/conversations/:id`

Deletes the conversation and cascades deletion to all associated messages.

**Response `204 No Content`** (empty body).

---

## 7. Message Endpoints

### 7.1 List Messages — `GET /api/v1/conversations/:id/messages`

Retrieves messages for a conversation in chronological order (`created_at ASC, id ASC`). Returns `404 Not Found` if the conversation does not exist, or `200 OK` with `"data": []` if the conversation exists and has no messages.

**Example Request:**
`GET /api/v1/conversations/123e4567-e89b-12d3-a456-426614174000/messages?limit=20&offset=0`

**Response `200 OK`:**
```json
{
  "data": [
    {
      "id": "987e6543-e21b-12d3-a456-426614174111",
      "conversation_id": "123e4567-e89b-12d3-a456-426614174000",
      "role": "user",
      "content": "Hello!",
      "created_at": "2026-11-17T13:05:00Z"
    }
  ],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

---

### 7.2 Create User Message — `POST /api/v1/conversations/:id/messages`

Persists a user message and atomically advances the parent conversation's `updated_at`.
- `role` is always assigned server-side as `"user"`.
- Client payloads containing a `"role"` field are rejected with `400 Bad Request` (`invalid_request`).

**Request:**
```json
{
  "content": "Hello!"
}
```

**Response `201 Created`:**
```json
{
  "data": {
    "id": "987e6543-e21b-12d3-a456-426614174111",
    "conversation_id": "123e4567-e89b-12d3-a456-426614174000",
    "role": "user",
    "content": "Hello!",
    "created_at": "2026-11-17T13:05:00Z"
  }
}
```

---

## 8. AI & Streaming Endpoints

### 8.1 Stream Chat Response — `POST /api/v1/conversations/:id/messages/stream`

Receives a user message, persists it, retrieves recent conversation history, streams AI tokens in real time via Server-Sent Events (SSE), and atomically persists the completed assistant response.

- **Request Headers**:
  - `Content-Type: application/json`
  - `Accept: text/event-stream`
- **Response Headers**:
  - `Content-Type: text/event-stream`
  - `Cache-Control: no-cache`
  - `Connection: keep-alive`
  - `X-Accel-Buffering: no`
- **Request Body**:
  ```json
  {
    "content": "Explain Go channels simply"
  }
  ```
- **Validation Rules**:
  - `content` is required, must not be blank or whitespace-only, and max 32,000 UTF-8 characters.
  - Client-supplied `role` is rejected with `400 Bad Request` (`invalid_request`).
  - Conversation `:id` must be a valid UUID. If not found, returns `404 Not Found`.

#### SSE Event Sequence

1. **`message_start`**: Emitted immediately upon successfully persisting the user message.
   ```
   event: message_start
   data: {"id":"987e6543-e21b-12d3-a456-426614174111","conversation_id":"123e4567-e89b-12d3-a456-426614174000","role":"user","content":"Explain Go channels simply","created_at":"2026-11-17T13:05:00Z"}

   ```

2. **`content_delta`**: Emitted repeatedly for each token chunk streamed from the AI provider.
   ```
   event: content_delta
   data: {"delta":"Go "}

   event: content_delta
   data: {"delta":"channels "}

   event: content_delta
   data: {"delta":"are pipelines..."}

   ```

3. **`message_complete`**: Emitted once generation finishes cleanly and the full assistant message is committed to PostgreSQL.
   ```
   event: message_complete
   data: {"id":"012e3456-e78b-12d3-a456-426614174222","conversation_id":"123e4567-e89b-12d3-a456-426614174000","role":"assistant","content":"Go channels are pipelines...","created_at":"2026-11-17T13:05:05Z"}

   ```

4. **`error`**: Emitted if an error occurs *after* SSE headers have been flushed to the client.
   ```
   event: error
   data: {"code":"ai_provider_error","message":"AI provider error: upstream service unavailable"}

   ```

#### Error Handling & Invariant Guarantees

- **Before Headers Committed**:
  If validation fails, the conversation does not exist, or the AI provider is unconfigured (`HF_TOKEN` empty), the handler writes standard HTTP error JSON envelopes (e.g. `400 Bad Request`, `404 Not Found`, or `503 Service Unavailable`).
- **After Headers Committed**:
  If the AI provider stream fails mid-generation or disconnects, an SSE `error` event is emitted.
- **Persistence Invariants**:
  - The user message is persisted immediately before calling the AI provider.
  - The assistant message is persisted **only** if the full stream completes without error or client cancellation.
  - Partial or aborted generations are never saved to PostgreSQL, preventing corrupt or half-generated messages in history.

---

## 9. Local Development Instructions

```bash
# 1. Configure environment variables
cp backend/.env.example backend/.env
# Edit backend/.env with your Supabase PostgreSQL Session Pooler DATABASE_URL

# 2. Apply database migrations
cd backend
go run ./cmd/migrate up

# 3. Start the API server (listens on :8080 by default)
go run ./cmd/api

# 4. Run unit and handler tests (no database required)
go test -race -count=1 ./...
```
