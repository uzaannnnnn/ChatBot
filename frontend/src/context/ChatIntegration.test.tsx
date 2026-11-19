import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '@/App'
import type { Conversation, Message } from '@/types/chat'

function sseStreamFromChunks(frames: string[], delayMs = 0): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream<Uint8Array>({
    async start(controller) {
      for (const frame of frames) {
        if (delayMs > 0) {
          await new Promise((r) => setTimeout(r, delayMs))
        }
        controller.enqueue(encoder.encode(frame))
      }
      controller.close()
    },
  })
}

const testUser = { id: '11111111-1111-4111-8111-111111111111', email: 'user@example.com' }

describe('Chat Application Integration', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('creates conversation, streams SSE chunks, reconciles IDs, renames, and deletes', async () => {
    const convs: Conversation[] = []
    const dbMessages: Record<string, Message[]> = {}
    const calledUrls: Array<{ method: string; url: string }> = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = init?.method ?? 'GET'
      calledUrls.push({ method, url })

      if (url.endsWith('/api/v1/conversations?limit=50&offset=0') && method === 'GET') {
        return new Response(
          JSON.stringify({
            data: convs,
            pagination: { limit: 50, offset: 0, has_more: false },
          }),
          { status: 200 },
        )
      }

      if (url.endsWith('/api/v1/conversations') && method === 'POST') {
        const conv: Conversation = {
          id: 'conv-1',
          title: 'New conversation',
          created_at: '2026-11-18T00:00:00Z',
          updated_at: '2026-11-18T00:00:00Z',
        }
        convs.unshift(conv)
        dbMessages[conv.id] = []
        return new Response(JSON.stringify({ data: conv }), { status: 201 })
      }

      if (url.endsWith('/api/v1/conversations/conv-1/messages/stream') && method === 'POST') {
        const body = JSON.parse(String(init?.body)) as { content: string }
        dbMessages['conv-1'].push(
          {
            id: 'persisted-user-1',
            conversation_id: 'conv-1',
            role: 'user',
            content: body.content,
            created_at: '2026-11-18T00:00:01Z',
          },
          {
            id: 'persisted-ast-1',
            conversation_id: 'conv-1',
            role: 'assistant',
            content: 'Hello from AI!',
            created_at: '2026-11-18T00:00:02Z',
          },
        )
        const stream = sseStreamFromChunks([
          'event: message_start\ndata: {"conversation_id":"conv-1","role":"assistant"}\n\n',
          'event: content_delta\ndata: {"delta":"Hello "}\n\n',
          'event: content_delta\ndata: {"delta":"from AI!"}\n\n',
          'event: message_complete\ndata: {"status":"completed","message_id":"persisted-ast-1"}\n\n',
        ])
        return new Response(stream, { status: 200 })
      }

      if (url.includes('/api/v1/conversations/conv-1/messages') && method === 'GET') {
        return new Response(
          JSON.stringify({
            data: dbMessages['conv-1'] ?? [],
            pagination: { limit: 100, offset: 0, has_more: false },
          }),
          { status: 200 },
        )
      }

      if (url.endsWith('/api/v1/conversations/conv-1') && method === 'PATCH') {
        const body = JSON.parse(String(init?.body)) as { title: string }
        convs[0] = { ...convs[0], title: body.title }
        return new Response(JSON.stringify({ data: convs[0] }), { status: 200 })
      }

      if (url.endsWith('/api/v1/conversations/conv-1') && method === 'DELETE') {
        convs.length = 0
        return new Response(null, { status: 204 })
      }

      return new Response('Not found', { status: 404 })
    })

    render(<App initialUser={testUser} initialToken="mock-jwt" />)

    // 1. Create conversation
    const newBtns = await screen.findAllByRole('button', { name: /new conversation/i })
    fireEvent.click(newBtns[0])

    await screen.findByRole('button', { name: /rename new conversation/i })

    // 2. Send message via streaming endpoint
    const input = screen.getByLabelText(/message input/i)
    await waitFor(() => expect(input).not.toBeDisabled())
    fireEvent.change(input, { target: { value: 'Hi Gemini' } })
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: false })

    await waitFor(() => {
      expect(screen.getByText('Hi Gemini')).toBeInTheDocument()
      expect(screen.getByText('Hello from AI!')).toBeInTheDocument()
    })

    // Ensure regular POST /messages was NEVER called
    const regularMsgCalls = calledUrls.filter(
      (c) => c.method === 'POST' && c.url.endsWith('/messages'),
    )
    expect(regularMsgCalls).toHaveLength(0)

    // 3. Rename conversation
    const renameBtn = screen.getByRole('button', { name: /rename new conversation/i })
    fireEvent.click(renameBtn)
    const editInput = screen.getByLabelText(/edit conversation title/i)
    fireEvent.change(editInput, { target: { value: 'Go Chat' } })
    fireEvent.keyDown(editInput, { key: 'Enter' })

    await waitFor(() => {
      expect(screen.getAllByText('Go Chat').length).toBeGreaterThan(0)
    })

    // 4. Delete conversation
    const deleteBtn = screen.getByRole('button', { name: /delete go chat/i })
    fireEvent.click(deleteBtn)

    const confirmDelete = await screen.findByRole('button', { name: /^delete$/i })
    fireEvent.click(confirmDelete)

    await waitFor(() => {
      expect(screen.queryByText('Go Chat')).not.toBeInTheDocument()
    })
  })

  it('cancels active stream on conversation switch and blocks stale chunks', async () => {
    const convs: Conversation[] = [
      {
        id: 'conv-a',
        title: 'Conv A',
        created_at: '2026-11-18T00:00:00Z',
        updated_at: '2026-11-18T00:00:00Z',
      },
      {
        id: 'conv-b',
        title: 'Conv B',
        created_at: '2026-11-18T00:00:00Z',
        updated_at: '2026-11-18T00:00:00Z',
      },
    ]

    let abortedSignal: AbortSignal | undefined

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = init?.method ?? 'GET'

      if (url.endsWith('/api/v1/conversations?limit=50&offset=0')) {
        return new Response(
          JSON.stringify({ data: convs, pagination: { limit: 50, offset: 0, has_more: false } }),
          { status: 200 },
        )
      }

      if (url.includes('/api/v1/conversations/conv-a/messages/stream') && method === 'POST') {
        abortedSignal = init?.signal ?? undefined
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const enc = new TextEncoder()
            controller.enqueue(
              enc.encode(
                'event: message_start\ndata: {"conversation_id":"conv-a","role":"assistant"}\n\n',
              ),
            )
            controller.enqueue(
              enc.encode('event: content_delta\ndata: {"delta":"Slow chunk A "}\n\n'),
            )
            // Keep open until aborted
          },
        })
        return new Response(stream, { status: 200 })
      }

      if (url.includes('/api/v1/conversations/conv-a/messages') && method === 'GET') {
        return new Response(
          JSON.stringify({ data: [], pagination: { limit: 100, offset: 0, has_more: false } }),
          { status: 200 },
        )
      }

      if (url.includes('/api/v1/conversations/conv-b/messages') && method === 'GET') {
        return new Response(
          JSON.stringify({
            data: [
              {
                id: 'b-msg-1',
                conversation_id: 'conv-b',
                role: 'user',
                content: 'Conv B existing message',
                created_at: '2026-11-18T00:00:00Z',
              },
            ],
            pagination: { limit: 100, offset: 0, has_more: false },
          }),
          { status: 200 },
        )
      }

      return new Response('Not found', { status: 404 })
    })

    render(<App initialUser={testUser} initialToken="mock-jwt" />)

    // Select Conv A and start streaming
    const convABtn = await screen.findByText('Conv A')
    fireEvent.click(convABtn)

    const input = screen.getByLabelText(/message input/i)
    await waitFor(() => expect(input).not.toBeDisabled())
    fireEvent.change(input, { target: { value: 'Prompt A' } })
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: false })

    await waitFor(() => {
      expect(screen.getByText('Slow chunk A')).toBeInTheDocument()
    })

    // Switch to Conv B while Conv A is streaming
    fireEvent.click(screen.getByText('Conv B'))

    expect(abortedSignal?.aborted).toBe(true)
    expect(await screen.findByText('Conv B existing message')).toBeInTheDocument()
    expect(screen.queryByText('Slow chunk A')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('displays error banner on genuine stream failure while preserving user message', async () => {
    const convs: Conversation[] = [
      {
        id: 'conv-err',
        title: 'Err Conv',
        created_at: '2026-11-18T00:00:00Z',
        updated_at: '2026-11-18T00:00:00Z',
      },
    ]

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = init?.method ?? 'GET'

      if (url.endsWith('/api/v1/conversations?limit=50&offset=0')) {
        return new Response(
          JSON.stringify({ data: convs, pagination: { limit: 50, offset: 0, has_more: false } }),
          { status: 200 },
        )
      }
      if (url.includes('/api/v1/conversations/conv-err/messages') && method === 'GET') {
        return new Response(
          JSON.stringify({ data: [], pagination: { limit: 100, offset: 0, has_more: false } }),
          { status: 200 },
        )
      }
      if (url.endsWith('/api/v1/conversations/conv-err/messages/stream') && method === 'POST') {
        return new Response(
          JSON.stringify({
            error: { code: 'ai_provider_error', message: 'Upstream failed' },
          }),
          { status: 502 },
        )
      }
      return new Response('Not found', { status: 404 })
    })

    render(<App initialUser={testUser} initialToken="mock-jwt" />)
    fireEvent.click(await screen.findByText('Err Conv'))

    const input = screen.getByLabelText(/message input/i)
    await waitFor(() => expect(input).not.toBeDisabled())
    fireEvent.change(input, { target: { value: 'Fail me' } })
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: false })

    expect(await screen.findByRole('alert')).toHaveTextContent(/AI provider encountered an error/i)
    expect(screen.getByText('Fail me')).toBeInTheDocument()
  })

  it('logs out and clears state on 401 unauthorized response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'Expired token' } }), {
        status: 401,
      }),
    )

    render(<App initialUser={testUser} initialToken="expired-jwt" />)

    expect(
      await screen.findByRole('heading', { name: /sign in to doubleu ai/i }),
    ).toBeInTheDocument()
  })
})
