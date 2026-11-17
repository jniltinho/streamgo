// Portions adapted from MeetHub (MIT): frontend/src/api/client.ts
// Typed client for /api/v1: JSON, same-origin cookies, and errors as ApiError with the server's stable code from the
// error envelope { "error": { "code", "message" } }. The server checks the Origin header of state-changing requests,
// which the browser sets by itself: there is no token to send.
import type { ErrorEnvelope } from './types'

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly retryAfter?: number,
    /** the input field at fault, for invalid_input */
    public readonly field?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

let onUnauthorized: ((code: string) => void) | null = null

// setUnauthorizedHandler is called when an authenticated call answers 401 (session ended).
export function setUnauthorizedHandler(fn: (code: string) => void): void {
  onUnauthorized = fn
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const unsafe = method !== 'GET' && method !== 'HEAD'
  const form = body instanceof FormData // the logo upload: the browser sets the multipart type
  if (unsafe && !form) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: 'same-origin',
      body: !unsafe ? undefined : form ? body : JSON.stringify(body ?? {}),
    })
  } catch {
    throw new ApiError(0, 'network', 'The server could not be reached.')
  }
  if (res.status === 204) return undefined as T
  const data: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const err = (data as Partial<ErrorEnvelope> | null)?.error
    const code = err?.code ?? 'internal'
    const retry = Number(res.headers.get('Retry-After')) || undefined
    if (res.status === 401 && path !== '/auth/login' && onUnauthorized) onUnauthorized(code)
    throw new ApiError(res.status, code, err?.message ?? res.statusText, retry, err?.field)
  }
  return data as T
}
