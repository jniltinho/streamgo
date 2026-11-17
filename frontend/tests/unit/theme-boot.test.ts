// The first-paint script (public/theme-boot.js): the precedence of the ui-themes spec before any app code runs.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'

const script = readFileSync(join(__dirname, '../../public/theme-boot.js'), 'utf8')

function boot(opts: { user?: string; saved?: string; light?: boolean }): string | undefined {
  const root = document.documentElement
  root.removeAttribute('data-theme')
  root.setAttribute('data-user-theme', opts.user ?? '%STREAMGO_THEME%')
  localStorage.clear()
  if (opts.saved) localStorage.setItem('streamgo.theme', opts.saved)
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('light') && !!opts.light }))
  new Function(script)()
  return root.dataset.theme
}

describe('theme before the first paint', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })
  it('first visit: streamgo-dark, or streamgo-light when the browser prefers light', () => {
    expect(boot({})).toBe('streamgo-dark')
    expect(boot({ light: true })).toBe('streamgo-light')
  })
  it('a choice remembered in the browser wins over the default (guests, before sign-in)', () => {
    expect(boot({ saved: 'matrix', light: true })).toBe('matrix')
  })
  it('the signed-in user’s saved theme, rendered by the server, wins over the browser', () => {
    expect(boot({ user: 'winamp', saved: 'matrix' })).toBe('winamp')
    expect(boot({ user: '', saved: 'matrix' })).toBe('matrix')
  })
})
