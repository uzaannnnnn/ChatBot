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

function isPseudoToolObject(raw: string): boolean {
  const trimmed = raw.trim()
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return false
  try {
    const parsed = JSON.parse(trimmed) as Record<string, unknown>
    const action = typeof parsed.action === 'string' ? parsed.action.toLowerCase().trim() : ''
    const tool = typeof parsed.tool === 'string' ? parsed.tool.toLowerCase().trim() : ''
    if (
      action.includes('dalle') ||
      action.includes('text2im') ||
      action === 'generate_image' ||
      tool === 'generate_image' ||
      tool.includes('dalle') ||
      tool.includes('text2im')
    ) {
      return true
    }
    if ('action_input' in parsed && ('thought' in parsed || 'action' in parsed)) {
      return true
    }
    return false
  } catch {
    return false
  }
}

function findBalancedJsonEnd(s: string, start: number): number {
  let depth = 0
  let inString = false
  let escaped = false
  for (let i = start; i < s.length; i++) {
    const ch = s[i]
    if (inString) {
      if (escaped) {
        escaped = false
        continue
      }
      if (ch === '\\') {
        escaped = true
        continue
      }
      if (ch === '"') {
        inString = false
      }
      continue
    }
    if (ch === '"') inString = true
    else if (ch === '{') depth++
    else if (ch === '}') {
      depth--
      if (depth === 0) return i + 1
    }
  }
  return -1
}

function sanitizeDisplayContent(content: string, hasAttachments: boolean): string {
  if (!content.trim()) return content

  let out = ''
  let foundPseudo = false
  let i = 0
  while (i < content.length) {
    if (content.startsWith('```', i)) {
      const nl = content.indexOf('\n', i)
      const closeIdx = nl !== -1 ? content.indexOf('```', nl + 1) : -1
      if (nl !== -1 && closeIdx !== -1) {
        const inner = content.slice(nl + 1, closeIdx).trim()
        if (isPseudoToolObject(inner)) {
          foundPseudo = true
          i = closeIdx + 3
          while (i < content.length && (content[i] === '\r' || content[i] === '\n')) i++
          continue
        }
      }
    }
    if (content[i] === '{') {
      const end = findBalancedJsonEnd(content, i)
      if (end !== -1 && isPseudoToolObject(content.slice(i, end))) {
        foundPseudo = true
        i = end
        continue
      }
    }
    out += content[i]
    i++
  }

  if (!foundPseudo) return content
  const cleaned = out.trim()
  if (cleaned !== '') return cleaned
  return hasAttachments ? '' : PSEUDO_TOOL_FALLBACK
}

export const AssistantMessage = React.memo(function AssistantMessage({
  message,
}: AssistantMessageProps) {
  const hasAttachments = Boolean(message.attachments && message.attachments.length > 0)
  const hasToolStatuses = Boolean(message.toolStatuses && message.toolStatuses.length > 0)
  const isEmptyStreaming = Boolean(
    message.isStreaming && message.content.trim() === '' && !hasAttachments && !hasToolStatuses,
  )

  const displayContent = sanitizeDisplayContent(message.content, hasAttachments)

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
