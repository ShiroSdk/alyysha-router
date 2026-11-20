// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
import { afterAll, beforeAll, describe, expect, test } from 'bun:test'
import type { Page } from '@playwright/test'
import { startDashboard, type Dashboard } from './harness'

let app: Dashboard
let page: Page
beforeAll(async () => {
  app = await startDashboard(20422)
  page = app.page
}, 60_000)
afterAll(async () => { await app?.stop() })

async function openForm(provider: string, cookie: string, kind: 'browser-login' | 'import-chrome') {
  await page.route(`**/api/providers/${provider}/${kind}`, (route) => route.fulfill({
    json: { provider, cookie, tokens: {}, warning: '' },
  }))
  await page.route('**/api/providers/validate', (route) => route.fulfill({
    json: { supported: false, valid: false },
  }))
  const connections = await page.request.get(`${app.baseURL}/api/connections`)
  const existing = await connections.json()
  for (const connection of existing) {
    if (connection.provider === provider) {
      await page.request.delete(`${app.baseURL}/api/connections/${connection.id}`)
    }
  }
  await page.goto(`${app.baseURL}/dashboard/providers/${provider}`)
  await page.getByRole('button', { name: /Add Connection$/ }).click()
  await page.locator('#connName').fill(`capture-${provider}-${kind}`)
  const action = kind === 'browser-login' ? 'Open a throwaway browser'
    : provider === 'gemini-web' ? 'Read it from an open browser' : 'Import from a signed-in browser'
  const captured = page.waitForResponse((res) => res.url().endsWith(`/api/providers/${provider}/${kind}`))
  await page.getByRole('button', { name: action, exact: true }).click()
  await captured
  await page.getByText(/filled in below|was not captured/).first().waitFor({ timeout: 3000 })
}

describe('dual-cookie capture and submit', () => {
  for (const provider of ['gemini-web', 'grok-web']) {
    for (const kind of ['browser-login', 'import-chrome'] as const) {
      test(`${provider} ${kind} fills fields and preserves named cookies on save`, async () => {
        const header = provider === 'gemini-web'
          ? '__Secure-1PSID=test-primary; __Secure-1PSIDTS=test-secondary'
          : 'sso=test-primary; sso-rw=test-secondary; cf_clearance=test-clearance'
        await openForm(provider, header, kind)
        await page.locator('#connCookiePrimary').waitFor()
        expect(await page.locator('#connCookiePrimary').inputValue()).toBe('test-primary')
        expect(await page.locator('#connCookieSecondary').inputValue()).toContain('test-secondary')
        const saved = page.waitForRequest((req) => req.method() === 'POST' && req.url().endsWith('/api/connections'))
        await page.getByRole('button', { name: 'Save', exact: true }).click()
        const payload = (await saved).postDataJSON()
        expect(payload.apiKey).toBe(header)
        expect(payload.authType).toBe('cookie')
        await page.unrouteAll()
      }, 60_000)
    }
  }
  test('missing secondary keeps form open for manual completion', async () => {
    await openForm('gemini-web', '__Secure-1PSID=test-primary', 'browser-login')
    await page.getByText('The second session cookie was not captured. Paste it manually in the second field.').first().waitFor()
    expect(await page.locator('#connCookieSecondary').inputValue()).toBe('')
    expect(await page.locator('#connName').isVisible()).toBe(true)
    await page.locator('#connCookieSecondary').fill('manual-timestamp')
    const saved = page.waitForRequest((req) => req.method() === 'POST' && req.url().endsWith('/api/connections'))
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    expect((await saved).postDataJSON().apiKey).toBe('__Secure-1PSID=test-primary; __Secure-1PSIDTS=manual-timestamp')
    await page.unrouteAll()
  }, 60_000)
})
