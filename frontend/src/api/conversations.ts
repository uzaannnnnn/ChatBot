import { requestJson } from '@/api/client'
import type {
  Conversation,
  CreateConversationRequest,
  ListResponse,
  SingleResponse,
  UpdateConversationRequest,
} from '@/types/chat'

export interface PaginationParams {
  limit?: number
  offset?: number
}

export async function createConversation(
  body: CreateConversationRequest = {},
  signal?: AbortSignal,
): Promise<Conversation> {
  const res = await requestJson<SingleResponse<Conversation>>('/api/v1/conversations', {
    method: 'POST',
    body: JSON.stringify(body),
    signal,
  })
  return res.data
}

export async function listConversations(
  params: PaginationParams = {},
  signal?: AbortSignal,
): Promise<ListResponse<Conversation>> {
  const search = new URLSearchParams()
  if (params.limit !== undefined) search.set('limit', String(params.limit))
  if (params.offset !== undefined) search.set('offset', String(params.offset))
  const qs = search.toString()
  return requestJson<ListResponse<Conversation>>(`/api/v1/conversations${qs ? `?${qs}` : ''}`, {
    method: 'GET',
    signal,
  })
}

export async function getConversation(id: string, signal?: AbortSignal): Promise<Conversation> {
  const res = await requestJson<SingleResponse<Conversation>>(
    `/api/v1/conversations/${encodeURIComponent(id)}`,
    {
      method: 'GET',
      signal,
    },
  )
  return res.data
}

export async function updateConversationTitle(
  id: string,
  title: string,
  signal?: AbortSignal,
): Promise<Conversation> {
  const payload: UpdateConversationRequest = { title }
  const res = await requestJson<SingleResponse<Conversation>>(
    `/api/v1/conversations/${encodeURIComponent(id)}`,
    {
      method: 'PATCH',
      body: JSON.stringify(payload),
      signal,
    },
  )
  return res.data
}

export async function deleteConversation(id: string, signal?: AbortSignal): Promise<void> {
  await requestJson<void>(`/api/v1/conversations/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    signal,
  })
}
