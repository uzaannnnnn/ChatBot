import { expect, test } from '@playwright/test'

const MOCK_USER = {
  id: '11111111-1111-4111-8111-111111111111',
  aud: 'authenticated',
  role: 'authenticated',
  email: 'confirmed@example.com',
}

test.describe('Registration & email confirmation callback', () => {
  test('registration sends active origin as redirect_to instead of localhost:3000', async ({
    page,
  }) => {
    let capturedRedirectTo: string | null = null

    await page.route('**/auth/v1/**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/signup') && route.request().method() === 'POST') {
        capturedRedirectTo = url.searchParams.get('redirect_to')
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            user: { ...MOCK_USER, email: 'newuser@example.com' },
            session: null,
          }),
        })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })

    await page.goto('/')
    await page.getByRole('button', { name: /^register$/i }).click()
    await page.getByLabel('Email').fill('newuser@example.com')
    await page.getByLabel('Password').fill('password123')
    await page.getByRole('button', { name: /create account/i }).click()

    await expect(page.getByRole('status')).toHaveText(/check your email to confirm/i)
    expect(capturedRedirectTo).toBe('http://localhost:5173')
    expect(capturedRedirectTo).not.toContain('localhost:3000')
  })

  test('processes signup confirmation hash, persists session, cleans URL, and leaks no tokens', async ({
    page,
  }) => {
    const consoleMessages: string[] = []
    page.on('console', (msg) => {
      consoleMessages.push(msg.text())
    })

    await page.route('**/auth/v1/**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/user') && route.request().method() === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_USER),
        })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })

    await page.route('**/api/v1/**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname === '/api/v1/conversations' && route.request().method() === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [],
            pagination: { limit: 50, offset: 0, has_more: false },
          }),
        })
      }
      return route.fulfill({ status: 404, body: 'Not found' })
    })

    const secretAccessToken = 'e2e-secret-access-token'
    const secretRefreshToken = 'e2e-secret-refresh-token'

    await page.goto(
      `/#access_token=${secretAccessToken}&refresh_token=${secretRefreshToken}&expires_in=3600&token_type=bearer&type=signup`,
    )

    await expect(page.getByText('confirmed@example.com')).toBeVisible()
    await expect.poll(() => page.url()).toBe('http://localhost:5173/')

    // Session persists across reload after URL cleanup
    await page.reload()
    await expect(page.getByText('confirmed@example.com')).toBeVisible()

    const combinedLogs = consoleMessages.join('\n')
    expect(combinedLogs).not.toContain(secretAccessToken)
    expect(combinedLogs).not.toContain(secretRefreshToken)
  })

  test('displays error banner for expired confirmation link and cleans error hash', async ({
    page,
  }) => {
    await page.goto(
      '/#error=access_denied&error_code=otp_expired&error_description=Email+link+is+invalid+or+has+expired',
    )

    await expect(page.getByRole('alert')).toHaveText(/expired/i)
    await expect.poll(() => page.url()).toBe('http://localhost:5173/')
  })
})
