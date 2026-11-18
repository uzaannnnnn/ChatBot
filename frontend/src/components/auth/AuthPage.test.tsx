import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AuthPage } from '@/components/auth/AuthPage'
import { AuthProvider } from '@/context/AuthContext'

describe('AuthPage', () => {
  it('validates email and password before submission', async () => {
    render(
      <AuthProvider>
        <AuthPage />
      </AuthProvider>,
    )

    const submitBtn = screen.getAllByRole('button', { name: /^sign in$/i })[1]
    fireEvent.click(submitBtn)
    expect(await screen.findByRole('alert')).toHaveTextContent(/valid email/i)

    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: 'user@example.com' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: '123' } })
    fireEvent.click(submitBtn)
    expect(await screen.findByRole('alert')).toHaveTextContent(/at least 6 characters/i)
  })

  it('toggles between login and registration modes', () => {
    render(
      <AuthProvider>
        <AuthPage />
      </AuthProvider>,
    )

    expect(screen.getByRole('heading', { name: /sign in to ai mini chatbot/i })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^register$/i }))
    expect(screen.getByRole('heading', { name: /create your account/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create account/i })).toBeInTheDocument()
  })
})
