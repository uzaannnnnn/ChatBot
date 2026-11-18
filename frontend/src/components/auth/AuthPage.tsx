import React, { useState } from 'react'
import { Bot } from 'lucide-react'
import { ErrorState } from '@/components/common/ErrorState'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAuth } from '@/hooks/useAuth'

type AuthMode = 'login' | 'register'

export function AuthPage() {
  const { signIn, signUp, error, clearError } = useAuth()
  const [mode, setMode] = useState<AuthMode>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [localError, setLocalError] = useState<string | null>(null)
  const [infoMessage, setInfoMessage] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)

  const switchMode = (next: AuthMode) => {
    setMode(next)
    setLocalError(null)
    setInfoMessage(null)
    clearError()
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLocalError(null)
    setInfoMessage(null)
    clearError()

    const trimmedEmail = email.trim()
    if (!trimmedEmail || !trimmedEmail.includes('@')) {
      setLocalError('Please enter a valid email address.')
      return
    }
    if (password.length < 6) {
      setLocalError('Password must be at least 6 characters.')
      return
    }

    setIsSubmitting(true)
    try {
      if (mode === 'login') {
        await signIn(trimmedEmail, password)
      } else {
        const msg = await signUp(trimmedEmail, password)
        if (msg) {
          setInfoMessage(msg)
        }
      }
    } catch {
      // Error surfaced via AuthContext error state
    } finally {
      setIsSubmitting(false)
    }
  }

  const displayError = localError || error

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4 py-12">
      <div className="w-full max-w-sm space-y-6 rounded-xl border border-border bg-card p-6 shadow-sm">
        <div className="flex flex-col items-center text-center">
          <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Bot className="h-5 w-5" aria-hidden="true" />
          </div>
          <h1 className="text-lg font-semibold tracking-tight text-foreground">
            {mode === 'login' ? 'Sign in to AI Mini Chatbot' : 'Create your account'}
          </h1>
          <p className="mt-1 text-xs text-muted-foreground">
            {mode === 'login'
              ? 'Enter your credentials to access your conversations.'
              : 'Sign up to start chatting with AI Assistant.'}
          </p>
        </div>

        <div className="grid grid-cols-2 gap-1 rounded-lg bg-muted p-1">
          <button
            type="button"
            onClick={() => switchMode('login')}
            className={`rounded-md py-1.5 text-xs font-medium transition-colors cursor-pointer ${
              mode === 'login'
                ? 'bg-background text-foreground shadow-xs'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            Sign in
          </button>
          <button
            type="button"
            onClick={() => switchMode('register')}
            className={`rounded-md py-1.5 text-xs font-medium transition-colors cursor-pointer ${
              mode === 'register'
                ? 'bg-background text-foreground shadow-xs'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            Register
          </button>
        </div>

        {displayError && (
          <ErrorState
            message={displayError}
            onDismiss={() => {
              setLocalError(null)
              clearError()
            }}
          />
        )}

        {infoMessage && (
          <div
            role="status"
            className="rounded-lg border border-border bg-muted px-3 py-2 text-xs text-foreground"
          >
            {infoMessage}
          </div>
        )}

        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <label htmlFor="auth-email" className="block text-xs font-medium text-foreground">
              Email
            </label>
            <Input
              id="auth-email"
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={isSubmitting}
              required
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="auth-password" className="block text-xs font-medium text-foreground">
              Password
            </label>
            <Input
              id="auth-password"
              type="password"
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={isSubmitting}
              required
            />
          </div>

          <Button type="submit" className="w-full" disabled={isSubmitting}>
            {isSubmitting
              ? mode === 'login'
                ? 'Signing in...'
                : 'Creating account...'
              : mode === 'login'
                ? 'Sign in'
                : 'Create account'}
          </Button>
        </form>
      </div>
    </div>
  )
}
