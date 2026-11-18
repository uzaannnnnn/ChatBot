import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AssistantMessage } from '@/components/chat/AssistantMessage'
import { UserMessage } from '@/components/chat/UserMessage'
import type { Message } from '@/types/chat'

describe('Message Rendering', () => {
  it('renders user message text faithfully', () => {
    const msg: Message = {
      id: 'u1',
      conversation_id: 'c1',
      role: 'user',
      content: 'Hello assistant!',
      created_at: '2026-11-18T00:00:00Z',
    }
    render(<UserMessage message={msg} />)
    expect(screen.getByText('Hello assistant!')).toBeInTheDocument()
  })

  it('renders markdown, code blocks, and copy button for assistant messages', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, {
      clipboard: { writeText },
    })

    const msg: Message = {
      id: 'a1',
      conversation_id: 'c1',
      role: 'assistant',
      content: 'Here is Go code:\n\n```go\nfmt.Println("hi")\n```',
      created_at: '2026-11-18T00:00:01Z',
    }
    render(<AssistantMessage message={msg} />)

    expect(screen.getByText('Here is Go code:')).toBeInTheDocument()
    expect(screen.getByText('go')).toBeInTheDocument()
    expect(screen.getByText('fmt.Println("hi")')).toBeInTheDocument()

    const copyBtn = screen.getByRole('button', { name: /copy code/i })
    fireEvent.click(copyBtn)
    expect(writeText).toHaveBeenCalledWith('fmt.Println("hi")')
  })

  it('renders streaming indicator when assistant message has no content yet', () => {
    const msg: Message = {
      id: 'a2',
      conversation_id: 'c1',
      role: 'assistant',
      content: '',
      created_at: '2026-11-18T00:00:02Z',
      isStreaming: true,
    }
    render(<AssistantMessage message={msg} />)
    expect(screen.getByRole('status', { name: /generating response/i })).toBeInTheDocument()
  })
})
