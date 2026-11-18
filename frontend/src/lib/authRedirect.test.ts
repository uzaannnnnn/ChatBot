import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  cleanAuthCallbackUrl,
  getEmailRedirectTo,
  hasAuthCallbackParams,
  parseAuthCallbackError,
} from '@/lib/authRedirect'

describe('authRedirect', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/')
  })

  describe('getEmailRedirectTo', () => {
    it('uses explicit production VITE_SITE_URL origin when configured', () => {
      expect(getEmailRedirectTo('https://doubleu-ai.eshace.com/')).toBe(
        'https://doubleu-ai.eshace.com',
      )
    })

    it('falls back to current browser origin for local development', () => {
      vi.spyOn(window, 'location', 'get').mockReturnValue({
        ...window.location,
        origin: 'http://localhost:5173',
      })

      expect(getEmailRedirectTo('')).toBe('http://localhost:5173')
    })

    it('falls back to production browser origin when VITE_SITE_URL is unset', () => {
      vi.spyOn(window, 'location', 'get').mockReturnValue({
        ...window.location,
        origin: 'https://doubleu-ai.eshace.com',
      })

      expect(getEmailRedirectTo(undefined)).toBe('https://doubleu-ai.eshace.com')
    })

    it('ignores invalid non-http(s) configured URLs', () => {
      vi.spyOn(window, 'location', 'get').mockReturnValue({
        ...window.location,
        origin: 'https://doubleu-ai.eshace.com',
      })

      expect(getEmailRedirectTo('javascript:alert(1)')).toBe('https://doubleu-ai.eshace.com')
      expect(getEmailRedirectTo('not-a-url')).toBe('https://doubleu-ai.eshace.com')
    })
  })

  describe('parseAuthCallbackError', () => {
    it('returns null when no error parameters exist', () => {
      expect(
        parseAuthCallbackError({
          search: '',
          hash: '#access_token=secret-jwt&refresh_token=secret-refresh&type=signup',
        }),
      ).toBeNull()
    })

    it('maps otp_expired hash error to a clear message', () => {
      const msg = parseAuthCallbackError({
        search: '',
        hash: '#error=access_denied&error_code=otp_expired&error_description=Email+link+is+invalid+or+has+expired',
      })
      expect(msg).toMatch(/expired/i)
    })

    it('maps access_denied query error to a clear message', () => {
      const msg = parseAuthCallbackError({
        search: '?error=access_denied&error_description=Invalid+token',
        hash: '',
      })
      expect(msg).toMatch(/invalid/i)
    })
  })

  describe('hasAuthCallbackParams and cleanAuthCallbackUrl', () => {
    it('removes sensitive auth hash parameters while preserving unrelated params', () => {
      window.history.replaceState(
        null,
        '',
        '/auth/callback?view=chat#access_token=secret-jwt&refresh_token=secret-refresh&type=signup&section=top',
      )

      expect(hasAuthCallbackParams(window.location)).toBe(true)
      cleanAuthCallbackUrl()

      expect(window.location.pathname).toBe('/')
      expect(window.location.search).toBe('?view=chat')
      expect(window.location.hash).toBe('#section=top')
      expect(window.location.href).not.toContain('access_token')
      expect(window.location.href).not.toContain('refresh_token')
    })

    it('removes PKCE code and error query parameters', () => {
      window.history.replaceState(
        null,
        '',
        '/?code=auth-code-123&error=access_denied&error_code=otp_expired',
      )

      expect(hasAuthCallbackParams(window.location)).toBe(true)
      cleanAuthCallbackUrl()

      expect(window.location.pathname).toBe('/')
      expect(window.location.search).toBe('')
      expect(window.location.hash).toBe('')
    })
  })
})
