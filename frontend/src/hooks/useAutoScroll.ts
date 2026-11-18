import { useCallback, useEffect, useRef } from 'react'

export function useAutoScroll<T extends HTMLElement>(dependency: unknown) {
  const containerRef = useRef<T | null>(null)
  const shouldAutoScrollRef = useRef<boolean>(true)

  const handleScroll = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    shouldAutoScrollRef.current = distanceFromBottom < 80
  }, [])

  useEffect(() => {
    const el = containerRef.current
    if (!el || !shouldAutoScrollRef.current) return
    el.scrollTop = el.scrollHeight
  }, [dependency])

  const scrollToBottom = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    shouldAutoScrollRef.current = true
    el.scrollTop = el.scrollHeight
  }, [])

  return { containerRef, handleScroll, scrollToBottom }
}
