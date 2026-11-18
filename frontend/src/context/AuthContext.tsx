import React, { useCallback, useEffect, useRef, useState } from 'react'
import type { Session } from '@supabase/supabase-js'
import { setAuthHooks } from '@/api/client'
import { AuthContext, type AuthUser } from '@/context/auth-context'
import {
  cleanAuthCallbackUrl,
  getEmailRedirectTo,
  hasAuthCallbackParams,
  parseAuthCallbackError,
} from '@/lib/authRedirect'
import { supabase } from '@/lib/supabase'

interface AuthProviderProps {
  children: React.ReactNode
  initialUser?: AuthUser | null
  initialToken?: string | null
}

export function AuthProvider({
  children,
  initialUser = null,
  initialToken = null,
}: AuthProviderProps) {
  const [user, setUser] = useState<AuthUser | null>(initialUser)
  const [accessToken, setAccessToken] = useState<string | null>(initialToken)
  const [isLoading, setIsLoading] = useState<boolean>(() => Boolean(supabase && !initialUser))
  const [error, setError] = useState<string | null>(null)

  const tokenRef = useRef<string | null>(initialToken)
  useEffect(() => {
    tokenRef.current = accessToken
  }, [accessToken])

  const applySession = useCallback((session: Session) => {
    if (tokenRef.current === session.access_token) {
      return
    }
    tokenRef.current = session.access_token
    setAccessToken(session.access_token)
    setUser({ id: session.user.id, email: session.user.email })
  }, [])

  const clearSessionState = useCallback(() => {
    tokenRef.current = null
    setAccessToken(null)
    setUser(null)
  }, [])

  const clearError = useCallback(() => setError(null), [])

  const signOut = useCallback(async () => {
    setError(null)
    try {
      if (supabase) {
        await supabase.auth.signOut()
      }
    } finally {
      tokenRef.current = null
      setAccessToken(null)
      setUser(null)
    }
  }, [])

  const handleUnauthorized = useCallback(async () => {
    if (supabase) {
      const { data, error: refreshErr } = await supabase.auth.refreshSession()
      if (!refreshErr && data.session) {
        tokenRef.current = data.session.access_token
        setAccessToken(data.session.access_token)
        setUser({ id: data.session.user.id, email: data.session.user.email })
        return
      }
      await supabase.auth.signOut()
    }
    tokenRef.current = null
    setAccessToken(null)
    setUser(null)
    setError('Your session has expired. Please sign in again.')
  }, [])

  useEffect(() => {
    setAuthHooks(
      () => tokenRef.current,
      () => {
        void handleUnauthorized()
      },
    )
    return () => setAuthHooks(null, null)
  }, [handleUnauthorized])

  useEffect(() => {
    if (!supabase || initialUser) {
      return
    }

    let mounted = true
    const callbackError =
      typeof window !== 'undefined' ? parseAuthCallbackError(window.location) : null

    supabase.auth
      .getSession()
      .then(({ data, error: sessErr }) => {
        if (!mounted) return
        if (sessErr) {
          setError(callbackError ?? sessErr.message)
        } else if (data.session) {
          applySession(data.session)
          setError(null)
        } else if (callbackError) {
          setError(callbackError)
        }

        if (
          typeof window !== 'undefined' &&
          (hasAuthCallbackParams(window.location) || window.location.href.endsWith('#'))
        ) {
          cleanAuthCallbackUrl()
        }
      })
      .finally(() => {
        if (mounted) setIsLoading(false)
      })

    const { data: sub } = supabase.auth.onAuthStateChange((event, session) => {
      if (!mounted) return
      if (session) {
        applySession(session)
        if (event === 'SIGNED_IN') {
          setError(null)
        }
        if (
          typeof window !== 'undefined' &&
          (hasAuthCallbackParams(window.location) || window.location.href.endsWith('#'))
        ) {
          cleanAuthCallbackUrl()
        }
      } else {
        clearSessionState()
      }
    })

    return () => {
      mounted = false
      sub.subscription.unsubscribe()
    }
  }, [applySession, clearSessionState, initialUser])

  const signIn = useCallback(async (email: string, password: string) => {
    setError(null)
    if (!supabase) {
      setError(
        'Authentication is not configured (missing VITE_SUPABASE_URL or VITE_SUPABASE_ANON_KEY).',
      )
      return
    }
    const { data, error: authErr } = await supabase.auth.signInWithPassword({ email, password })
    if (authErr) {
      setError(authErr.message)
      throw authErr
    }
    if (data.session) {
      tokenRef.current = data.session.access_token
      setAccessToken(data.session.access_token)
      setUser({ id: data.session.user.id, email: data.session.user.email })
    }
  }, [])

  const signUp = useCallback(async (email: string, password: string): Promise<string | null> => {
    setError(null)
    if (!supabase) {
      const msg =
        'Authentication is not configured (missing VITE_SUPABASE_URL or VITE_SUPABASE_ANON_KEY).'
      setError(msg)
      return null
    }
    const emailRedirectTo = getEmailRedirectTo()
    const { data, error: authErr } = await supabase.auth.signUp({
      email,
      password,
      ...(emailRedirectTo ? { options: { emailRedirectTo } } : {}),
    })
    if (authErr) {
      setError(authErr.message)
      throw authErr
    }
    if (data.session) {
      tokenRef.current = data.session.access_token
      setAccessToken(data.session.access_token)
      setUser({ id: data.session.user.id, email: data.session.user.email })
      return null
    }
    return 'Registration successful. Check your email to confirm your account before signing in.'
  }, [])

  return (
    <AuthContext.Provider
      value={{
        user,
        accessToken,
        isLoading,
        error,
        clearError,
        signIn,
        signUp,
        signOut,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}
