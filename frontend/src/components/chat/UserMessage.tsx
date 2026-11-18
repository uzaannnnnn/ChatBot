import React from 'react'
import type { Message } from '@/types/chat'

interface UserMessageProps {
  message: Message
}

export const UserMessage = React.memo(function UserMessage({ message }: UserMessageProps) {
  return (
    <div className="flex w-full justify-end">
      <div
        className="max-w-[85%] rounded-2xl rounded-tr-sm bg-primary px-4 py-2.5 text-sm leading-relaxed text-primary-foreground shadow-xs sm:max-w-[75%]"
        data-testid="user-message"
      >
        <p className="whitespace-pre-wrap break-words">{message.content}</p>
      </div>
    </div>
  )
})
