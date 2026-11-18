import { MessageSquarePlus, Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface EmptyStateProps {
  onNewChat?: () => void
  isLoading?: boolean
}

export function EmptyState({ onNewChat, isLoading }: EmptyStateProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-12 text-center">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl border border-border bg-muted">
        <Sparkles className="h-6 w-6 text-foreground" aria-hidden="true" />
      </div>
      <h2 className="text-xl font-semibold tracking-tight text-foreground">
        How can I help you today?
      </h2>
      <p className="mt-2 max-w-sm text-sm text-muted-foreground">
        Start a new conversation or type a message below to chat with the AI assistant.
      </p>
      {onNewChat && (
        <Button
          type="button"
          variant="outline"
          className="mt-6"
          onClick={onNewChat}
          disabled={isLoading}
        >
          <MessageSquarePlus className="h-4 w-4" aria-hidden="true" />
          New conversation
        </Button>
      )}
    </div>
  )
}
