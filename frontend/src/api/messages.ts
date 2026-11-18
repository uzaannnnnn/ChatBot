import { requestJson } from '@/api/client'
import type { PaginationParams } from '@/api/conversations'
import type { ListResponse, Message } from '@/types/chat'

export async function listMessages(
  conversationId: string,
  params: PaginationParams = {},
  signal?: AbortSignal,
): Promise<ListResponse<Message>> {
  const search = new URLSearchParams()
  if (params.limit !== undefined) search.set('limit', String(params.limit))
  if (params.offset !== undefined) search.set('offset', String(params.offset))
  const qs = search.toString()
  return requestJson<ListResponse<Message>>(
    `/api/v1/conversations/${encodeURIComponent(conversationId)}/messages${qs ? `?${qs}` : ''}`,
    {
      method: 'GET',
      signal,
    },
  )
}
