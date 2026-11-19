import { expect, test } from '@playwright/test'

// 1x1 valid PNG pixel for deterministic E2E image rendering without external network calls
const TINY_PNG_BUFFER = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
)

test('DOUBLEU AI v2.0 image generation E2E flow: generate, persist across reload, and handle quota failure', async ({
  page,
}) => {
  const userId = '11111111-1111-1111-1111-111111111111'
  const convId = '33333333-3333-3333-3333-333333333333'
  const signedImageUrl = 'http://localhost:8080/mock-storage/signed/generated-landscape.png'

  const conversations: Array<{
    id: string
    title: string
    created_at: string
    updated_at: string
  }> = [
    {
      id: convId,
      title: 'Visual Studio',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ]

  const messages: Array<{
    id: string
    conversation_id: string
    role: 'user' | 'assistant'
    content: string
    attachments?: Array<{
      id: string
      kind: 'image'
      url: string
      mime_type: string
      alt_text: string
      width: number
      height: number
    }>
    created_at: string
  }> = []

  // Mock Supabase auth login + session refresh
  await page.route('**/auth/v1/**', async (route) => {
    const req = route.request()
    const url = req.url()
    const fakeSession = {
      access_token: 'e2e-access-token',
      token_type: 'bearer',
      expires_in: 3600,
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      refresh_token: 'e2e-refresh-token',
      user: {
        id: userId,
        email: 'creator@doubleu.ai',
        aud: 'authenticated',
        role: 'authenticated',
      },
    }
    if (url.includes('/token') || url.includes('/user')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(url.includes('/user') ? fakeSession.user : fakeSession),
      })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  })

  // Mock private Supabase Storage signed image bytes
  await page.route(signedImageUrl, async (route) => {
    return route.fulfill({
      status: 200,
      contentType: 'image/png',
      body: TINY_PNG_BUFFER,
    })
  })

  // Mock backend API
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const method = req.method()

    if (url.pathname === '/api/v1/conversations' && method === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: conversations,
          pagination: { limit: 50, offset: 0, has_more: false },
        }),
      })
    }

    if (url.pathname === `/api/v1/conversations/${convId}/messages` && method === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: messages,
          pagination: { limit: 100, offset: 0, has_more: false },
        }),
      })
    }

    if (url.pathname === `/api/v1/conversations/${convId}/messages/stream` && method === 'POST') {
      const body = JSON.parse(req.postData() || '{}') as { content: string }
      const now = new Date().toISOString()

      messages.push({
        id: `user-${messages.length + 1}`,
        conversation_id: convId,
        role: 'user',
        content: body.content,
        created_at: now,
      })

      // Quota exhaustion scenario
      if (body.content.toLowerCase().includes('quota')) {
        const failText =
          'Image generation is temporarily rate-limited or quota-exhausted. Please wait a moment and try again.'
        messages.push({
          id: `asst-${messages.length + 1}`,
          conversation_id: convId,
          role: 'assistant',
          content: failText,
          created_at: now,
        })

        const sseBody = [
          `event: message_start\ndata: ${JSON.stringify({ conversation_id: convId, role: 'assistant' })}\n\n`,
          `event: tool_status\ndata: ${JSON.stringify({ tool: 'generate_image', status: 'running' })}\n\n`,
          `event: tool_status\ndata: ${JSON.stringify({ tool: 'generate_image', status: 'failed', message: failText })}\n\n`,
          `event: content_delta\ndata: ${JSON.stringify({ delta: failText })}\n\n`,
          `event: message_complete\ndata: ${JSON.stringify({ status: 'completed', message_id: `asst-${messages.length}` })}\n\n`,
        ].join('')

        return route.fulfill({
          status: 200,
          contentType: 'text/event-stream',
          body: sseBody,
        })
      }

      // Successful image generation scenario
      const attachment = {
        id: 'att-e2e-1',
        kind: 'image' as const,
        url: signedImageUrl,
        mime_type: 'image/png',
        alt_text: 'A serene misty mountain lake at dawn',
        width: 1024,
        height: 1024,
      }
      const caption = 'Here is your misty mountain lake at dawn.'

      messages.push({
        id: 'asst-img-1',
        conversation_id: convId,
        role: 'assistant',
        content: caption,
        attachments: [attachment],
        created_at: now,
      })

      const sseBody = [
        `event: message_start\ndata: ${JSON.stringify({ conversation_id: convId, role: 'assistant' })}\n\n`,
        `event: tool_status\ndata: ${JSON.stringify({ tool: 'generate_image', status: 'running' })}\n\n`,
        `event: tool_status\ndata: ${JSON.stringify({ tool: 'generate_image', status: 'completed' })}\n\n`,
        `event: attachment\ndata: ${JSON.stringify(attachment)}\n\n`,
        `event: content_delta\ndata: ${JSON.stringify({ delta: caption })}\n\n`,
        `event: message_complete\ndata: ${JSON.stringify({ status: 'completed', message_id: 'asst-img-1' })}\n\n`,
      ].join('')

      return route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: sseBody,
      })
    }

    return route.fulfill({ status: 404, body: 'Not found' })
  })

  // 1. Sign in and verify DOUBLEU AI branding
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /sign in to doubleu ai/i })).toBeVisible()
  await page.getByLabel('Email').fill('creator@doubleu.ai')
  await page.getByLabel('Password').fill('password123')
  await page
    .getByRole('button', { name: /^sign in$/i })
    .nth(1)
    .click()

  await expect(page.getByText('Think More. Discover More.').first()).toBeVisible()

  // 2. Open conversation and request an image
  await page.getByRole('button', { name: 'Visual Studio', exact: true }).click()
  const input = page.getByLabel('Message input')
  await input.fill('Generate an image of a serene misty mountain lake at dawn')
  await input.press('Enter')

  // 3. Verify image renders with caption
  const generatedImg = page.getByRole('img', { name: 'A serene misty mountain lake at dawn' })
  await expect(generatedImg).toBeVisible()
  await expect(page.getByText('Here is your misty mountain lake at dawn.')).toBeVisible()

  // 4. Reload page and reopen conversation -> image remains visible
  await page.reload()
  await page.getByRole('button', { name: 'Visual Studio', exact: true }).click()
  await expect(
    page.getByRole('img', { name: 'A serene misty mountain lake at dawn' }),
  ).toBeVisible()

  // 5. Trigger quota exhaustion failure -> truthful error message, no crash
  await page.getByLabel('Message input').fill('Generate another image during quota exhaustion')
  await page.getByLabel('Message input').press('Enter')
  await expect(page.getByText(/temporarily rate-limited or quota-exhausted/i).first()).toBeVisible()
})
