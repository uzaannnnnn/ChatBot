import { API_BASE_URL, getAuthHeaders, parseApiError } from '@/api/client'
import type {
  CreateMessageRequest,
  SSEContentDeltaPayload,
  SSEErrorPayload,
  SSEEvent,
  SSEMessageCompletePayload,
  SSEMessageStartPayload,
} from '@/types/chat'

export class StreamProviderError extends Error {
  public readonly code: string

  constructor(code: string, message: string) {
    super(message)
    this.name = 'StreamProviderError'
    this.code = code
  }
}

/**
 * Parses a single raw SSE frame (lines separated by LF/CRLF) into a typed SSEEvent.
 * Returns null for comment-only or empty frames.
 */
export function parseSSEFrame(rawFrame: string): SSEEvent | null {
  const normalized = rawFrame.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  const lines = normalized.split('\n')

  let eventName = 'message'
  const dataLines: string[] = []

  for (const line of lines) {
    if (!line || line.startsWith(':')) {
      continue
    }

    const colonIdx = line.indexOf(':')
    let field: string
    let value: string

    if (colonIdx === -1) {
      field = line
      value = ''
    } else {
      field = line.slice(0, colonIdx)
      value = line.slice(colonIdx + 1)
      if (value.startsWith(' ')) {
        value = value.slice(1)
      }
    }

    if (field === 'event') {
      eventName = value.trim()
    } else if (field === 'data') {
      dataLines.push(value)
    }
  }

  if (dataLines.length === 0) {
    return null
  }

  const dataStr = dataLines.join('\n')
  if (!dataStr || dataStr === '[DONE]') {
    return null
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(dataStr)
  } catch {
    throw new StreamProviderError('invalid_sse_json', `Malformed SSE JSON payload: ${dataStr}`)
  }

  switch (eventName) {
    case 'message_start':
      return { event: 'message_start', data: parsed as SSEMessageStartPayload }
    case 'content_delta':
      return { event: 'content_delta', data: parsed as SSEContentDeltaPayload }
    case 'message_complete':
      return { event: 'message_complete', data: parsed as SSEMessageCompletePayload }
    case 'error':
      return { event: 'error', data: parsed as SSEErrorPayload }
    default:
      return null
  }
}

/**
 * Incremental stateful SSE parser supporting fragmented UTF-8 chunks,
 * CRLF/LF normalization, incomplete frames, and EOF flushing.
 */
export class SSEParser {
  private buffer = ''
  private readonly decoder = new TextDecoder('utf-8')

  /**
   * Feeds a raw byte chunk into the parser and returns all complete SSE events parsed so far.
   */
  public feed(chunk: Uint8Array): SSEEvent[] {
    const text = this.decoder.decode(chunk, { stream: true })
    return this.feedText(text)
  }

  /**
   * Feeds decoded text into the parser and returns complete SSE events.
   */
  public feedText(text: string): SSEEvent[] {
    const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
    this.buffer += normalized
    return this.drainBuffer(false)
  }

  /**
   * Flushes remaining UTF-8 decoder state and processes any trailing incomplete frame at EOF.
   */
  public flush(): SSEEvent[] {
    const remainingUtf8 = this.decoder.decode()
    if (remainingUtf8) {
      this.buffer += remainingUtf8.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
    }
    return this.drainBuffer(true)
  }

  private drainBuffer(isEOF: boolean): SSEEvent[] {
    const events: SSEEvent[] = []

    while (true) {
      const boundaryIdx = this.buffer.indexOf('\n\n')
      if (boundaryIdx === -1) {
        break
      }

      const rawFrame = this.buffer.slice(0, boundaryIdx)
      this.buffer = this.buffer.slice(boundaryIdx + 2)

      if (rawFrame.trim() !== '') {
        const evt = parseSSEFrame(rawFrame)
        if (evt) {
          events.push(evt)
        }
      }
    }

    if (isEOF && this.buffer.trim() !== '') {
      const evt = parseSSEFrame(this.buffer)
      if (evt) {
        events.push(evt)
      }
      this.buffer = ''
    }

    return events
  }
}

export interface StreamMessageCallbacks {
  onStart?: (payload: SSEMessageStartPayload) => void
  onDelta?: (payload: SSEContentDeltaPayload) => void
  onComplete?: (payload: SSEMessageCompletePayload) => void
  onError?: (payload: SSEErrorPayload) => void
}

export interface StreamMessageOptions {
  conversationId: string
  content: string
  signal?: AbortSignal
  callbacks: StreamMessageCallbacks
}

/**
 * Sends a user message to POST /api/v1/conversations/:id/messages/stream
 * and streams incremental SSE events back to callbacks.
 */
export async function streamMessage({
  conversationId,
  content,
  signal,
  callbacks,
}: StreamMessageOptions): Promise<void> {
  const url = `${API_BASE_URL}/api/v1/conversations/${encodeURIComponent(conversationId)}/messages/stream`
  const payload: CreateMessageRequest = { content }

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      ...getAuthHeaders(),
    },
    body: JSON.stringify(payload),
    signal,
  })

  if (!response.ok) {
    throw await parseApiError(response)
  }

  if (!response.body) {
    throw new StreamProviderError('missing_stream_body', 'Response body stream is unavailable')
  }

  const reader = response.body.getReader()
  const parser = new SSEParser()

  const dispatch = (events: SSEEvent[]) => {
    for (const evt of events) {
      switch (evt.event) {
        case 'message_start':
          callbacks.onStart?.(evt.data)
          break
        case 'content_delta':
          callbacks.onDelta?.(evt.data)
          break
        case 'message_complete':
          callbacks.onComplete?.(evt.data)
          break
        case 'error':
          callbacks.onError?.(evt.data)
          throw new StreamProviderError(evt.data.code, evt.data.message)
      }
    }
  }

  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) {
        dispatch(parser.flush())
        break
      }
      if (value) {
        dispatch(parser.feed(value))
      }
    }
  } finally {
    try {
      await reader.cancel()
    } catch {
      // Ignore reader cancel errors on cleanup
    }
  }
}
