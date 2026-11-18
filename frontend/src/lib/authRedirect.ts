const AUTH_HASH_PARAMS = [
  'access_token',
  'refresh_token',
  'expires_in',
  'expires_at',
  'token_type',
  'provider_token',
  'provider_refresh_token',
  'type',
  'error',
  'error_code',
  'error_description',
] as const

const AUTH_QUERY_PARAMS = [
  'code',
  'token_hash',
  'type',
  'error',
  'error_code',
  'error_description',
] as const

function normalizeHttpOrigin(rawUrl: string | undefined): string | null {
  const trimmed = rawUrl?.trim()
  if (!trimmed || trimmed === 'null') {
    return null
  }
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return null
    }
    return parsed.origin
  } catch {
    return null
  }
}

/**
 * Resolves the email confirmation redirect origin.
 * Prefers explicit VITE_SITE_URL when configured, otherwise uses the active browser origin.
 */
export function getEmailRedirectTo(
  configuredSiteUrl: string | undefined = import.meta.env.VITE_SITE_URL,
): string | undefined {
  const configuredOrigin = normalizeHttpOrigin(configuredSiteUrl)
  if (configuredOrigin) {
    return configuredOrigin
  }

  if (typeof window !== 'undefined') {
    const browserOrigin = normalizeHttpOrigin(window.location?.origin)
    if (browserOrigin) {
      return browserOrigin
    }
  }

  return undefined
}

function getCallbackParams(locationLike: Pick<Location, 'search' | 'hash'>): {
  searchParams: URLSearchParams
  hashParams: URLSearchParams
} {
  const rawSearch = locationLike.search?.startsWith('?')
    ? locationLike.search.slice(1)
    : locationLike.search || ''
  const rawHash = locationLike.hash?.startsWith('#')
    ? locationLike.hash.slice(1)
    : locationLike.hash || ''

  return {
    searchParams: new URLSearchParams(rawSearch),
    hashParams: new URLSearchParams(rawHash),
  }
}

export function hasAuthCallbackParams(
  locationLike: Pick<Location, 'search' | 'hash'> = window.location,
): boolean {
  const { searchParams, hashParams } = getCallbackParams(locationLike)
  return (
    AUTH_HASH_PARAMS.some((key) => hashParams.has(key)) ||
    AUTH_QUERY_PARAMS.some((key) => searchParams.has(key))
  )
}

export function parseAuthCallbackError(
  locationLike: Pick<Location, 'search' | 'hash'> = window.location,
): string | null {
  const { searchParams, hashParams } = getCallbackParams(locationLike)
  const errorCode = hashParams.get('error_code') ?? searchParams.get('error_code')
  const error = hashParams.get('error') ?? searchParams.get('error')
  const description = hashParams.get('error_description') ?? searchParams.get('error_description')

  if (!error && !errorCode && !description) {
    return null
  }

  const normalizedDesc = description?.toLowerCase() ?? ''
  if (errorCode === 'otp_expired' || normalizedDesc.includes('expired')) {
    return 'Your email confirmation link has expired. Please register again or request a new confirmation email.'
  }

  if (error === 'access_denied' || normalizedDesc.includes('invalid')) {
    return 'The email confirmation link is invalid or has already been used. Please sign in or register again.'
  }

  if (description && description.trim().length > 0) {
    return description.trim()
  }

  return 'Email confirmation failed. Please sign in or register again.'
}

/**
 * Removes sensitive Supabase auth callback parameters from the browser URL
 * using history.replaceState. Call only after Supabase has processed the URL.
 */
export function cleanAuthCallbackUrl(): void {
  if (typeof window === 'undefined' || !window.history?.replaceState) {
    return
  }

  const { searchParams, hashParams } = getCallbackParams(window.location)
  let changed = false

  for (const key of AUTH_QUERY_PARAMS) {
    if (searchParams.has(key)) {
      searchParams.delete(key)
      changed = true
    }
  }

  for (const key of AUTH_HASH_PARAMS) {
    if (hashParams.has(key)) {
      hashParams.delete(key)
      changed = true
    }
  }

  const nextPathname =
    window.location.pathname === '/auth/callback' ? '/' : window.location.pathname
  if (nextPathname !== window.location.pathname) {
    changed = true
  }

  const nextSearch = searchParams.toString()
  const nextHash = hashParams.toString()
  if (!nextHash && window.location.href.endsWith('#')) {
    changed = true
  }

  if (!changed) {
    return
  }

  const cleanUrl = `${nextPathname}${nextSearch ? `?${nextSearch}` : ''}${nextHash ? `#${nextHash}` : ''}`
  window.history.replaceState(null, '', cleanUrl)
}
