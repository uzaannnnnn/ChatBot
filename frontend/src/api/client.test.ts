import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/client'
import {
  createConversation,
  deleteConversation,
  listConversations,
  updateConversationTitle,
} from '@/api/conversations'
import { listMessages } from '@/api/messages'

describe('API client & endpoints', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('creates and lists conversations using typed DTO envelopes', async () => {
    const conv = {
      id: 'c1',
      title: 'Test Chat',
      created_at: '2026-11-18T00:00:00Z',
      updated_at: '2026-11-18T00:00:00Z',
    }

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ data: conv }), { status: 201 }),
    )

    const created = await createConversation({ title: 'Test Chat' })
    expect(created).toEqual(conv)

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          data: [conv],
          pagination: { limit: 20, offset: 0, has_more: false },
        }),
        { status: 200 },
      ),
    )

    const listed = await listConversations({ limit: 20, offset: 0 })
    expect(listed.data).toHaveLength(1)
    expect(listed.pagination.has_more).toBe(false)
  })

  it('renames and deletes conversations cleanly', async () => {
    const updated = {
      id: 'c1',
      title: 'Renamed',
      created_at: '2026-11-18T00:00:00Z',
      updated_at: '2026-11-18T00:05:00Z',
    }

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ data: updated }), { status: 200 }),
    )
    expect(await updateConversationTitle('c1', 'Renamed')).toEqual(updated)

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(deleteConversation('c1')).resolves.toBeUndefined()
  })

  it('fetches message history and parses structured API errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          data: [],
          pagination: { limit: 50, offset: 0, has_more: false },
        }),
        { status: 200 },
      ),
    )

    const msgs = await listMessages('c1')
    expect(msgs.data).toEqual([])

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: { code: 'conversation_not_found', message: 'Conversation not found' },
        }),
        { status: 404 },
      ),
    )

    await expect(listMessages('missing')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      code: 'conversation_not_found',
    } satisfies Partial<ApiError>)
  })
})
