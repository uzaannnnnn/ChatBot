import { cn } from '@/utils/cn'

interface BrandMarkProps {
  className?: string
}

export function BrandMark({ className }: BrandMarkProps) {
  return (
    <svg viewBox="0 0 32 32" fill="none" aria-hidden="true" className={cn('h-5 w-5', className)}>
      <path
        d="M5 9.5L9.5 22.5L13.75 13.25L18 22.5L22.5 9.5"
        stroke="currentColor"
        strokeWidth="2.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="26.25" cy="10.5" r="2.25" fill="#38BDF8" />
    </svg>
  )
}
