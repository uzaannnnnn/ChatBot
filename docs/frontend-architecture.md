# Frontend Architecture — AI Mini Chatbot

## 1. Tech Stack

- **Framework**: React 19 + TypeScript 7 + Vite 8
- **Styling & UI Primitives**: Tailwind CSS v4 (`@tailwindcss/vite`) + shadcn/ui (`components.json`, Radix Dialog/Slot, `class-variance-authority`, `lucide-react`)
- **Markdown**: `react-markdown` + `remark-gfm` with custom `CodeBlock` copy-to-clipboard support
- **Linting & Formatting**: Oxlint + Prettier
- **Testing**: Vitest + React Testing Library + Playwright

---

## 2. Directory Structure

```text
frontend/src/
├── api/
│   ├── client.ts         # Fetch wrapper, VITE_API_BASE_URL, ApiError parser
│   ├── conversations.ts  # Conversation CRUD endpoints
│   ├── messages.ts       # Message history retrieval
│   └── sse.ts            # Chunked ReadableStream SSE parser & streamMessage client
├── components/
│   ├── chat/
│   │   ├── AssistantMessage.tsx
│   │   ├── ChatContainer.tsx
│   │   ├── CodeBlock.tsx
│   │   ├── MessageComposer.tsx
│   │   ├── MessageList.tsx
│   │   ├── StreamingIndicator.tsx
│   │   └── UserMessage.tsx
│   ├── common/
│   │   ├── EmptyState.tsx
│   │   └── ErrorState.tsx
│   ├── conversation/
│   │   ├── ConversationItem.tsx
│   │   ├── ConversationList.tsx
│   │   └── DeleteConversationDialog.tsx
│   ├── layout/
│   │   ├── AppLayout.tsx
│   │   ├── ChatHeader.tsx
│   │   └── Sidebar.tsx
│   └── ui/               # shadcn/ui primitives (Button, Dialog, Input, Textarea)
├── context/
│   ├── chat-context.ts   # React Context interface
│   └── ChatContext.tsx   # State provider & stream lifecycle orchestration
├── hooks/
│   ├── useAutoScroll.ts  # Auto-scroll with manual scroll-up override
│   └── useChat.ts
├── types/
│   └── chat.ts           # Typed DTOs matching backend dto.ConversationResponse/MessageResponse
└── utils/
    └── cn.ts             # clsx + tailwind-merge utility
```

---

## 3. API & SSE Streaming Behavior

- **Base URL**: Configured via `VITE_API_BASE_URL` (defaults to `http://localhost:8080`).
- **Single-Submit Persistence**: User messages are sent exclusively to `POST /api/v1/conversations/:id/messages/stream`, which persists the user message, streams assistant tokens, and persists the completed assistant message. The regular `POST /messages` endpoint is never called alongside streaming.
- **SSE Client (`src/api/sse.ts`)**:
  - Uses `fetch()` + `ReadableStreamDefaultReader` (since native `EventSource` does not support `POST` bodies).
  - `SSEParser` uses stateful `TextDecoder('utf-8', { stream: true })` to decode multi-byte UTF-8 characters split across network chunks.
  - Normalizes `\r\n` and `\r` to `\n`, supports multiple `data:` lines per event, holds incomplete trailing frames in buffer until `\n\n`, and flushes remaining buffer at EOF.
  - Parses backend events: `message_start`, `content_delta`, `message_complete`, `error`.
  - Distinguishes `AbortError` (user stop or conversation switch) from genuine upstream/network failures (`StreamProviderError`, `ApiError`).

---

## 4. State Management & Race Protection

- **Stream Generation Guard**: `ChatContext` tracks a monotonic `streamGenerationRef` and `activeConversationIdRef`. Switching conversations or deleting the active conversation aborts the active `AbortController` and increments the generation counter so late chunks from an aborted stream can never mutate the newly selected conversation.
- **Token Batching**: Incoming deltas are coalesced via `requestAnimationFrame` to avoid excessive React re-renders during fast token streams, and flushed synchronously upon `message_complete`.
- **Optimistic ID Reconciliation**: Upon `message_complete`, the optimistic assistant message ID is updated to the persisted `message_id`, followed by a background sync with `GET /api/v1/conversations/:id/messages` and sidebar refresh.

---

## 5. Local Development & Testing

```bash
cd frontend

# 1. Configure environment
cp .env.example .env

# 2. Install dependencies & start Vite dev server
npm install
npm run dev

# 3. Quality gates
npm run lint
npm run format:check
npm test
npm run build
npm run test:e2e
```
