# Model Selection & Benchmark Report — Hugging Face Inference Providers

## 1. Overview

Sprint 4 introduces AI conversation capabilities to the AI Mini Chatbot via Hugging Face Inference Providers (`https://router.huggingface.co/v1/chat/completions`), utilizing the OpenAI-compatible chat completions API with streaming (`stream: true`).

To determine the production default model, we evaluated models currently routed through the Hugging Face Inference Router catalog.

---

## 2. Hugging Face Inference Router Catalog Survey

Queried the live official models endpoint (`GET https://router.huggingface.co/v1/models`):
- Total routed models: **164**
- All active router endpoints operate under Hugging Face Inference Providers (`is_free: false`), requiring a personal access token (`HF_TOKEN`) with an active payment method or inference credits.
- Top candidate models analyzed for conversational chatbots:

| Model ID | Provider Count | Context Window | Input / Output Price (per M tokens) | Multilingual Quality |
|---|---|---|---|---|
| `meta-llama/Llama-3.3-70B-Instruct` | 11 providers (Together, Fireworks, Cerebras, Sambanova, Novita, etc.) | 128k tokens | $0.10 / $0.32 | SOTA open-weights (Strong Indonesian + English) |
| `meta-llama/Llama-3.1-8B-Instruct` | 5 providers (Together, Sambanova, Cerebras, Novita, etc.) | 128k tokens | $0.02 / $0.04 | Good (Strong English, moderate Indonesian) |
| `Qwen/Qwen2.5-72B-Instruct` | 2 providers (Together, Novita) | 32k tokens | $0.36 / $0.40 | SOTA open-weights (Strong Asian & Multilingual) |

---

## 3. Evaluation & Comparative Analysis

### 3.1 `meta-llama/Llama-3.3-70B-Instruct` (Selected Default)
- **Strengths**:
  - Unmatched provider redundancy on HF router: 11 active backend providers ensuring high uptime and automatic fallback.
  - Large context window: 128,000 tokens (well beyond the configured bounded context of 20 messages).
  - Benchmark performance comparable to GPT-4o and Claude 3.5 Sonnet on conversational, reasoning, and instruction-following tasks.
  - Excellent support for Indonesian syntax, colloquial expressions, and formal grammar.
  - Cost-efficient pricing ($0.10-$0.32/M tokens).
- **Trade-offs**: Slightly higher compute footprint than 8B models, but provider routing achieves sub-500ms first-token latency on high-throughput providers (Cerebras, SambaNova, Together).

### 3.2 `meta-llama/Llama-3.1-8B-Instruct`
- **Strengths**: Extremely low latency, minimal token pricing ($0.02-$0.04/M tokens), 128k context window.
- **Trade-offs**: Noticeably degraded nuance and reasoning in Indonesian dialog, higher tendency to hallucinate formatting under complex multi-turn context.

### 3.3 `Qwen/Qwen2.5-72B-Instruct`
- **Strengths**: Exceptional multilingual capabilities across Asian languages and high technical reasoning.
- **Trade-offs**: Only 2 routed providers on HF router; smaller context window (32k); higher cost ($0.36-$0.40/M tokens).

---

## 4. Benchmark Decision & Default Configuration

**Selected Model**: `meta-llama/Llama-3.3-70B-Instruct`

Default environment variables configured in `internal/config/config.go`:
```env
HF_BASE_URL=https://router.huggingface.co/v1
HF_MODEL_ID=meta-llama/Llama-3.3-70B-Instruct
HF_PROVIDER=auto
AI_MAX_OUTPUT_TOKENS=2048
AI_TEMPERATURE=0.7
AI_TIMEOUT=60s
AI_MAX_CONTEXT_MESSAGES=20
AI_SYSTEM_PROMPT=You are a helpful, respectful, and concise AI assistant.
```

---

## 6. Google Gemini Models Benchmark & Selection

Evaluated 3 stable Gemini models for free-tier live conversational inference:

| Model ID | Stability Status | Free-Tier Status | Stream Latency (First Token / Total) | Discrete SSE Chunks | Benchmark Outcome |
|---|---|---|---|---|---|
| `gemini-3.1-flash-lite` | Stable GA | **Eligible (HTTP 200)** | 1.42s / 1.69s (streamGenerateContent) \| 4.64s / 6.96s (Interactions) | **Yes (multiple)** | **SELECTED (Active Stable Default)** |
| `gemini-3.8-flash` | Stable GA | High Demand / Read Timeout | > 20s timeout | 0 | Blocked by upstream latency |
| `gemini-3.5-flash` | Stable GA | Overloaded (HTTP 503) | 5.81s (Interactions) \| 1.42s (stream) | 0 | Blocked (`service_unavailable` high demand) |

### Benchmark Analysis & Selection
- **`gemini-3.1-flash-lite`** is fully available on free-tier API quotas, produces instant sub-2s responses with clean multi-chunk SSE token streaming, and reliably persists completed assistant messages to PostgreSQL.
- **`gemini-3.8-flash`** and **`gemini-3.5-flash`** experienced upstream capacity saturation and 503 high-demand errors on standard API quotas during testing.
- Configured default: `GEMINI_MODEL_ID=gemini-3.1-flash-lite`.
