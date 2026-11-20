import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AssistantMessage } from '@/components/chat/AssistantMessage'
import type { Message } from '@/types/chat'

describe('AssistantMessage image generation & tool states', () => {
  it('renders running, completed, and failed tool statuses', () => {
    const runningMsg: Message = {
      id: 'm1',
      conversation_id: 'c1',
      role: 'assistant',
      content: '',
      created_at: new Date().toISOString(),
      isStreaming: true,
      toolStatuses: [{ tool: 'generate_image', status: 'running' }],
    }

    const { rerender } = render(<AssistantMessage message={runningMsg} />)
    expect(screen.getByTestId('tool-status-running')).toHaveTextContent('Generating image...')

    rerender(
      <AssistantMessage
        message={{
          ...runningMsg,
          isStreaming: false,
          content: 'Done!',
          toolStatuses: [{ tool: 'generate_image', status: 'completed' }],
        }}
      />,
    )
    expect(screen.getByTestId('tool-status-completed')).toHaveTextContent('Image generated')

    rerender(
      <AssistantMessage
        message={{
          ...runningMsg,
          isStreaming: false,
          content: 'Image generation is temporarily rate-limited.',
          toolStatuses: [
            {
              tool: 'generate_image',
              status: 'failed',
              message: 'Rate limit exceeded',
            },
          ],
        }}
      />,
    )
    expect(screen.getByTestId('tool-status-failed')).toHaveTextContent('Rate limit exceeded')
  })

  it('renders image attachment with loading skeleton, load transition, and error fallback', () => {
    const msg: Message = {
      id: 'm2',
      conversation_id: 'c1',
      role: 'assistant',
      content: 'Here is your sunset mountain.',
      created_at: new Date().toISOString(),
      attachments: [
        {
          id: 'att-1',
          kind: 'image',
          url: 'https://signed.example/sunset.jpg',
          mime_type: 'image/jpeg',
          alt_text: 'Sunset over alpine peaks',
          width: 1024,
          height: 1024,
        },
      ],
    }

    render(<AssistantMessage message={msg} />)
    expect(screen.getByTestId('attachment-loading')).toBeInTheDocument()

    const img = screen.getByRole('img', { name: 'Sunset over alpine peaks' })
    expect(img).toHaveAttribute('src', 'https://signed.example/sunset.jpg')

    fireEvent.load(img)
    expect(screen.queryByTestId('attachment-loading')).not.toBeInTheDocument()

    fireEvent.error(img)
    expect(screen.getByTestId('attachment-error')).toHaveTextContent(
      'Unable to load generated image',
    )
  })

  it('never renders raw dalle.text2im pseudo-tool JSON as a successful result', () => {
    const msg: Message = {
      id: 'm3',
      conversation_id: 'c1',
      role: 'assistant',
      content: '{"action":"dalle.text2im","action_input":"{\\"prompt\\":\\"a red panda\\"}"}',
      created_at: new Date().toISOString(),
    }

    render(<AssistantMessage message={msg} />)
    expect(screen.queryByText(/dalle\.text2im/i)).not.toBeInTheDocument()
    expect(screen.getByText(/image generation could not be completed/i)).toBeInTheDocument()
  })

  it('strips leaked dalle.text2im + thought JSON while preserving attachment and normal prose', () => {
    const msg: Message = {
      id: 'm4',
      conversation_id: 'c1',
      role: 'assistant',
      content:
        '{"action":"dalle.text2im","action_input":{"prompt":"desk"},"thought":"internal"}\n\nHere is your minimalist studio desk.',
      created_at: new Date().toISOString(),
      attachments: [
        {
          id: 'att-desk',
          kind: 'image',
          url: 'https://signed.example/desk.jpg',
          mime_type: 'image/jpeg',
          alt_text: 'Minimalist sunlit studio desk',
          width: 1024,
          height: 1024,
        },
      ],
    }

    render(<AssistantMessage message={msg} />)
    expect(screen.queryByText(/dalle\.text2im/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/internal/i)).not.toBeInTheDocument()
    expect(screen.getByText('Here is your minimalist studio desk.')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Minimalist sunlit studio desk' })).toBeInTheDocument()
  })

  it('preserves legitimate user-requested JSON examples', () => {
    const msg: Message = {
      id: 'm5',
      conversation_id: 'c1',
      role: 'assistant',
      content: '```json\n{"status":"ok","count":3}\n```',
      created_at: new Date().toISOString(),
    }

    render(<AssistantMessage message={msg} />)
    expect(screen.getByText(/"status":"ok"/)).toBeInTheDocument()
  })

  it('displays full >160-char alt_text in lightbox without line-clamp while keeping compact card caption', () => {
    const fullDescription =
      'A minimalist studio desk bathed in soft, warm morning sunlight. The scene features long, elegant shadows cast across a clean, light-colored wooden desk surface with a ceramic mug and notebook.'
    const msg: Message = {
      id: 'm6',
      conversation_id: 'c1',
      role: 'assistant',
      content: 'Here is your generated image.',
      created_at: new Date().toISOString(),
      attachments: [
        {
          id: 'att-long-alt',
          kind: 'image',
          url: 'https://signed.example/studio-desk.jpg',
          mime_type: 'image/jpeg',
          alt_text: fullDescription,
          width: 1024,
          height: 1024,
        },
      ],
    }

    render(<AssistantMessage message={msg} />)

    // Compact thumbnail card keeps single-line truncate
    const caption = screen.getByText(fullDescription)
    expect(caption.tagName.toLowerCase()).toBe('figcaption')
    expect(caption.className).toContain('truncate')

    // Open lightbox preview -> full description wraps naturally without line-clamp-2
    fireEvent.click(screen.getByRole('button', { name: 'Open full-size preview' }))
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent(fullDescription)
    const descEl = dialog.querySelector('p')
    expect(descEl).not.toBeNull()
    expect(descEl?.className).not.toContain('line-clamp-2')
    expect(descEl?.className).toContain('overflow-y-auto')
  })
})
