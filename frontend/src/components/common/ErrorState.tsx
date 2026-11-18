import { AlertCircle, X } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ErrorStateProps {
  message: string
  onDismiss?: () => void
  onRetry?: () => void
}

export function ErrorState({ message, onDismiss, onRetry }: ErrorStateProps) {
  return (
    <div
      role="alert"
      className="mx-auto mb-3 flex w-full max-w-3xl items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive"
    >
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
      <div className="flex-1 break-words">{message}</div>
      <div className="flex items-center gap-1">
        {onRetry && (
          <Button type="button" variant="ghost" size="sm" onClick={onRetry}>
            Retry
          </Button>
        )}
        {onDismiss && (
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Dismiss error"
            className="rounded-md p-1 opacity-75 transition-opacity hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        )}
      </div>
    </div>
  )
}
