import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MessageComposer } from '@/components/chat/MessageComposer'

describe('MessageComposer', () => {
  it('disables send button when empty and sends on Enter', () => {
    const onSend = vi.fn()
    const onStop = vi.fn()
    render(<MessageComposer onSend={onSend} onStop={onStop} isStreaming={false} />)

    const sendBtn = screen.getByRole('button', { name: /send message/i })
    expect(sendBtn).toBeDisabled()

    const textarea = screen.getByLabelText(/message input/i)
    fireEvent.change(textarea, { target: { value: 'Hello world' } })
    expect(sendBtn).not.toBeDisabled()

    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })
    expect(onSend).toHaveBeenCalledWith('Hello world')
    expect(textarea).toHaveValue('')
  })

  it('inserts newline on Shift+Enter without sending', () => {
    const onSend = vi.fn()
    render(<MessageComposer onSend={onSend} onStop={vi.fn()} isStreaming={false} />)

    const textarea = screen.getByLabelText(/message input/i)
    fireEvent.change(textarea, { target: { value: 'Line 1' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })

    expect(onSend).not.toHaveBeenCalled()
  })

  it('shows stop button while streaming and calls onStop', () => {
    const onStop = vi.fn()
    render(<MessageComposer onSend={vi.fn()} onStop={onStop} isStreaming={true} />)

    const stopBtn = screen.getByRole('button', { name: /stop generating/i })
    fireEvent.click(stopBtn)
    expect(onStop).toHaveBeenCalledTimes(1)
  })
})
