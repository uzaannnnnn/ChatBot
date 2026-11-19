import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
import type { ToolStatus } from '@/types/chat'

interface ToolStatusBadgeProps {
  status: ToolStatus
}

function formatToolLabel(status: ToolStatus): string {
  if (status.tool === 'generate_image') {
    if (status.status === 'running') return 'Generating image...'
    if (status.status === 'completed') return 'Image generated'
    return status.message || 'Image generation failed'
  }
  if (status.status === 'running') return `Running ${status.tool}...`
  if (status.status === 'completed') return `Completed ${status.tool}`
  return status.message || `${status.tool} failed`
}

export function ToolStatusBadge({ status }: ToolStatusBadgeProps) {
  const label = formatToolLabel(status)

  if (status.status === 'running') {
    return (
      <div
        role="status"
        data-testid="tool-status-running"
        className="mb-2 inline-flex items-center gap-2 rounded-lg border border-border bg-muted/60 px-3 py-1.5 text-xs font-medium text-foreground"
      >
        <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
        <span>{label}</span>
      </div>
    )
  }

  if (status.status === 'failed') {
    return (
      <div
        role="alert"
        data-testid="tool-status-failed"
        className="mb-2 inline-flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-1.5 text-xs font-medium text-destructive"
      >
        <AlertCircle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <span>{label}</span>
      </div>
    )
  }

  return (
    <div
      role="status"
      data-testid="tool-status-completed"
      className="mb-2 inline-flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground"
    >
      <CheckCircle2
        className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400"
        aria-hidden="true"
      />
      <span>{label}</span>
    </div>
  )
}
