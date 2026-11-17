// Portions adapted from MeetHub (MIT): frontend/tests/unit/session.test.ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSession } from '@/stores/session'
import { locale } from '@/i18n'
import type { Me } from '@/api/types'

const me: Me = { id: 'u1', name: 'Ana', email: 'ana@example.com', theme: '', language: '' }
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })

describe('session', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
    locale.value = 'en'
  })

  it('applies the theme and the language saved in the user preferences (they follow the user)', async () => {
    localStorage.setItem('streamgo.theme', 'matrix')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ ...me, theme: 'streamgo-light', language: 'pt-BR' })))
    expect(await useSession().loadMe()).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('streamgo-light')
    expect(localStorage.getItem('streamgo.theme')).toBe('streamgo-light')
    expect(locale.value).toBe('pt-BR')
  })

  it('ignores a theme id the catalog does not know', async () => {
    document.documentElement.dataset.theme = 'winamp'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ ...me, theme: 'gone' })))
    await useSession().loadMe()
    expect(document.documentElement.dataset.theme).toBe('winamp')
  })

  it('is signed out when GET /me answers 401', async () => {
    const body = { error: { code: 'unauthenticated', message: 'x' } }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(body, 401)))
    const s = useSession()
    expect(await s.loadMe()).toBe(false)
    expect(s.loggedIn).toBe(false)
  })
})

describe('logout', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('keeps the local session when the server could not revoke it', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const s = useSession()
    s.me = me
    await expect(s.logout()).rejects.toMatchObject({ code: 'network' })
    expect(s.loggedIn).toBe(true)
  })

  it('clears after the server revoked the session, back to the default theme', async () => {
    localStorage.setItem('streamgo.theme', 'matrix')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })))
    const s = useSession()
    s.me = me
    await s.logout()
    expect(s.loggedIn).toBe(false)
    expect(localStorage.getItem('streamgo.theme')).toBeNull()
    expect(document.documentElement.dataset.theme).toBe('streamgo-dark')
  })

  it('clears when the server says the session already ended', async () => {
    const body = JSON.stringify({ error: { code: 'unauthenticated', message: 'x' } })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, { status: 401 })))
    const s = useSession()
    s.me = me
    await s.logout()
    expect(s.loggedIn).toBe(false)
  })
})
