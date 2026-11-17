// Portions adapted from MeetHub (MIT): frontend/tests/unit/client.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, setUnauthorizedHandler } from '@/api/client'

function reply(status: number, body: unknown, headers: Record<string, string> = {}) {
  return vi.fn().mockResolvedValue(
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json', ...headers },
    }),
  )
}

describe('api client', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setUnauthorizedHandler(() => {})
  })

  it('sends JSON with the session cookie, and no body on GET', async () => {
    const f = reply(200, { ok: true })
    vi.stubGlobal('fetch', f)
    await api('PATCH', '/me', { theme: 'matrix' })
    await api('GET', '/me')
    const [url, patch] = f.mock.calls[0]!
    const [, get] = f.mock.calls[1]!
    expect(url).toBe('/api/v1/me')
    expect(patch.headers['Content-Type']).toBe('application/json')
    expect(patch.body).toBe('{"theme":"matrix"}')
    expect(patch.credentials).toBe('same-origin')
    expect(get.body).toBeUndefined()
  })

  it('reads the error envelope when the body is not JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('<html>', { status: 502, statusText: 'Bad Gateway' })),
    )
    await expect(api('GET', '/me')).rejects.toMatchObject({ status: 502, code: 'internal' })
  })

  it('turns error responses into ApiError with the code and Retry-After', async () => {
    vi.stubGlobal(
      'fetch',
      reply(429, { error: { code: 'rate_limited', message: 'Too many.' } }, { 'Retry-After': '30' }),
    )
    const err = await api('POST', '/auth/login', {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 429, code: 'rate_limited', retryAfter: 30 })
  })

  it('reports an ended session to the unauthorized handler', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    vi.stubGlobal('fetch', reply(401, { error: { code: 'session_expired', message: 'Expired.' } }))
    await expect(api('GET', '/me')).rejects.toMatchObject({ code: 'session_expired' })
    expect(handler).toHaveBeenCalledWith('session_expired')
  })

  it('does not treat a failed login as an ended session', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    vi.stubGlobal('fetch', reply(401, { error: { code: 'invalid_credentials', message: 'Bad.' } }))
    await expect(api('POST', '/auth/login', {})).rejects.toBeInstanceOf(ApiError)
    expect(handler).not.toHaveBeenCalled()
  })

  it('maps network failures to the network code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await expect(api('GET', '/me')).rejects.toMatchObject({ status: 0, code: 'network' })
  })
})
