import { useState } from 'react'
import { Download, Expand, ImageOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { MessageAttachment } from '@/types/chat'

interface AttachmentGalleryProps {
  attachments: MessageAttachment[]
}

function fileExtensionForMime(mimeType: string): string {
  if (mimeType === 'image/png') return 'png'
  if (mimeType === 'image/webp') return 'webp'
  return 'jpg'
}

async function triggerDownload(attachment: MessageAttachment) {
  const filename = `doubleu-ai-${attachment.id.slice(0, 8)}.${fileExtensionForMime(attachment.mime_type)}`
  try {
    const response = await fetch(attachment.url)
    if (!response.ok) {
      throw new Error('download failed')
    }
    const blob = await response.blob()
    const objectUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    link.download = filename
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(objectUrl)
  } catch {
    // Fallback to direct anchor navigation when cross-origin fetch is restricted
    const link = document.createElement('a')
    link.href = attachment.url
    link.download = filename
    link.target = '_blank'
    link.rel = 'noopener noreferrer'
    document.body.appendChild(link)
    link.click()
    link.remove()
  }
}

function ImageAttachmentCard({
  attachment,
  onOpenLightbox,
}: {
  attachment: MessageAttachment
  onOpenLightbox: (att: MessageAttachment) => void
}) {
  const [isLoaded, setIsLoaded] = useState(false)
  const [hasError, setHasError] = useState(!attachment.url)

  const altText = attachment.alt_text || 'Generated image'

  if (hasError) {
    return (
      <div
        role="alert"
        data-testid="attachment-error"
        className="flex flex-col items-center justify-center gap-2 rounded-xl border border-border bg-muted/40 p-6 text-center text-xs text-muted-foreground"
      >
        <ImageOff className="h-6 w-6 text-muted-foreground" aria-hidden="true" />
        <p className="font-medium text-foreground">Unable to load generated image</p>
        {attachment.alt_text && (
          <p className="max-w-xs text-muted-foreground">{attachment.alt_text}</p>
        )}
      </div>
    )
  }

  return (
    <figure
      data-testid="image-attachment"
      className="group relative overflow-hidden rounded-xl border border-border bg-muted/30 shadow-xs"
    >
      {!isLoaded && (
        <div
          data-testid="attachment-loading"
          aria-label="Loading image"
          className="h-64 w-full animate-pulse bg-muted"
        />
      )}
      <button
        type="button"
        onClick={() => onOpenLightbox(attachment)}
        aria-label={`Inspect image: ${altText}`}
        className="block w-full cursor-zoom-in text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <img
          src={attachment.url}
          alt={altText}
          width={attachment.width > 0 ? attachment.width : undefined}
          height={attachment.height > 0 ? attachment.height : undefined}
          loading="lazy"
          onLoad={() => setIsLoaded(true)}
          onError={() => {
            setIsLoaded(true)
            setHasError(true)
          }}
          className={`max-h-[420px] w-full object-contain bg-muted/20 transition-opacity duration-200 ${
            isLoaded ? 'opacity-100' : 'h-0 opacity-0'
          }`}
        />
      </button>

      <div className="flex items-center justify-between gap-2 border-t border-border/80 bg-card/90 px-3 py-2">
        <figcaption className="truncate text-xs text-muted-foreground" title={altText}>
          {altText}
        </figcaption>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => onOpenLightbox(attachment)}
            aria-label="Open full-size preview"
            title="Preview"
          >
            <Expand className="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => void triggerDownload(attachment)}
            aria-label="Download image"
            title="Download"
          >
            <Download className="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </figure>
  )
}

export function AttachmentGallery({ attachments }: AttachmentGalleryProps) {
  const [activeAttachment, setActiveAttachment] = useState<MessageAttachment | null>(null)

  const imageAttachments = attachments.filter((a) => a.kind === 'image')
  if (imageAttachments.length === 0) {
    return null
  }

  return (
    <>
      <div className="mt-3 grid max-w-lg grid-cols-1 gap-3">
        {imageAttachments.map((att) => (
          <ImageAttachmentCard key={att.id} attachment={att} onOpenLightbox={setActiveAttachment} />
        ))}
      </div>

      <Dialog
        open={activeAttachment !== null}
        onOpenChange={(open) => {
          if (!open) setActiveAttachment(null)
        }}
      >
        <DialogContent className="max-w-3xl p-4 sm:p-6">
          <DialogHeader>
            <DialogTitle className="pr-8 text-sm">Generated Image</DialogTitle>
            <DialogDescription className="max-h-28 overflow-y-auto whitespace-pre-wrap break-words text-xs">
              {activeAttachment?.alt_text || 'DOUBLEU AI generated image'}
            </DialogDescription>
          </DialogHeader>
          {activeAttachment && (
            <div className="space-y-3">
              <div className="overflow-hidden rounded-lg border border-border bg-muted/30">
                <img
                  src={activeAttachment.url}
                  alt={activeAttachment.alt_text || 'Generated image'}
                  className="max-h-[70vh] w-full object-contain"
                />
              </div>
              <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>
                  {activeAttachment.width > 0 && activeAttachment.height > 0
                    ? `${activeAttachment.width} × ${activeAttachment.height} · ${activeAttachment.mime_type}`
                    : activeAttachment.mime_type}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void triggerDownload(activeAttachment)}
                >
                  <Download className="h-3.5 w-3.5" aria-hidden="true" />
                  Download
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
