export function StreamingIndicator() {
  return (
    <span
      role="status"
      aria-label="Generating response"
      className="inline-flex items-center gap-1 py-1 text-muted-foreground"
    >
      <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
      <span
        className="h-1.5 w-1.5 animate-pulse rounded-full bg-current"
        style={{ animationDelay: '150ms' }}
      />
      <span
        className="h-1.5 w-1.5 animate-pulse rounded-full bg-current"
        style={{ animationDelay: '300ms' }}
      />
    </span>
  )
}
