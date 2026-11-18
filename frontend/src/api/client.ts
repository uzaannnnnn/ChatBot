import type { ApiErrorResponse } from '@/types/chat'

const rawBaseUrl = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'
export const API_BASE_URL = rawBaseUrl.replace(/\/+$/, '')

let accessTokenProvider: (() => string | null) | null = null
let unauthorizedHandler: (() => void) | null = null

export function setAuthHooks(
  getToken: (() => string | null) | null,
  onUnauthorized: (() => void) | null,
): void {
  accessTokenProvider = getToken
  unauthorizedHandler = onUnauthorized
}

export function getAuthHeaders(): Record<string, string> {
  const token = accessTokenProvider?.()
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

export function notifyUnauthorized(): void {
  unauthorizedHandler?.()
}

export class ApiError extends Error {
  public readonly status: number
  public readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export async function parseApiError(response: Response): Promise<ApiError> {
  if (response.status === 401) {
    notifyUnauthorized()
  }

  try {
    const text = await response.text()
    if (!text) {
      return new ApiError(
        response.status,
        'http_error',
        response.statusText || `HTTP ${response.status}`,
      )
    }
    const parsed = JSON.parse(text) as Partial<ApiErrorResponse>
    if (parsed?.error?.code && parsed?.error?.message) {
      return new ApiError(response.status, parsed.error.code, parsed.error.message)
    }
    return new ApiError(response.status, 'http_error', text)
  } catch {
    return new ApiError(
      response.status,
      'http_error',
      response.statusText || `HTTP ${response.status}`,
    )
  }
}

export async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${API_BASE_URL}${path}`
  const headers = new Headers(init?.headers)

  for (const [k, v] of Object.entries(getAuthHeaders())) {
    if (!headers.has(k)) {
      headers.set(k, v)
    }
  }

  if (init?.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(url, {
    ...init,
    headers,
  })

  if (!response.ok) {
    throw await parseApiError(response)
  }

  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  if (!text) {
    return undefined as T
  }

  return JSON.parse(text) as T
}
