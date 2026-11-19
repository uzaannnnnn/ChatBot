export type MessageRole = 'user' | 'assistant'

export interface Conversation {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export interface MessageAttachment {
  id: string
  kind: 'image'
  url: string
  mime_type: string
  alt_text: string
  width: number
  height: number
}

export type ToolExecutionState = 'running' | 'completed' | 'failed'

export interface ToolStatus {
  tool: string
  status: ToolExecutionState
  message?: string
}

export interface Message {
  id: string
  conversation_id: string
  role: MessageRole
  content: string
  attachments?: MessageAttachment[]
  created_at: string
  /** Client-only optimistic flags; never sent to backend */
  isOptimistic?: boolean
  isStreaming?: boolean
  toolStatuses?: ToolStatus[]
}

export interface PaginationMeta {
  limit: number
  offset: number
  has_more: boolean
}

export interface SingleResponse<T> {
  data: T
}

export interface ListResponse<T> {
  data: T[]
  pagination: PaginationMeta
}

export interface ApiErrorDetail {
  code: string
  message: string
}

export interface ApiErrorResponse {
  error: ApiErrorDetail
}

export interface CreateConversationRequest {
  title?: string
}

export interface UpdateConversationRequest {
  title: string
}

export interface CreateMessageRequest {
  content: string
}

export interface SSEMessageStartPayload {
  conversation_id: string
  role: 'assistant'
}

export interface SSEContentDeltaPayload {
  delta: string
}

export interface SSEToolStatusPayload {
  tool: string
  status: ToolExecutionState
  message?: string
}

export type SSEAttachmentPayload = MessageAttachment

export interface SSEMessageCompletePayload {
  status: 'completed'
  message_id: string
}

export interface SSEErrorPayload {
  code: string
  message: string
}

export type SSEEvent =
  | { event: 'message_start'; data: SSEMessageStartPayload }
  | { event: 'content_delta'; data: SSEContentDeltaPayload }
  | { event: 'tool_status'; data: SSEToolStatusPayload }
  | { event: 'attachment'; data: SSEAttachmentPayload }
  | { event: 'message_complete'; data: SSEMessageCompletePayload }
  | { event: 'error'; data: SSEErrorPayload }
