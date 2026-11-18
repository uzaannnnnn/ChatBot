import { MessageComposer } from '@/components/chat/MessageComposer'
import { MessageList } from '@/components/chat/MessageList'
import { ErrorState } from '@/components/common/ErrorState'
import { useChat } from '@/hooks/useChat'

export function ChatContainer() {
  const {
    activeConversationId,
    messages,
    isLoadingMessages,
    isStreaming,
    error,
    clearError,
    createConversation,
    sendMessage,
    stopStreaming,
  } = useChat()

  return (
    <main className="flex min-h-0 flex-1 flex-col bg-background">
      <MessageList
        messages={messages}
        isLoading={isLoadingMessages}
        hasActiveConversation={activeConversationId !== null}
        onNewChat={() => void createConversation()}
      />

      {error && (
        <div className="px-4 sm:px-6">
          <ErrorState message={error} onDismiss={clearError} />
        </div>
      )}

      <MessageComposer
        onSend={sendMessage}
        onStop={stopStreaming}
        isStreaming={isStreaming}
        disabled={isLoadingMessages}
      />
    </main>
  )
}
