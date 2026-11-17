// A fake of the routes the sign-in, broadcasts and destinations pages call, built to "API shapes" in design.md:
// every /api/v1 request is answered here with an in-memory state the tests read and change.
import type { Page, Route } from '@playwright/test'
import type { Broadcast, BroadcastDetail, Destination, Me } from '../../src/api/types'

export const PASSWORD = 'correct horse battery staple'

export interface FakeApi {
  me: Me | null
  user: Me
  broadcasts: Broadcast[]
  destinations: Destination[]
  /** destination ids the fake answers 409 for (in use) */
  inUse: Set<string>
  /** answer POST /auth/login with 429 */
  rateLimited: boolean
  /** every request body, keyed "METHOD /path" */
  bodies: { route: string; body: unknown }[]
}

const offline = { state: 'offline', reason: '', started_at: null, deadline: null } as const
let seq = 0
const ulid = () => `01JTEST${String(++seq).padStart(19, '0')}`

export function broadcast(title: string, state: Broadcast['output']['state'] = 'offline'): Broadcast {
  return { id: ulid(), title, output: { ...offline, state } }
}

function detail(b: Broadcast): BroadcastDetail {
  return {
    ...b,
    invite_url: `http://127.0.0.1/join/tok-${b.id}`,
    stage: { v: 1, on_air: false, layout: 'grid', sources: [], banner: null, logo: null, accent: '#3b46d8' },
    destinations: [],
  }
}

export async function fakeApi(page: Page, user: Me, signedIn: boolean, seed: Partial<FakeApi> = {}): Promise<FakeApi> {
  const api: FakeApi = {
    me: signedIn ? user : null,
    user,
    broadcasts: [],
    destinations: [],
    inUse: new Set(),
    rateLimited: false,
    bodies: [],
    ...seed,
  }
  const json = (route: Route, status: number, body?: unknown) =>
    route.fulfill(
      body === undefined ? { status } : { status, contentType: 'application/json', body: JSON.stringify(body) },
    )
  const error = (route: Route, status: number, code: string, message = code) =>
    json(route, status, { error: { code, message } })

  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname.replace('/api/v1', '')
    const m = req.method()
    const body = req.postData() ? req.postDataJSON() : undefined
    if (m !== 'GET') api.bodies.push({ route: `${m} ${path}`, body })

    if (path === '/auth/login' && m === 'POST') {
      if (api.rateLimited) return error(route, 429, 'rate_limited', 'Too many attempts.')
      if (body.email !== api.user.email || body.password !== PASSWORD)
        return error(route, 401, 'invalid_credentials', 'Invalid e-mail or password.')
      api.me = api.user
      return json(route, 200, api.me)
    }
    if (path === '/auth/logout' && m === 'POST') {
      api.me = null
      return json(route, 204)
    }
    if (!api.me) return error(route, 401, 'unauthenticated')

    if (path === '/me' && m === 'GET') return json(route, 200, api.me)
    if (path === '/me' && m === 'PATCH') {
      api.me = api.user = { ...api.me, ...body }
      return json(route, 200, api.me)
    }

    if (path === '/broadcasts' && m === 'GET') return json(route, 200, { broadcasts: api.broadcasts })
    if (path === '/broadcasts' && m === 'POST') {
      const b = broadcast(body.title)
      api.broadcasts.unshift(b)
      return json(route, 201, detail(b))
    }
    const bm = path.match(/^\/broadcasts\/([^/]+)$/)
    const b = bm && api.broadcasts.find((x) => x.id === bm[1])
    if (bm && !b) return error(route, 404, 'not_found')
    if (b && m === 'GET') return json(route, 200, detail(b))
    if (b && m === 'PATCH') {
      b.title = body.title
      return json(route, 200, detail(b))
    }
    if (b && m === 'DELETE') {
      if (b.output.state !== 'offline') return error(route, 409, 'conflict', 'The broadcast is live.')
      api.broadcasts = api.broadcasts.filter((x) => x !== b)
      return json(route, 204)
    }

    if (path === '/destinations' && m === 'GET') return json(route, 200, { destinations: api.destinations })
    if (path === '/destinations' && m === 'POST') {
      const u = new URL(body.address)
      const d = { id: ulid(), name: body.name, scheme: u.protocol.slice(0, -1), host: u.host, has_key: !!body.key }
      api.destinations.push(d)
      return json(route, 201, d)
    }
    const dm = path.match(/^\/destinations\/([^/]+)$/)
    const d = dm && api.destinations.find((x) => x.id === dm[1])
    if (dm && !d) return error(route, 404, 'not_found')
    if (d && api.inUse.has(d.id)) return error(route, 409, 'conflict', 'The destination is in use.')
    if (d && m === 'PATCH') {
      if (body.name !== undefined) d.name = body.name
      if (body.address !== undefined) {
        const u = new URL(body.address)
        Object.assign(d, { scheme: u.protocol.slice(0, -1), host: u.host })
      }
      if (body.key !== undefined) d.has_key = body.key !== ''
      return json(route, 200, d)
    }
    if (d && m === 'DELETE') {
      api.destinations = api.destinations.filter((x) => x !== d)
      return json(route, 204)
    }
    return error(route, 404, 'not_found')
  })
  return api
}
