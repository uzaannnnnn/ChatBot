import { Code2, ImageIcon, MessageSquarePlus, Sparkles } from 'lucide-react'
import { BrandMark } from '@/components/common/BrandMark'
import { Button } from '@/components/ui/button'

interface EmptyStateProps {
  onNewChat?: () => void
  onSelectPrompt?: (prompt: string) => void
  isLoading?: boolean
}

const STARTER_PROMPTS = [
  {
    icon: Sparkles,
    label: 'Explore an idea',
    prompt: 'Explain how modern AI assistants coordinate language models and tools.',
  },
  {
    icon: Code2,
    label: 'Write clean code',
    prompt: 'Write a clean TypeScript helper for retrying async requests with exponential backoff.',
  },
  {
    icon: ImageIcon,
    label: 'Generate an image',
    prompt: 'Generate an image of a minimalist sunlit studio desk with warm morning shadows.',
  },
]

export function EmptyState({ onNewChat, onSelectPrompt, isLoading }: EmptyStateProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-12 text-center">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-xs">
        <BrandMark className="h-6 w-6" />
      </div>
      <h2 className="text-2xl font-semibold tracking-tight text-foreground">DOUBLEU AI</h2>
      <p className="mt-1 text-sm font-medium text-muted-foreground">Think More. Discover More.</p>
      <p className="mt-2 max-w-md text-sm text-muted-foreground">
        Ask questions, write code, or generate images in one unified AI assistant workspace.
      </p>

      {onSelectPrompt && (
        <div className="mt-6 grid w-full max-w-2xl grid-cols-1 gap-2.5 sm:grid-cols-3">
          {STARTER_PROMPTS.map(({ icon: Icon, label, prompt }) => (
            <button
              key={label}
              type="button"
              disabled={isLoading}
              onClick={() => onSelectPrompt(prompt)}
              className="group flex flex-col items-start gap-1.5 rounded-xl border border-border bg-card p-3.5 text-left transition-colors hover:border-foreground/25 hover:bg-accent/50 disabled:pointer-events-none disabled:opacity-50 cursor-pointer"
            >
              <div className="flex items-center gap-2 text-xs font-semibold text-foreground">
                <Icon className="h-3.5 w-3.5 text-muted-foreground group-hover:text-foreground" />
                <span>{label}</span>
              </div>
              <span className="line-clamp-2 text-xs text-muted-foreground">{prompt}</span>
            </button>
          ))}
        </div>
      )}

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
