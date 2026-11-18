import { expect, test } from '@playwright/test'

interface MockConversation {
  id: string
  title: string
  created_at: string
  updated_at: string
}

interface MockMessage {
  id: string
  conversation_id: string
  role: 'user' | 'assistant'
  content: string
  created_at: string
}

test('full chat lifecycle: create, stream, refresh, reopen, rename, delete + screenshots', async ({
  page,
}) => {
  const conversations: MockConversation[] = []
  const messagesByConv: Record<string, MockMessage[]> = {}

  await page.route('**/auth/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    const mockUser = {
      id: '11111111-1111-4111-8111-111111111111',
      aud: 'authenticated',
      role: 'authenticated',
      email: 'e2e@example.com',
    }

    if (url.pathname.endsWith('/token') && method === 'POST') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          access_token: 'mock-access-token',
          token_type: 'bearer',
          expires_in: 3600,
          refresh_token: 'mock-refresh-token',
          user: mockUser,
        }),
      })
    }

    if (url.pathname.endsWith('/user') && method === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockUser),
      })
    }

    if (url.pathname.endsWith('/logout') && method === 'POST') {
      return route.fulfill({ status: 204, body: '' })
    }

    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  })

  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname
    const method = req.method()

    if (path === '/api/v1/conversations' && method === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: conversations,
          pagination: { limit: 50, offset: 0, has_more: false },
        }),
      })
    }

    if (path === '/api/v1/conversations' && method === 'POST') {
      const conv: MockConversation = {
        id: '11111111-1111-4111-8111-111111111111',
        title: 'New conversation',
        created_at: '2026-11-18T00:00:00Z',
        updated_at: '2026-11-18T00:00:00Z',
      }
      conversations.unshift(conv)
      messagesByConv[conv.id] = []
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ data: conv }),
      })
    }

    const streamMatch = path.match(/^\/api\/v1\/conversations\/([^/]+)\/messages\/stream$/)
    if (streamMatch && method === 'POST') {
      const convId = streamMatch[1]
      const body = JSON.parse(req.postData() || '{}') as { content?: string }
      const userContent = body.content || ''
      const assistantContent =
        'Here is a concise Go channel example:\n\n```go\nch := make(chan string)\ngo func() { ch <- "hello" }()\nfmt.Println(<-ch)\n```'

      messagesByConv[convId] = messagesByConv[convId] || []
      messagesByConv[convId].push(
        {
          id: '22222222-2222-4222-8222-222222222222',
          conversation_id: convId,
          role: 'user',
          content: userContent,
          created_at: '2026-11-18T00:00:01Z',
        },
        {
          id: '33333333-3333-4333-8333-333333333333',
          conversation_id: convId,
          role: 'assistant',
          content: assistantContent,
          created_at: '2026-11-18T00:00:02Z',
        },
      )

      const sseBody = [
        `event: message_start\ndata: ${JSON.stringify({ conversation_id: convId, role: 'assistant' })}\n\n`,
        `event: content_delta\ndata: ${JSON.stringify({ delta: 'Here is a concise Go channel example:\n\n' })}\n\n`,
        `event: content_delta\ndata: ${JSON.stringify({ delta: '```go\nch := make(chan string)\ngo func() { ch <- "hello" }()\nfmt.Println(<-ch)\n```' })}\n\n`,
        `event: message_complete\ndata: ${JSON.stringify({ status: 'completed', message_id: '33333333-3333-4333-8333-333333333333' })}\n\n`,
      ].join('')

      return route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
        },
        body: sseBody,
      })
    }

    const msgsMatch = path.match(/^\/api\/v1\/conversations\/([^/]+)\/messages$/)
    if (msgsMatch && method === 'GET') {
      const convId = msgsMatch[1]
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: messagesByConv[convId] || [],
          pagination: { limit: 100, offset: 0, has_more: false },
        }),
      })
    }

    const convMatch = path.match(/^\/api\/v1\/conversations\/([^/]+)$/)
    if (convMatch && method === 'PATCH') {
      const convId = convMatch[1]
      const body = JSON.parse(req.postData() || '{}') as { title: string }
      const idx = conversations.findIndex((c) => c.id === convId)
      if (idx !== -1) {
        conversations[idx] = { ...conversations[idx], title: body.title }
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: conversations[idx] }),
        })
      }
    }

    if (convMatch && method === 'DELETE') {
      const convId = convMatch[1]
      const idx = conversations.findIndex((c) => c.id === convId)
      if (idx !== -1) {
        conversations.splice(idx, 1)
        delete messagesByConv[convId]
      }
      return route.fulfill({ status: 204, body: '' })
    }

    return route.fulfill({ status: 404, body: 'Not found' })
  })

  // 1. Open application on desktop and sign in
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/')

  await page.getByLabel('Email').fill('e2e@example.com')
  await page.getByLabel('Password').fill('password123')
  await page
    .getByRole('button', { name: /^sign in$/i })
    .nth(1)
    .click()
  await expect(page.getByText('e2e@example.com')).toBeVisible()

  // 2. Create conversation
  await page.getByRole('button', { name: 'New conversation' }).first().click()
  await expect(page.getByRole('button', { name: /rename new conversation/i })).toBeVisible()

  // 3. Send message
  const input = page.getByLabel('Message input')
  await input.fill('Show me a Go channel example')
  await input.press('Enter')

  // 4. Observe streaming assistant response
  await expect(page.getByText('Show me a Go channel example')).toBeVisible()
  await expect(page.getByText('Here is a concise Go channel example:')).toBeVisible()
  await expect(page.getByText('ch := make(chan string)')).toBeVisible()

  // Capture desktop screenshot
  await page.screenshot({
    path: '/Users/zannho/.gemini/antigravity/brain/be827462-378c-4913-9a63-aa386a482787/desktop-chat.png',
    fullPage: true,
  })

  // 5. Refresh page
  await page.reload()

  // 6. Reopen conversation & 7. Verify message history
  await page.getByRole('button', { name: 'New conversation' }).nth(1).click()
  await expect(page.getByText('Show me a Go channel example')).toBeVisible()
  await expect(page.getByText('Here is a concise Go channel example:')).toBeVisible()

  // Capture mobile screenshot
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByLabel('Conversation history')).toBeHidden()
  await page.screenshot({
    path: '/Users/zannho/.gemini/antigravity/brain/be827462-378c-4913-9a63-aa386a482787/mobile-chat.png',
    fullPage: true,
  })

  // Restore desktop viewport for rename & delete
  await page.setViewportSize({ width: 1280, height: 800 })

  // 8. Rename conversation
  await page.getByRole('button', { name: /rename new conversation/i }).click()
  const editInput = page.getByLabel('Edit conversation title')
  await editInput.fill('Go Channels Guide')
  await editInput.press('Enter')
  await expect(page.getByRole('heading', { name: 'Go Channels Guide' })).toBeVisible()

  // 9. Delete conversation
  await page.getByRole('button', { name: /delete go channels guide/i }).click()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'New chat' })).toBeVisible()

  // 10. Sign out
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByRole('heading', { name: /sign in to ai mini chatbot/i })).toBeVisible()
})
