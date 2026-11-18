import React, { useState } from 'react'
import { Check, MessageSquare, Pencil, Trash2, X } from 'lucide-react'
import { Input } from '@/components/ui/input'
import type { Conversation } from '@/types/chat'
import { cn } from '@/utils/cn'

interface ConversationItemProps {
  conversation: Conversation
  isActive: boolean
  onSelect: (id: string) => void
  onRename: (id: string, title: string) => Promise<Conversation>
  onRequestDelete: (conversation: Conversation) => void
}

export function ConversationItem({
  conversation,
  isActive,
  onSelect,
  onRename,
  onRequestDelete,
}: ConversationItemProps) {
  const [isEditing, setIsEditing] = useState(false)
  const [draftTitle, setDraftTitle] = useState(conversation.title)
  const [isSaving, setIsSaving] = useState(false)

  const startEditing = (e: React.MouseEvent) => {
    e.stopPropagation()
    setDraftTitle(conversation.title)
    setIsEditing(true)
  }

  const cancelEditing = (e?: React.MouseEvent) => {
    e?.stopPropagation()
    setIsEditing(false)
    setDraftTitle(conversation.title)
  }

  const saveRename = async (e?: React.SyntheticEvent) => {
    e?.stopPropagation()
    const trimmed = draftTitle.trim()
    if (!trimmed || trimmed === conversation.title) {
      setIsEditing(false)
      setDraftTitle(conversation.title)
      return
    }
    setIsSaving(true)
    try {
      await onRename(conversation.id, trimmed)
      setIsEditing(false)
    } finally {
      setIsSaving(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void saveRename()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancelEditing()
    }
  }

  if (isEditing) {
    return (
      <div
        className={cn(
          'flex items-center gap-1 rounded-lg px-2 py-1.5',
          isActive ? 'bg-accent text-accent-foreground' : 'bg-muted/50',
        )}
      >
        <Input
          value={draftTitle}
          onChange={(e) => setDraftTitle(e.target.value)}
          onKeyDown={handleKeyDown}
          aria-label="Edit conversation title"
          maxLength={200}
          autoFocus
          disabled={isSaving}
          className="h-7 px-2 text-xs"
        />
        <button
          type="button"
          onClick={(e) => void saveRename(e)}
          aria-label="Save title"
          disabled={isSaving}
          className="rounded p-1 text-foreground hover:bg-background cursor-pointer"
        >
          <Check className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
        <button
          type="button"
          onClick={cancelEditing}
          aria-label="Cancel rename"
          disabled={isSaving}
          className="rounded p-1 text-muted-foreground hover:bg-background cursor-pointer"
        >
          <X className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    )
  }

  return (
    <div
      className={cn(
        'group flex items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-sm transition-colors',
        isActive
          ? 'bg-accent font-medium text-accent-foreground'
          : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(conversation.id)}
        className="flex min-w-0 flex-1 items-center gap-2.5 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded cursor-pointer"
      >
        <MessageSquare className="h-4 w-4 shrink-0 opacity-70" aria-hidden="true" />
        <span className="truncate">{conversation.title}</span>
      </button>

      <div
        className={cn(
          'flex items-center gap-0.5 transition-opacity',
          isActive
            ? 'opacity-100'
            : 'opacity-100 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-within:opacity-100',
        )}
      >
        <button
          type="button"
          onClick={startEditing}
          aria-label={`Rename ${conversation.title}`}
          title="Rename conversation"
          className="rounded p-1 text-muted-foreground hover:bg-background hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer"
        >
          <Pencil className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onRequestDelete(conversation)
          }}
          aria-label={`Delete ${conversation.title}`}
          title="Delete conversation"
          className="rounded p-1 text-muted-foreground hover:bg-background hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer"
        >
          <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    </div>
  )
}
