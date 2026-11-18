import { useState } from 'react'
import { ConversationItem } from '@/components/conversation/ConversationItem'
import { DeleteConversationDialog } from '@/components/conversation/DeleteConversationDialog'
import type { Conversation } from '@/types/chat'

interface ConversationListProps {
  conversations: Conversation[]
  activeConversationId: string | null
  isLoading: boolean
  onSelect: (id: string) => void
  onRename: (id: string, title: string) => Promise<Conversation>
  onDelete: (id: string) => Promise<void>
}

export function ConversationList({
  conversations,
  activeConversationId,
  isLoading,
  onSelect,
  onRename,
  onDelete,
}: ConversationListProps) {
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null)

  if (isLoading) {
    return (
      <div className="space-y-2 p-2" aria-label="Loading conversations">
        <div className="h-9 animate-pulse rounded-lg bg-muted" />
        <div className="h-9 animate-pulse rounded-lg bg-muted" />
        <div className="h-9 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  if (conversations.length === 0) {
    return (
      <p className="px-3 py-6 text-center text-xs text-muted-foreground">No conversations yet.</p>
    )
  }

  return (
    <>
      <nav aria-label="Conversations" className="flex flex-col gap-1 p-2">
        {conversations.map((conv) => (
          <ConversationItem
            key={conv.id}
            conversation={conv}
            isActive={conv.id === activeConversationId}
            onSelect={onSelect}
            onRename={onRename}
            onRequestDelete={setPendingDelete}
          />
        ))}
      </nav>
      <DeleteConversationDialog
        conversation={pendingDelete}
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        onConfirm={onDelete}
      />
    </>
  )
}
