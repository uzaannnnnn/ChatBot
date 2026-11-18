import { createContext } from 'react'
import type { Conversation, Message } from '@/types/chat'

export interface ChatContextValue {
  conversations: Conversation[]
  activeConversationId: string | null
  activeConversation: Conversation | null
  messages: Message[]
  isLoadingConversations: boolean
  isLoadingMessages: boolean
  isStreaming: boolean
  error: string | null
  clearError: () => void
  refreshConversations: () => Promise<void>
  selectConversation: (id: string | null) => Promise<void>
  createConversation: (title?: string) => Promise<Conversation>
  renameConversation: (id: string, title: string) => Promise<Conversation>
  deleteConversation: (id: string) => Promise<void>
  sendMessage: (content: string) => Promise<void>
  stopStreaming: () => void
}

export const ChatContext = createContext<ChatContextValue | null>(null)
