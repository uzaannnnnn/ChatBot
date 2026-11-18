import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthPage } from '@/components/auth/AuthPage'
import { AuthProvider } from '@/context/AuthContext'
import { useAuth } from '@/hooks/useAuth'

const { mockGetSession, mockOnAuthStateChange, mockSignUp, mockSignInWithPassword, mockSignOut } =
  vi.hoisted(() => ({
    mockGetSession: vi.fn(),
    mockOnAuthStateChange: vi.fn(),
    mockSignUp: vi.fn(),
    mockSignInWithPassword: vi.fn(),
    mockSignOut: vi.fn(),
  }))

vi.mock('@/lib/supabase', () => ({
  isSupabaseConfigured: true,
  supabase: {
    auth: {
      getSession: mockGetSession,
      onAuthStateChange: mockOnAuthStateChange,
      signUp: mockSignUp,
      signInWithPassword: mockSignInWithPassword,
      signOut: mockSignOut,
      refreshSession: vi.fn(),
    },
  },
}))

function AuthProbe() {
  const { user, accessToken, isLoading, error } = useAuth()
  if (isLoading) return <div>Loading session...</div>
  if (user) {
    return (
      <div>
        <div data-testid="authed-email">{user.email}</div>
        <div data-testid="has-token">{accessToken ? 'yes' : 'no'}</div>
      </div>
    )
  }
  return (
    <div>
      {error && <div role="alert">{error}</div>}
      <AuthPage />
    </div>
  )
}

describe('AuthContext email confirmation flow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.replaceState(null, '', '/')
    mockGetSession.mockResolvedValue({ data: { session: null }, error: null })
    mockOnAuthStateChange.mockReturnValue({
      data: { subscription: { unsubscribe: vi.fn() } },
    })
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/')
  })

  it('passes production emailRedirectTo to supabase.auth.signUp', async () => {
    vi.stubEnv('VITE_SITE_URL', 'https://doubleu-ai.eshace.com')
    mockSignUp.mockResolvedValue({ data: { user: { id: 'u1' }, session: null }, error: null })

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )

    await waitFor(() => expect(screen.queryByText('Loading session...')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /^register$/i }))
    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'new@example.com' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'password123' } })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))

    await waitFor(() => {
      expect(mockSignUp).toHaveBeenCalledWith({
        email: 'new@example.com',
        password: 'password123',
        options: { emailRedirectTo: 'https://doubleu-ai.eshace.com' },
      })
    })

    expect(await screen.findByRole('status')).toHaveTextContent(/check your email to confirm/i)
  })

  it('passes local development emailRedirectTo when VITE_SITE_URL is unset', async () => {
    vi.stubEnv('VITE_SITE_URL', '')
    vi.spyOn(window, 'location', 'get').mockReturnValue({
      ...window.location,
      origin: 'http://localhost:5173',
      search: '',
      hash: '',
      pathname: '/',
    })
    mockSignUp.mockResolvedValue({ data: { user: { id: 'u1' }, session: null }, error: null })

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )

    await waitFor(() => expect(screen.queryByText('Loading session...')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /^register$/i }))
    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'dev@example.com' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'password123' } })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))

    await waitFor(() => {
      expect(mockSignUp).toHaveBeenCalledWith({
        email: 'dev@example.com',
        password: 'password123',
        options: { emailRedirectTo: 'http://localhost:5173' },
      })
    })
  })

  it('establishes session from confirmation callback, cleans URL after getSession, and leaks no tokens in logs', async () => {
    const secretAccess = 'eyJhbGciOiJIUzI1NiJ9.secret-access-token.sig'
    const secretRefresh = 'secret-refresh-token-value'
    window.history.replaceState(
      null,
      '',
      `/#access_token=${secretAccess}&refresh_token=${secretRefresh}&expires_in=3600&token_type=bearer&type=signup`,
    )

    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const infoSpy = vi.spyOn(console, 'info').mockImplementation(() => {})
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const replaceSpy = vi.spyOn(window.history, 'replaceState')

    const callOrder: string[] = []
    mockGetSession.mockImplementation(async () => {
      callOrder.push('getSession')
      // Hash must still be intact while Supabase processes the callback
      expect(window.location.hash).toContain('access_token=')
      return {
        data: {
          session: {
            access_token: secretAccess,
            refresh_token: secretRefresh,
            user: { id: 'user-123', email: 'confirmed@example.com' },
          },
        },
        error: null,
      }
    })

    replaceSpy.mockImplementation((_data, _unused, url) => {
      callOrder.push('replaceState')
      History.prototype.replaceState.call(window.history, _data, _unused, url)
    })

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )

    expect(await screen.findByTestId('authed-email')).toHaveTextContent('confirmed@example.com')
    expect(screen.getByTestId('has-token')).toHaveTextContent('yes')

    expect(callOrder).toEqual(['getSession', 'replaceState'])
    expect(window.location.hash).toBe('')
    expect(window.location.href).not.toContain(secretAccess)
    expect(window.location.href).not.toContain(secretRefresh)

    for (const spy of [logSpy, infoSpy, warnSpy, errorSpy]) {
      const output = spy.mock.calls.flat().join(' ')
      expect(output).not.toContain(secretAccess)
      expect(output).not.toContain(secretRefresh)
    }
  })

  it('persists authenticated session across remounts after callback cleanup', async () => {
    const persistedSession = {
      access_token: 'persisted-jwt-token',
      refresh_token: 'persisted-refresh-token',
      user: { id: 'user-456', email: 'persisted@example.com' },
    }
    mockGetSession.mockResolvedValue({ data: { session: persistedSession }, error: null })

    const { unmount } = render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('authed-email')).toHaveTextContent('persisted@example.com')
    unmount()

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('authed-email')).toHaveTextContent('persisted@example.com')
  })

  it('surfaces expired confirmation link error and cleans error params from URL', async () => {
    window.history.replaceState(
      null,
      '',
      '/#error=access_denied&error_code=otp_expired&error_description=Email+link+is+invalid+or+has+expired',
    )
    mockGetSession.mockResolvedValue({ data: { session: null }, error: null })

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    )

    const alerts = await screen.findAllByRole('alert')
    expect(alerts[0]).toHaveTextContent(/expired/i)
    expect(window.location.hash).toBe('')
  })
})
