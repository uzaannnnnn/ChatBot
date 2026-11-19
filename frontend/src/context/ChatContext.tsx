import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '@/api/client'
import {
  createConversation as apiCreateConversation,
  deleteConversation as apiDeleteConversation,
  listConversations as apiListConversations,
  updateConversationTitle as apiUpdateConversationTitle,
} from '@/api/conversations'
import { listMessages as apiListMessages } from '@/api/messages'
import { StreamProviderError, streamMessage } from '@/api/sse'
import { ChatContext } from '@/context/chat-context'
import type { Conversation, Message } from '@/types/chat'

function formatErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'conversation_not_found') {
      return 'Conversation not found. It may have been deleted.'
    }
    if (err.code === 'ai_unavailable') {
      return 'AI service is currently unavailable or unconfigured.'
    }
    if (err.code === 'ai_provider_error') {
      return 'AI provider encountered an error while generating a response.'
    }
    return err.message
  }
  if (err instanceof StreamProviderError) {
    return err.message
  }
  if (err instanceof Error) {
    return err.message
  }
  return 'An unexpected error occurred.'
}

function isAbortError(err: unknown): boolean {
  return err instanceof DOMException
    ? err.name === 'AbortError'
    : err instanceof Error && err.name === 'AbortError'
}

export function ChatProvider({ children }: { children: React.ReactNode }) {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoadingConversations, setIsLoadingConversations] = useState<boolean>(true)
  const [isLoadingMessages, setIsLoadingMessages] = useState<boolean>(false)
  const [isStreaming, setIsStreaming] = useState<boolean>(false)
  const [error, setError] = useState<string | null>(null)

  const activeConversationIdRef = useRef<string | null>(null)
  useEffect(() => {
    activeConversationIdRef.current = activeConversationId
  }, [activeConversationId])

  const streamAbortRef = useRef<AbortController | null>(null)
  const messagesAbortRef = useRef<AbortController | null>(null)
  const streamGenerationRef = useRef<number>(0)

  const pendingDeltaRef = useRef<string>('')
  const rafIdRef = useRef<number | null>(null)

  const cancelActiveStream = useCallback(() => {
    streamGenerationRef.current += 1
    if (streamAbortRef.current) {
      streamAbortRef.current.abort()
      streamAbortRef.current = null
    }
    if (rafIdRef.current !== null) {
      cancelAnimationFrame(rafIdRef.current)
      rafIdRef.current = null
    }
    pendingDeltaRef.current = ''
    setIsStreaming(false)
  }, [])

  const clearError = useCallback(() => {
    setError(null)
  }, [])

  const refreshConversations = useCallback(async () => {
    try {
      const res = await apiListConversations({ limit: 50, offset: 0 })
      setConversations(res.data)
    } catch (err) {
      if (!isAbortError(err)) {
        setError(formatErrorMessage(err))
      }
    } finally {
      setIsLoadingConversations(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    apiListConversations({ limit: 50, offset: 0 }, controller.signal)
      .then((res) => {
        if (!controller.signal.aborted) {
          setConversations(res.data)
        }
      })
      .catch((err) => {
        if (!isAbortError(err) && !controller.signal.aborted) {
          setError(formatErrorMessage(err))
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setIsLoadingConversations(false)
        }
      })

    return () => {
      controller.abort()
      cancelActiveStream()
      messagesAbortRef.current?.abort()
    }
  }, [cancelActiveStream])

  const selectConversation = useCallback(
    async (id: string | null) => {
      cancelActiveStream()
      messagesAbortRef.current?.abort()
      setError(null)
      setActiveConversationId(id)
      activeConversationIdRef.current = id

      if (!id) {
        setMessages([])
        setIsLoadingMessages(false)
        return
      }

      const controller = new AbortController()
      messagesAbortRef.current = controller
      setIsLoadingMessages(true)
      setMessages([])

      try {
        const res = await apiListMessages(id, { limit: 100, offset: 0 }, controller.signal)
        if (activeConversationIdRef.current === id && !controller.signal.aborted) {
          setMessages(res.data)
        }
      } catch (err) {
        if (!isAbortError(err) && activeConversationIdRef.current === id) {
          setError(formatErrorMessage(err))
        }
      } finally {
        if (activeConversationIdRef.current === id && !controller.signal.aborted) {
          setIsLoadingMessages(false)
        }
      }
    },
    [cancelActiveStream],
  )

  const createConversation = useCallback(
    async (title?: string): Promise<Conversation> => {
      cancelActiveStream()
      setError(null)
      const created = await apiCreateConversation(title ? { title } : {})
      setConversations((prev) => [created, ...prev])
      setActiveConversationId(created.id)
      activeConversationIdRef.current = created.id
      setMessages([])
      return created
    },
    [cancelActiveStream],
  )

  const renameConversation = useCallback(
    async (id: string, title: string): Promise<Conversation> => {
      setError(null)
      const updated = await apiUpdateConversationTitle(id, title)
      setConversations((prev) => prev.map((c) => (c.id === id ? updated : c)))
      return updated
    },
    [],
  )

  const deleteConversation = useCallback(
    async (id: string): Promise<void> => {
      setError(null)
      if (activeConversationIdRef.current === id) {
        cancelActiveStream()
        messagesAbortRef.current?.abort()
      }

      await apiDeleteConversation(id)
      setConversations((prev) => prev.filter((c) => c.id !== id))

      if (activeConversationIdRef.current === id) {
        setActiveConversationId(null)
        activeConversationIdRef.current = null
        setMessages([])
      }
    },
    [cancelActiveStream],
  )

  const stopStreaming = useCallback(() => {
    const targetId = activeConversationIdRef.current
    cancelActiveStream()
    if (!targetId) return

    setMessages((prev) =>
      prev
        .map((m) => (m.isStreaming ? { ...m, isStreaming: false } : m))
        .filter(
          (m) =>
            !(
              m.role === 'assistant' &&
              m.isOptimistic &&
              m.content.trim() === '' &&
              (!m.attachments || m.attachments.length === 0)
            ),
        ),
    )
  }, [cancelActiveStream])

  const sendMessage = useCallback(
    async (rawContent: string) => {
      const trimmed = rawContent.trim()
      if (!trimmed) {
        setError('Message content cannot be empty.')
        return
      }

      setError(null)

      let targetConvId = activeConversationIdRef.current
      if (!targetConvId) {
        try {
          const created = await createConversation()
          targetConvId = created.id
        } catch (err) {
          setError(formatErrorMessage(err))
          return
        }
      }

      // Cancel any residual stream before starting a new one
      cancelActiveStream()

      const myGen = ++streamGenerationRef.current
      const controller = new AbortController()
      streamAbortRef.current = controller

      const tempUserId = `temp-user-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const tempAssistantId = `temp-assistant-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const nowISO = new Date().toISOString()

      const optimisticUserMsg: Message = {
        id: tempUserId,
        conversation_id: targetConvId,
        role: 'user',
        content: trimmed,
        created_at: nowISO,
        isOptimistic: true,
      }

      const optimisticAssistantMsg: Message = {
        id: tempAssistantId,
        conversation_id: targetConvId,
        role: 'assistant',
        content: '',
        created_at: nowISO,
        isOptimistic: true,
        isStreaming: true,
      }

      setIsStreaming(true)
      setMessages((prev) => [...prev, optimisticUserMsg, optimisticAssistantMsg])

      const flushPendingDeltas = () => {
        if (rafIdRef.current !== null) {
          cancelAnimationFrame(rafIdRef.current)
          rafIdRef.current = null
        }
        const delta = pendingDeltaRef.current
        if (!delta) return
        pendingDeltaRef.current = ''

        if (
          streamGenerationRef.current !== myGen ||
          activeConversationIdRef.current !== targetConvId
        ) {
          return
        }

        setMessages((prev) =>
          prev.map((m) => (m.id === tempAssistantId ? { ...m, content: m.content + delta } : m)),
        )
      }

      try {
        await streamMessage({
          conversationId: targetConvId,
          content: trimmed,
          signal: controller.signal,
          callbacks: {
            onStart: () => {
              // Stream started cleanly
            },
            onDelta: ({ delta }) => {
              if (
                streamGenerationRef.current !== myGen ||
                activeConversationIdRef.current !== targetConvId
              ) {
                return
              }
              pendingDeltaRef.current += delta
              if (rafIdRef.current === null) {
                rafIdRef.current = requestAnimationFrame(flushPendingDeltas)
              }
            },
            onToolStatus: (statusPayload) => {
              if (
                streamGenerationRef.current !== myGen ||
                activeConversationIdRef.current !== targetConvId
              ) {
                return
              }
              setMessages((prev) =>
                prev.map((m) => {
                  if (m.id !== tempAssistantId) return m
                  const existing = m.toolStatuses ?? []
                  const idx = existing.findIndex((s) => s.tool === statusPayload.tool)
                  const nextStatuses =
                    idx >= 0
                      ? existing.map((s, i) => (i === idx ? statusPayload : s))
                      : [...existing, statusPayload]
                  return { ...m, toolStatuses: nextStatuses }
                }),
              )
            },
            onAttachment: (attachmentPayload) => {
              if (
                streamGenerationRef.current !== myGen ||
                activeConversationIdRef.current !== targetConvId
              ) {
                return
              }
              setMessages((prev) =>
                prev.map((m) => {
                  if (m.id !== tempAssistantId) return m
                  const existing = m.attachments ?? []
                  if (existing.some((a) => a.id === attachmentPayload.id)) {
                    return m
                  }
                  return { ...m, attachments: [...existing, attachmentPayload] }
                }),
              )
            },
            onComplete: ({ message_id }) => {
              flushPendingDeltas()
              if (
                streamGenerationRef.current !== myGen ||
                activeConversationIdRef.current !== targetConvId
              ) {
                return
              }

              setMessages((prev) =>
                prev.map((m) =>
                  m.id === tempAssistantId
                    ? { ...m, id: message_id, isStreaming: false, isOptimistic: false }
                    : m,
                ),
              )
            },
            onError: () => {
              flushPendingDeltas()
            },
          },
        })

        // Reconcile optimistic user message ID and timestamps from backend + refresh sidebar order
        if (
          streamGenerationRef.current === myGen &&
          activeConversationIdRef.current === targetConvId
        ) {
          try {
            const [msgsRes] = await Promise.all([
              apiListMessages(targetConvId, { limit: 100, offset: 0 }),
              refreshConversations(),
            ])
            if (
              streamGenerationRef.current === myGen &&
              activeConversationIdRef.current === targetConvId
            ) {
              setMessages(msgsRes.data)
            }
          } catch {
            // Non-fatal: optimistic state already holds message_id
          }
        }
      } catch (err) {
        flushPendingDeltas()

        if (isAbortError(err)) {
          // User cancellation is handled distinctly from genuine errors
          return
        }

        if (
          streamGenerationRef.current !== myGen ||
          activeConversationIdRef.current !== targetConvId
        ) {
          return
        }

        setError(formatErrorMessage(err))
        // Keep user message visible; remove empty/incomplete assistant placeholder
        setMessages((prev) =>
          prev
            .filter(
              (m) =>
                m.id !== tempAssistantId ||
                m.content.trim() !== '' ||
                (m.attachments && m.attachments.length > 0),
            )
            .map((m) => (m.id === tempAssistantId ? { ...m, isStreaming: false } : m)),
        )
      } finally {
        if (streamGenerationRef.current === myGen) {
          streamAbortRef.current = null
          setIsStreaming(false)
        }
      }
    },
    [cancelActiveStream, createConversation, refreshConversations],
  )

  const activeConversation = conversations.find((c) => c.id === activeConversationId) ?? null

  return (
    <ChatContext.Provider
      value={{
        conversations,
        activeConversationId,
        activeConversation,
        messages,
        isLoadingConversations,
        isLoadingMessages,
        isStreaming,
        error,
        clearError,
        refreshConversations,
        selectConversation,
        createConversation,
        renameConversation,
        deleteConversation,
        sendMessage,
        stopStreaming,
      }}
    >
      {children}
    </ChatContext.Provider>
  )
}
