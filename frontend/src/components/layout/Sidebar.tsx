import { LogOut, Plus, X } from 'lucide-react'
import { BrandMark } from '@/components/common/BrandMark'
import { ConversationList } from '@/components/conversation/ConversationList'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/hooks/useAuth'
import { useChat } from '@/hooks/useChat'
import { cn } from '@/utils/cn'

interface SidebarProps {
  isOpenMobile: boolean
  onCloseMobile: () => void
}

export function Sidebar({ isOpenMobile, onCloseMobile }: SidebarProps) {
  const { user, signOut } = useAuth()
  const {
    conversations,
    activeConversationId,
    isLoadingConversations,
    selectConversation,
    createConversation,
    renameConversation,
    deleteConversation,
    stopStreaming,
  } = useChat()

  const handleSignOut = async () => {
    stopStreaming()
    onCloseMobile()
    await signOut()
  }

  const handleSelect = (id: string) => {
    void selectConversation(id)
    onCloseMobile()
  }

  const handleNewConversation = async () => {
    await createConversation()
    onCloseMobile()
  }

  return (
    <>
      {/* Mobile overlay backdrop */}
      {isOpenMobile && (
        <div
          className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[1px] md:hidden"
          onClick={onCloseMobile}
          aria-hidden="true"
        />
      )}

      <aside
        aria-label="Conversation history"
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex w-72 flex-col border-r border-border bg-card transition-all duration-200 ease-in-out md:visible md:static md:translate-x-0',
          isOpenMobile ? 'visible translate-x-0' : 'invisible -translate-x-full',
        )}
      >
        <div className="flex h-14 items-center justify-between border-b border-border px-4">
          <div className="flex items-center gap-2.5 font-semibold tracking-tight text-foreground">
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <BrandMark className="h-4 w-4" />
            </div>
            <div className="flex flex-col leading-none">
              <span className="text-sm font-semibold tracking-tight">DOUBLEU AI</span>
              <span className="mt-0.5 text-[10px] font-normal text-muted-foreground">
                Think More. Discover More.
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={onCloseMobile}
            aria-label="Close sidebar"
            className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground md:hidden"
          >
            <X className="h-5 w-5" aria-hidden="true" />
          </button>
        </div>

        <div className="p-3">
          <Button
            type="button"
            variant="outline"
            className="w-full justify-start"
            onClick={() => void handleNewConversation()}
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            New conversation
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto">
          <ConversationList
            conversations={conversations}
            activeConversationId={activeConversationId}
            isLoading={isLoadingConversations}
            onSelect={handleSelect}
            onRename={renameConversation}
            onDelete={deleteConversation}
          />
        </div>

        <div className="border-t border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-xs text-muted-foreground" title={user?.email}>
              {user?.email || 'Authenticated user'}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => void handleSignOut()}
              aria-label="Sign out"
              title="Sign out"
            >
              <LogOut className="h-4 w-4" aria-hidden="true" />
            </Button>
          </div>
        </div>
      </aside>
    </>
  )
}
