import { AuthPage } from '@/components/auth/AuthPage'
import { AppLayout } from '@/components/layout/AppLayout'
import type { AuthUser } from '@/context/auth-context'
import { AuthProvider } from '@/context/AuthContext'
import { ChatProvider } from '@/context/ChatContext'
import { useAuth } from '@/hooks/useAuth'

function AppContent() {
  const { user, isLoading } = useAuth()

  if (isLoading) {
    return (
      <div
        role="status"
        aria-label="Loading DOUBLEU AI"
        className="flex min-h-dvh items-center justify-center bg-background text-sm text-muted-foreground"
      >
        Loading DOUBLEU AI...
      </div>
    )
  }

  if (!user) {
    return <AuthPage />
  }

  return (
    <ChatProvider key={user.id}>
      <AppLayout />
    </ChatProvider>
  )
}

interface AppProps {
  initialUser?: AuthUser | null
  initialToken?: string | null
}

export function App({ initialUser = null, initialToken = null }: AppProps) {
  return (
    <AuthProvider initialUser={initialUser} initialToken={initialToken}>
      <AppContent />
    </AuthProvider>
  )
}

export default App
