import { describe, expect, it, vi } from 'vitest'
import { SSEParser, StreamProviderError, parseSSEFrame, streamMessage } from '@/api/sse'

describe('SSEParser and parseSSEFrame', () => {
  it('parses CRLF and LF line endings cleanly', () => {
    const crlfFrame = 'event: content_delta\r\ndata: {"delta":"Hello"}\r\n'
    const lfFrame = 'event: content_delta\ndata: {"delta":" World"}\n'

    expect(parseSSEFrame(crlfFrame)).toEqual({
      event: 'content_delta',
      data: { delta: 'Hello' },
    })
    expect(parseSSEFrame(lfFrame)).toEqual({
      event: 'content_delta',
      data: { delta: ' World' },
    })
  })

  it('handles fragmented UTF-8 multi-byte chunks across byte boundaries', () => {
    const parser = new SSEParser()
    const payload = 'event: content_delta\ndata: {"delta":"Halo 🚀 dunia!"}\n\n'
    const bytes = new TextEncoder().encode(payload)

    // Split directly inside the 4-byte emoji UTF-8 sequence
    const rocketIdx = bytes.indexOf(0xf0)
    expect(rocketIdx).toBeGreaterThan(-1)

    const part1 = bytes.slice(0, rocketIdx + 2)
    const part2 = bytes.slice(rocketIdx + 2)

    expect(parser.feed(part1)).toEqual([])
    const events = parser.feed(part2)
    expect(events).toEqual([
      {
        event: 'content_delta',
        data: { delta: 'Halo 🚀 dunia!' },
      },
    ])
  })

  it('handles multiple data lines joined with newline and comments', () => {
    const frame = [
      ': ping keepalive',
      'event: content_delta',
      'data: {"delta":',
      'data: "split json"}',
    ].join('\n')

    expect(parseSSEFrame(frame)).toEqual({
      event: 'content_delta',
      data: { delta: 'split json' },
    })
  })

  it('retains incomplete frames until delimiter arrives and flushes remaining buffer at EOF', () => {
    const parser = new SSEParser()

    expect(
      parser.feedText('event: message_start\ndata: {"conversation_id":"c1","role":"assistant"}'),
    ).toEqual([])
    expect(
      parser.feedText(
        '\n\nevent: message_complete\ndata: {"status":"completed","message_id":"m1"}',
      ),
    ).toEqual([
      {
        event: 'message_start',
        data: { conversation_id: 'c1', role: 'assistant' },
      },
    ])

    // Flush trailing frame without final \n\n at EOF
    expect(parser.flush()).toEqual([
      {
        event: 'message_complete',
        data: { status: 'completed', message_id: 'm1' },
      },
    ])
  })
})

describe('streamMessage', () => {
  it('streams chunks from ReadableStream and handles completion', async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'event: message_start\ndata: {"conversation_id":"c1","role":"assistant"}\n\n',
          ),
        )
        controller.enqueue(encoder.encode('event: content_delta\ndata: {"delta":"Hi"}\n\n'))
        controller.enqueue(encoder.encode('event: content_delta\ndata: {"delta":" there"}\n\n'))
        controller.enqueue(
          encoder.encode(
            'event: message_complete\ndata: {"status":"completed","message_id":"m99"}\n\n',
          ),
        )
        controller.close()
      },
    })

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(stream, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    )

    const events: string[] = []
    let completeId = ''

    await streamMessage({
      conversationId: 'c1',
      content: 'Hello',
      callbacks: {
        onStart: (p) => events.push(`start:${p.conversation_id}`),
        onDelta: (p) => events.push(`delta:${p.delta}`),
        onComplete: (p) => {
          completeId = p.message_id
          events.push(`complete:${p.message_id}`)
        },
      },
    })

    expect(events).toEqual(['start:c1', 'delta:Hi', 'delta: there', 'complete:m99'])
    expect(completeId).toBe('m99')
    fetchSpy.mockRestore()
  })

  it('throws StreamProviderError when SSE error event arrives', async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'event: error\ndata: {"code":"ai_provider_error","message":"Upstream down"}\n\n',
          ),
        )
        controller.close()
      },
    })

    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(stream, { status: 200 }))

    await expect(
      streamMessage({
        conversationId: 'c1',
        content: 'Hello',
        callbacks: {},
      }),
    ).rejects.toThrow(StreamProviderError)

    fetchSpy.mockRestore()
  })

  it('parses tool_status and attachment events and ignores unknown events safely', async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'event: message_start\ndata: {"conversation_id":"c1","role":"assistant"}\n\n',
          ),
        )
        controller.enqueue(
          encoder.encode(
            'event: tool_status\ndata: {"tool":"generate_image","status":"running"}\n\n',
          ),
        )
        controller.enqueue(encoder.encode('event: future_unknown_event\ndata: {"foo":"bar"}\n\n'))
        controller.enqueue(
          encoder.encode(
            'event: tool_status\ndata: {"tool":"generate_image","status":"completed"}\n\n',
          ),
        )
        controller.enqueue(
          encoder.encode(
            'event: attachment\ndata: {"id":"att-1","kind":"image","url":"https://signed.example/img.jpg","mime_type":"image/jpeg","alt_text":"Neon city","width":1024,"height":1024}\n\n',
          ),
        )
        controller.enqueue(
          encoder.encode('event: content_delta\ndata: {"delta":"Here is your neon city."}\n\n'),
        )
        controller.enqueue(
          encoder.encode(
            'event: message_complete\ndata: {"status":"completed","message_id":"m-img"}\n\n',
          ),
        )
        controller.close()
      },
    })

    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(stream, { status: 200 }))

    const log: string[] = []
    await streamMessage({
      conversationId: 'c1',
      content: 'Draw a neon city',
      callbacks: {
        onStart: () => log.push('start'),
        onToolStatus: (s) => log.push(`tool:${s.tool}:${s.status}`),
        onAttachment: (a) => log.push(`att:${a.id}:${a.url}`),
        onDelta: (d) => log.push(`delta:${d.delta}`),
        onComplete: (c) => log.push(`complete:${c.message_id}`),
      },
    })

    expect(log).toEqual([
      'start',
      'tool:generate_image:running',
      'tool:generate_image:completed',
      'att:att-1:https://signed.example/img.jpg',
      'delta:Here is your neon city.',
      'complete:m-img',
    ])
    fetchSpy.mockRestore()
  })
})
