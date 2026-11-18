import { createContext } from 'react'

export interface AuthUser {
  id: string
  email?: string
}

export interface AuthContextValue {
  user: AuthUser | null
  accessToken: string | null
  isLoading: boolean
  error: string | null
  clearError: () => void
  signIn: (email: string, password: string) => Promise<void>
  signUp: (email: string, password: string) => Promise<string | null>
  signOut: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)
