# Changelog

All notable changes to DOUBLEU AI are documented in this file.

## [2.0.0] - 2026-11-19

### Added
- **Product Rebranding**: Evolved from AI Mini Chatbot to **DOUBLEU AI** (*Think More. Discover More.*) across document metadata, custom SVG brand mark & favicon, auth screen, sidebar, header, welcome workspace with starter prompts, and documentation.
- **AI Orchestration & Tool Registry**:
  - Extended `internal/ai` and Gemini Interactions API client with structured `tools`, `previous_interaction_id`, `function_result`, and streaming `function_call` parsing.
  - Added backend-controlled Tool Registry (`internal/ai/tools`) with timeout enforcement, per-user rate limiting, max 1 image execution per request, usage/failure counters, and strict JSON validation.
  - Added pseudo-tool JSON guard (`dalle.text2im`) across backend and frontend so raw pseudo-tool JSON is never rendered as a successful image generation.
- **Native Image Generation**:
  - Added Cloudflare Workers AI image provider (`internal/ai/cloudflare`) targeting `@cf/black-forest-labs/flux-1-schnell` with Base64 JSON + binary decoding, magic-byte & MIME validation (`image/jpeg`, `image/png`, `image/webp`), dimension parsing, HTTP 400/401/403/429/5xx error mapping, and credential redaction.
- **Private Storage & Attachments Schema**:
  - Added migration `000003_create_message_attachments` (`chatbot.message_attachments` table with cascade delete and role revocations).
  - Added Supabase Storage REST client (`internal/storage`) for private bucket uploads, short-lived signed URLs, orphan cleanup on failure, and object cleanup when deleting conversations.
- **Additive SSE Contract & Frontend Image UI**:
  - Added `tool_status` and `attachment` SSE events (no Base64 over SSE).
  - Added `ToolStatusBadge` and `AttachmentGallery` components with loading skeletons, responsive previews, Radix Dialog lightbox, download action, and error fallback.
- **Testing**:
  - Added backend unit & race tests for Cloudflare provider, Gemini tool calling, Tool Registry, Supabase Storage, ChatService tool loop/orphan cleanup, and SSE event ordering.
  - Added frontend Vitest and Playwright E2E tests covering full image generation, reload persistence, and quota exhaustion handling.
