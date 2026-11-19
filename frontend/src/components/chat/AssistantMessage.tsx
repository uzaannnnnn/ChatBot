import React from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { AttachmentGallery } from '@/components/chat/AttachmentGallery'
import { CodeBlock } from '@/components/chat/CodeBlock'
import { StreamingIndicator } from '@/components/chat/StreamingIndicator'
import { ToolStatusBadge } from '@/components/chat/ToolStatusBadge'
import { BrandMark } from '@/components/common/BrandMark'
import type { Message } from '@/types/chat'

interface AssistantMessageProps {
  message: Message
}

const PSEUDO_TOOL_FALLBACK =
  'Image generation could not be completed because the model did not invoke the image tool properly. Please try again.'

function isPseudoToolPayload(content: string): boolean {
  const trimmed = content.trim()
  if (!trimmed) return false
  const lower = trimmed.toLowerCase()
  if (
    lower.includes('"dalle.text2im"') ||
    (lower.includes('"action_input"') && lower.includes('"dalle'))
  ) {
    return true
  }
  let unwrapped = trimmed
  if (unwrapped.startsWith('```')) {
    const firstNewline = unwrapped.indexOf('\n')
    if (firstNewline !== -1) {
      unwrapped = unwrapped.slice(firstNewline + 1).trim()
    }
    if (unwrapped.endsWith('```')) {
      unwrapped = unwrapped.slice(0, -3).trim()
    }
  }
  if (unwrapped.startsWith('{') && unwrapped.endsWith('}')) {
    try {
      const parsed = JSON.parse(unwrapped) as Record<string, unknown>
      const action = typeof parsed.action === 'string' ? parsed.action.toLowerCase() : ''
      const tool = typeof parsed.tool === 'string' ? parsed.tool.toLowerCase() : ''
      if (
        action.includes('dalle') ||
        action.includes('text2im') ||
        action === 'generate_image' ||
        tool === 'generate_image' ||
        tool.includes('dalle')
      ) {
        return true
      }
    } catch {
      return false
    }
  }
  return false
}

export const AssistantMessage = React.memo(function AssistantMessage({
  message,
}: AssistantMessageProps) {
  const hasAttachments = Boolean(message.attachments && message.attachments.length > 0)
  const hasToolStatuses = Boolean(message.toolStatuses && message.toolStatuses.length > 0)
  const isEmptyStreaming = Boolean(
    message.isStreaming && message.content.trim() === '' && !hasAttachments && !hasToolStatuses,
  )

  const displayContent =
    !hasAttachments && isPseudoToolPayload(message.content) ? PSEUDO_TOOL_FALLBACK : message.content

  return (
    <div className="flex w-full items-start gap-3" data-testid="assistant-message">
      <div
        className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-border bg-primary text-primary-foreground"
        aria-hidden="true"
      >
        <BrandMark className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1 text-sm text-foreground">
        {hasToolStatuses && (
          <div className="flex flex-wrap gap-2">
            {message.toolStatuses!.map((status, idx) => (
              <ToolStatusBadge key={`${status.tool}-${idx}`} status={status} />
            ))}
          </div>
        )}

        {isEmptyStreaming ? (
          <StreamingIndicator />
        ) : (
          <>
            {displayContent.trim() !== '' && (
              <div className="markdown-body break-words">
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={{
                    pre({ children }) {
                      return <>{children}</>
                    },
                    code({ className, children, ...props }) {
                      const match = /language-(\w+)/.exec(className || '')
                      const rawCode = String(children).replace(/\n$/, '')
                      const isBlock = Boolean(match) || rawCode.includes('\n')

                      if (isBlock) {
                        return <CodeBlock language={match?.[1]} code={rawCode} />
                      }

                      return (
                        <code className={className} {...props}>
                          {children}
                        </code>
                      )
                    },
                  }}
                >
                  {displayContent}
                </ReactMarkdown>
                {message.isStreaming && (
                  <span className="ml-1 inline-block h-3.5 w-1.5 animate-pulse bg-foreground align-middle" />
                )}
              </div>
            )}

            {hasAttachments && <AttachmentGallery attachments={message.attachments!} />}
          </>
        )}
      </div>
    </div>
  )
})
