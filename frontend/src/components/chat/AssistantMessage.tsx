import React from 'react'
import { Bot } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { CodeBlock } from '@/components/chat/CodeBlock'
import { StreamingIndicator } from '@/components/chat/StreamingIndicator'
import type { Message } from '@/types/chat'

interface AssistantMessageProps {
  message: Message
}

export const AssistantMessage = React.memo(function AssistantMessage({
  message,
}: AssistantMessageProps) {
  const isEmptyStreaming = Boolean(message.isStreaming && message.content.trim() === '')

  return (
    <div className="flex w-full items-start gap-3" data-testid="assistant-message">
      <div
        className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted text-foreground"
        aria-hidden="true"
      >
        <Bot className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1 text-sm text-foreground">
        {isEmptyStreaming ? (
          <StreamingIndicator />
        ) : (
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
              {message.content}
            </ReactMarkdown>
            {message.isStreaming && (
              <span className="ml-1 inline-block h-3.5 w-1.5 animate-pulse bg-foreground align-middle" />
            )}
          </div>
        )}
      </div>
    </div>
  )
})
