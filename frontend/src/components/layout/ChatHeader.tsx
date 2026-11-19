import { useState } from 'react'
import { Menu, Plus, Trash2 } from 'lucide-react'
import { DeleteConversationDialog } from '@/components/conversation/DeleteConversationDialog'
import { Button } from '@/components/ui/button'
import { useChat } from '@/hooks/useChat'

interface ChatHeaderProps {
  onOpenMobileSidebar: () => void
}

export function ChatHeader({ onOpenMobileSidebar }: ChatHeaderProps) {
  const { activeConversation, createConversation, deleteConversation } = useChat()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-border bg-background px-4 sm:px-6">
      <div className="flex min-w-0 items-center gap-3">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onOpenMobileSidebar}
          aria-label="Open sidebar"
          className="md:hidden"
        >
          <Menu className="h-5 w-5" aria-hidden="true" />
        </Button>
        <h1 className="truncate text-sm font-semibold text-foreground sm:text-base">
          {activeConversation ? activeConversation.title : 'DOUBLEU AI'}
        </h1>
      </div>

      <div className="flex items-center gap-1.5">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={() => void createConversation()}
          aria-label="New conversation"
          title="New conversation"
          className="md:hidden"
        >
          <Plus className="h-4 w-4" aria-hidden="true" />
        </Button>
        {activeConversation && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => setDeleteDialogOpen(true)}
            aria-label="Delete active conversation"
            title="Delete conversation"
            className="text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" aria-hidden="true" />
          </Button>
        )}
      </div>

      <DeleteConversationDialog
        conversation={activeConversation}
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        onConfirm={deleteConversation}
      />
    </header>
  )
}
