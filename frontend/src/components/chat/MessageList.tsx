import { AssistantMessage } from '@/components/chat/AssistantMessage'
import { UserMessage } from '@/components/chat/UserMessage'
import { EmptyState } from '@/components/common/EmptyState'
import { useAutoScroll } from '@/hooks/useAutoScroll'
import type { Message } from '@/types/chat'

interface MessageListProps {
  messages: Message[]
  isLoading: boolean
  hasActiveConversation: boolean
  onNewChat: () => void
  onSelectPrompt?: (prompt: string) => void
}

export function MessageList({
  messages,
  isLoading,
  hasActiveConversation,
  onNewChat,
  onSelectPrompt,
}: MessageListProps) {
  const lastContent = messages[messages.length - 1]?.content ?? ''
  const { containerRef, handleScroll } = useAutoScroll<HTMLDivElement>(
    `${messages.length}:${lastContent.length}`,
  )

  if (isLoading) {
    return (
      <div
        className="flex flex-1 flex-col gap-6 overflow-y-auto px-4 py-6 sm:px-6"
        aria-label="Loading messages"
      >
        <div className="mx-auto w-full max-w-3xl space-y-6">
          <div className="ml-auto h-10 w-48 animate-pulse rounded-2xl bg-muted" />
          <div className="h-20 w-3/4 animate-pulse rounded-2xl bg-muted" />
          <div className="ml-auto h-10 w-64 animate-pulse rounded-2xl bg-muted" />
        </div>
      </div>
    )
  }

  if (messages.length === 0) {
    return (
      <EmptyState
        onNewChat={!hasActiveConversation ? onNewChat : undefined}
        onSelectPrompt={onSelectPrompt}
        isLoading={isLoading}
      />
    )
  }

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex-1 overflow-y-auto px-4 py-6 sm:px-6"
      role="log"
      aria-label="Chat messages"
    >
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
        {messages.map((message) =>
          message.role === 'user' ? (
            <UserMessage key={message.id} message={message} />
          ) : (
            <AssistantMessage key={message.id} message={message} />
          ),
        )}
      </div>
    </div>
  )
}
