// The WHIP client of design D3 (src/core/whip.ts) in a real Chrome. The server is stood in for by a fetch
// replacement whose answers come from a second RTCPeerConnection in the page, so the connection really forms.
// The proxy and MediaMTX themselves are package D's and the integration's.
import { expect, test, type Page } from '@playwright/test'

const run = <T>(page: Page, fn: string) =>
  page.evaluate(`(async () => { const L = window.lab; ${fn} })()`) as Promise<T>

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/e2e/fixtures/core-lab.html')
  await page.waitForFunction(() => (window as unknown as { labReady?: boolean }).labReady)
  await page.mouse.click(10, 10) // a user gesture, for the audio context
  await run(
    page,
    `
    window.calls = []
    window.answer = { status: 201 }
    let n = 0
    window.fetch = async (url, init = {}) => {
      window.calls.push({ url: String(url), method: init.method, credentials: init.credentials,
        type: init.headers?.['Content-Type'], body: init.body })
      if (init.method !== 'POST') return new Response(null, { status: 200 })
      if (window.answer.status !== 201)
        return new Response(JSON.stringify({ error: { code: 'conflict', message: 'not live' } }), { status: window.answer.status })
      const pc = new RTCPeerConnection()
      await pc.setRemoteDescription({ type: 'offer', sdp: init.body })
      await pc.setLocalDescription(await pc.createAnswer())
      await new Promise((r) => pc.iceGatheringState === 'complete' ? r() : pc.addEventListener('icegatheringstatechange', () => pc.iceGatheringState === 'complete' && r()))
      ;(window.answerers ||= []).push(pc)
      return new Response(pc.localDescription.sdp, { status: 201,
        headers: { Location: '/api/v1/broadcasts/b1/program/whip/sessions/s' + ++n } })
    }
    const canvas = Object.assign(document.createElement('canvas'), { width: 1280, height: 720 })
    window.comp = new L.Compositor(canvas, 'program')
    window.mix = new L.AudioMix(true)
    await window.mix.resume()
    window.states = []
    window.publisher = (codec) => new L.WhipPublisher({ broadcastId: 'b1', codec, maxBitrate: 8000000,
      video: window.comp.stream.getVideoTracks()[0], audio: window.mix.programStream.getAudioTracks()[0],
      audioRunning: () => window.mix.running, onState: (s, e) => window.states.push(e ? s + ':' + e.message : s) })
  `,
  )
})

// the m= section of an SDP for a kind
const section = (sdp: string, kind: string) => sdp.split(/(?=^m=)/m).find((s) => s.startsWith(`m=${kind}`)) ?? ''
const codecs = (sec: string) => [...sec.matchAll(/^a=rtpmap:\d+ ([^/]+)\//gm)].map((m) => m[1])

for (const codec of ['vp8', 'h264'] as const)
  test(`publishes with ${codec} and Opus only, candidates in the offer, and ends with DELETE`, async ({ page }) => {
    const r = await run<{
      calls: { url: string; method: string; credentials: string; type: string; body: string }[]
      state: string
      params: RTCRtpSendParameters
    }>(
      page,
      `
      const p = window.publisher('${codec}')
      await p.start()
      for (let i = 0; i < 100 && p.pc.connectionState !== 'connected'; i++) await new Promise((r) => setTimeout(r, 100))
      const state = p.pc.connectionState
      const params = p.pc.getSenders().find((s) => s.track.kind === 'video').getParameters()
      await p.stop()
      return { calls: window.calls, state, params: JSON.parse(JSON.stringify(params)) }
    `,
    )
    const [post, del] = r.calls
    expect(post).toMatchObject({
      url: '/api/v1/broadcasts/b1/program/whip',
      method: 'POST',
      credentials: 'same-origin',
      type: 'application/sdp',
    })
    const video = section(post!.body, 'video')
    const audio = section(post!.body, 'audio')
    expect(new Set(codecs(video))).toEqual(new Set([codec === 'h264' ? 'H264' : 'VP8']))
    expect(new Set(codecs(audio))).toEqual(new Set(['opus']))
    if (codec === 'h264') expect(video).toMatch(/packetization-mode=1/)
    expect(post!.body).toMatch(/^a=candidate:/m) // gathered before the offer was sent
    expect(r.state).toBe('connected')
    expect(r.params.degradationPreference).toBe('maintain-resolution')
    expect(r.params.encodings[0]!.maxBitrate).toBe(8000000)
    expect(del).toMatchObject({
      url: new URL('/api/v1/broadcasts/b1/program/whip/sessions/s1', page.url()).href,
      method: 'DELETE',
      credentials: 'same-origin',
    })
    expect(r.calls.filter((c) => c.method === 'PATCH')).toEqual([]) // no trickle
    expect(r.calls).toHaveLength(2)
  })

test('never offers while the audio context is suspended', async ({ page }) => {
  const r = await run<{ error: string; calls: number }>(
    page,
    `
    await window.mix.context.suspend()
    let error = ''
    try { await window.publisher('vp8').start() } catch (e) { error = e.name }
    return { error, calls: window.calls.length }
  `,
  )
  expect(r).toEqual({ error: 'AudioSuspendedError', calls: 0 })
})

test('publishes again when the connection fails', async ({ page }) => {
  const r = await run<{ posts: string[]; deletes: string[]; states: string[] }>(
    page,
    `
    const p = window.publisher('vp8')
    await p.start()
    for (let i = 0; i < 100 && p.pc.connectionState !== 'connected'; i++) await new Promise((r) => setTimeout(r, 100))
    const first = p.pc
    Object.defineProperty(first, 'connectionState', { value: 'failed' })
    first.dispatchEvent(new Event('connectionstatechange'))
    for (let i = 0; i < 100 && (p.pc === first || p.pc.connectionState !== 'connected'); i++) await new Promise((r) => setTimeout(r, 100))
    await p.stop()
    return { posts: window.calls.filter((c) => c.method !== 'DELETE').map((c) => c.method),
      deletes: window.calls.filter((c) => c.method === 'DELETE').map((c) => c.url), states: window.states }
  `,
  )
  expect(r.posts).toEqual(['POST', 'POST'])
  expect(r.states).toEqual(['connecting', 'connected', 'retrying', 'connecting', 'connected', 'stopped'])
  expect(r.deletes).toEqual([expect.stringMatching(/\/sessions\/s2$/)]) // the latest session only
})

test('a refused offer throws the server error', async ({ page }) => {
  const r = await run<{ status: number; code: string }>(
    page,
    `
    window.answer.status = 409
    try { await window.publisher('vp8').start() } catch (e) { return { status: e.status, code: e.code } }
  `,
  )
  expect(r).toEqual({ status: 409, code: 'conflict' })
})
