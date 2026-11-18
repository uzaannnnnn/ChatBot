import { useState } from 'react'
import { ChatContainer } from '@/components/chat/ChatContainer'
import { ChatHeader } from '@/components/layout/ChatHeader'
import { Sidebar } from '@/components/layout/Sidebar'

export function AppLayout() {
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-background text-foreground">
      <Sidebar isOpenMobile={mobileSidebarOpen} onCloseMobile={() => setMobileSidebarOpen(false)} />
      <div className="flex min-w-0 flex-1 flex-col">
        <ChatHeader onOpenMobileSidebar={() => setMobileSidebarOpen(true)} />
        <ChatContainer />
      </div>
    </div>
  )
}
