// The Go server is not part of these tests: every /api/v1 request is answered here (Playwright route interception).
import type { Page } from '@playwright/test'
import type { Me } from '../../src/api/types'

export const ANA: Me = { id: 'u1', name: 'Ana Souza', email: 'ana@example.com', theme: '', language: '' }

export interface StubbedApi {
  /** the bodies of every PATCH /me */
  patches: unknown[]
}

// stubApi answers GET/PATCH /me (401 while signed out), POST /auth/logout, and 404 for anything else.
export async function stubApi(page: Page, me: Me | null = null): Promise<StubbedApi> {
  const state: StubbedApi = { patches: [] }
  let current = me ? { ...me } : null
  const json = (status: number, body: unknown) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
  const error = (status: number, code: string) => json(status, { error: { code, message: code } })
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname.replace('/api/v1', '')
    if (path === '/me' && req.method() === 'GET')
      return route.fulfill(current ? json(200, current) : error(401, 'unauthenticated'))
    if (path === '/me' && req.method() === 'PATCH') {
      if (!current) return route.fulfill(error(401, 'unauthenticated'))
      const patch = req.postDataJSON() as Partial<Me>
      state.patches.push(patch)
      current = { ...current, ...patch }
      return route.fulfill(json(200, current))
    }
    if (path === '/auth/logout' && req.method() === 'POST') {
      current = null
      return route.fulfill({ status: 204 })
    }
    return route.fulfill(error(404, 'not_found'))
  })
  return state
}

// recordFirstTheme stores the theme the document had when <body> first appeared (no frame with another theme).
export async function recordFirstTheme(page: Page): Promise<() => Promise<string>> {
  await page.addInitScript(() => {
    const w = window as unknown as { __firstTheme?: string }
    new MutationObserver((_, obs) => {
      if (document.body) {
        w.__firstTheme = document.documentElement.dataset.theme
        obs.disconnect()
      }
    }).observe(document, { childList: true, subtree: true })
  })
  return () => page.evaluate(() => (window as unknown as { __firstTheme: string }).__firstTheme)
}
