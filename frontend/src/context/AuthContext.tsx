import React, { useCallback, useEffect, useRef, useState } from 'react'
import { setAuthHooks } from '@/api/client'
import { AuthContext, type AuthUser } from '@/context/auth-context'
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
    supabase.auth
      .getSession()
      .then(({ data, error: sessErr }) => {
        if (!mounted) return
        if (sessErr) {
          setError(sessErr.message)
        } else if (data.session) {
          tokenRef.current = data.session.access_token
          setAccessToken(data.session.access_token)
          setUser({ id: data.session.user.id, email: data.session.user.email })
        }
      })
      .finally(() => {
        if (mounted) setIsLoading(false)
      })

    const { data: sub } = supabase.auth.onAuthStateChange((_event, session) => {
      if (!mounted) return
      if (session) {
        tokenRef.current = session.access_token
        setAccessToken(session.access_token)
        setUser({ id: session.user.id, email: session.user.email })
      } else {
        tokenRef.current = null
        setAccessToken(null)
        setUser(null)
      }
    })

    return () => {
      mounted = false
      sub.subscription.unsubscribe()
    }
  }, [initialUser])

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
    const { data, error: authErr } = await supabase.auth.signUp({ email, password })
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
