import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

interface MessageComposerProps {
  onSend: (content: string) => Promise<void> | void
  onStop: () => void
  isStreaming: boolean
  disabled?: boolean
}

export function MessageComposer({
  onSend,
  onStop,
  isStreaming,
  disabled = false,
}: MessageComposerProps) {
  const [value, setValue] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const nextHeight = Math.min(el.scrollHeight, 180)
    el.style.height = `${Math.max(nextHeight, 44)}px`
  }, [])

  useEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  const submit = useCallback(() => {
    const trimmed = value.trim()
    if (!trimmed || isStreaming || disabled) return
    setValue('')
    if (textareaRef.current) {
      textareaRef.current.style.height = '44px'
    }
    void onSend(trimmed)
  }, [value, isStreaming, disabled, onSend])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }

  const canSend = value.trim().length > 0 && !isStreaming && !disabled

  return (
    <div className="border-t border-border bg-background px-4 py-3 sm:px-6 sm:py-4">
      <div className="mx-auto flex w-full max-w-3xl items-end gap-2 rounded-xl border border-input bg-card p-2 shadow-xs focus-within:ring-2 focus-within:ring-ring">
        <Textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask DOUBLEU AI anything or describe an image to generate..."
          aria-label="Message input"
          rows={1}
          disabled={disabled}
          className="min-h-[44px] max-h-[180px] resize-none border-0 bg-transparent px-2 py-2.5 shadow-none focus-visible:ring-0"
        />
        {isStreaming ? (
          <Button
            type="button"
            variant="destructive"
            size="icon"
            onClick={onStop}
            aria-label="Stop generating"
            title="Stop generating"
            className="shrink-0 rounded-lg"
          >
            <Square className="h-4 w-4 fill-current" aria-hidden="true" />
          </Button>
        ) : (
          <Button
            type="button"
            variant="default"
            size="icon"
            onClick={submit}
            disabled={!canSend}
            aria-label="Send message"
            title="Send message (Enter)"
            className="shrink-0 rounded-lg"
          >
            <ArrowUp className="h-4 w-4" aria-hidden="true" />
          </Button>
        )}
      </div>
      <p className="mx-auto mt-1.5 max-w-3xl text-center text-[11px] text-muted-foreground">
        Press Enter to send, Shift + Enter for new line
      </p>
    </div>
  )
}
