// The live controls (task 4.3) against a real StreamGo, LiveKit, MediaMTX and an RTMP test sink: go live, the
// program in the destinations' recordings, destination errors and switching while live, the refusals, the reload
// recovery, the end confirmation, the browser check and the hidden-tab warning. Run with a running stack whose
// output.allow_private_destinations is on and a second MediaMTX with RTMP as the sink:
//   E2E_BASE_URL=http://localhost:28390 E2E_OWNER_EMAIL=… E2E_OWNER_PASSWORD=… \
//   E2E_SINK_RTMP=rtmp://sink:1935/live E2E_SINK_RECORD=/path/record.sh [E2E_SHOTS=dir] npx playwright test 21-live
// E2E_SINK_RECORD is a command called as `<command> <sink path> <seconds> <file>` that records what the sink
// receives on that path into the file. Microphones are page-side oscillators (no echo cancellation or noise
// suppression on a tone); cameras are Chrome's fake device. Skipped without the stack.
import AxeBuilder from '@axe-core/playwright'
import { execFileSync } from 'node:child_process'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test'

const base = process.env.E2E_BASE_URL ?? ''
const email = process.env.E2E_OWNER_EMAIL ?? ''
const password = process.env.E2E_OWNER_PASSWORD ?? ''
const sink = process.env.E2E_SINK_RTMP ?? ''
const record = process.env.E2E_SINK_RECORD ?? ''
const shots = process.env.E2E_SHOTS ?? ''

test.skip(!base || !email || !password || !sink || !record, 'needs a running stack and a sink (E2E_BASE_URL, …)')
test.describe.configure({ mode: 'serial' })
test.use({
  launchOptions: { args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] },
})

// blockAutoplay makes every audio context of the page start suspended until the page's own code resumes it, as
// Chrome's autoplay policy does in a profile with no exemption. Needed because pages driven by this test runner
// report a user activation from the start (navigator.userActivation.hasBeenActive is true at load, measured), so
// Chrome lets their audio run at once; the real policy is checked in plain Chrome by the manual-style run.
function blockAutoplay() {
  const Native = window.AudioContext
  window.AudioContext = class extends Native {
    constructor(o?: AudioContextOptions) {
      super(o)
      void this.suspend()
    }
  }
}

const headers = () => ({ Origin: new URL(base).origin })
const FHD = { width: 1920, height: 1080 }
const LEVEL = 0.25 // each tone's amplitude at the source: -12.04 dBFS
const dB = (a: number) => 20 * Math.log10(a)
let signedIn: Awaited<ReturnType<BrowserContext['storageState']>>
const recordings = mkdtempSync(join(tmpdir(), 'streamgo-live-'))

test.beforeAll(async ({ browser }) => {
  if (!base) return
  const ctx = await browser.newContext({ baseURL: base })
  const r = await ctx.request.post('/api/v1/auth/login', { data: { email, password }, headers: headers() })
  expect(r.status()).toBe(200)
  signedIn = await ctx.storageState()
  await ctx.close()
})

// tone replaces the microphone with an oscillator at freq (the camera stays Chrome's fake one); window.__tone.g is
// its gain. Its audio context starts on the first click, as the studio's does.
function tone() {
  return ({ freq, level }: { freq: number; level: number }) => {
    const md = navigator.mediaDevices
    const original = md.getUserMedia.bind(md)
    let ctx: AudioContext | null = null
    md.getUserMedia = async (c?: MediaStreamConstraints) => {
      const out = new MediaStream()
      if (c?.video) for (const t of (await original({ video: c.video })).getVideoTracks()) out.addTrack(t)
      if (c?.audio) {
        ctx ??= new AudioContext()
        const g = new GainNode(ctx, { gain: level })
        const d = ctx.createMediaStreamDestination()
        const o = new OscillatorNode(ctx, { frequency: freq })
        o.connect(g).connect(d)
        o.start()
        ;(window as unknown as { __tone: { g: GainNode } }).__tone = { g }
        out.addTrack(d.stream.getAudioTracks()[0]!)
      }
      return out
    }
    for (const ev of ['pointerdown', 'keydown']) addEventListener(ev, () => void ctx?.resume(), true)
  }
}

async function ownerContext(browser: Browser, freq = 440, viewport = FHD) {
  const ctx = await browser.newContext({ viewport, colorScheme: 'dark', baseURL: base, storageState: signedIn })
  await ctx.addInitScript(tone(), { freq, level: LEVEL })
  return ctx
}

async function api(ctx: BrowserContext, method: string, path: string, data?: unknown) {
  const r = await ctx.request.fetch(`/api/v1${path}`, { method, data, headers: headers() })
  expect(r.ok(), `${method} ${path}: ${r.status()} ${await r.text()}`).toBe(true)
  return r.status() === 204 ? null : await r.json()
}

const newBroadcast = (ctx: BrowserContext, title: string) =>
  api(ctx, 'POST', '/broadcasts', { title }) as Promise<{ id: string; invite_url: string }>

/** a destination on the sink; its recording path is live/<key> */
const newDestination = (ctx: BrowserContext, name: string, key: string, address = sink) =>
  api(ctx, 'POST', '/destinations', { name, address, key }) as Promise<{ id: string }>

async function openStudio(ctx: BrowserContext, id: string) {
  const page = await ctx.newPage()
  await page.goto(`/studio/${id}`)
  await expect(page.locator('[data-card]').first()).toBeVisible({ timeout: 20_000 })
  return page
}

async function guestJoins(browser: Browser, invite: string, name: string, freq: number) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await ctx.addInitScript(tone(), { freq, level: LEVEL })
  const page = await ctx.newPage()
  await page.goto(invite)
  await page.getByLabel('Your name').fill(name)
  await page.getByRole('button', { name: 'Enter studio' }).click()
  await expect(page.locator('[data-guest-place]')).toBeVisible({ timeout: 20_000 })
  await page.locator('body').click({ position: { x: 2, y: 2 } }) // starts the guest's audio
  return { ctx, page }
}

const state = (page: Page) => page.locator('[data-output-state]')
const chip = (page: Page, name: string) => page.locator('[data-destination-chip]', { hasText: name })

// tabTo moves the focus with Tab only until the target holds it
async function tabTo(page: Page, target: Locator) {
  for (let i = 0; i < 150; i++) {
    await page.keyboard.press('Tab')
    if (await target.evaluate((el) => el === document.activeElement)) return
  }
  throw new Error(`Tab never reached ${target}`)
}

// ── the sink's recordings ──────────────────────────────────────────────────────────────────────────────────────
function rec(path: string, seconds: number): string {
  const file = join(recordings, `${path.replaceAll('/', '_')}-${Date.now()}.mkv`)
  execFileSync(record, [path, String(seconds), file], { timeout: (seconds + 30) * 1000 })
  return file
}

function streams(file: string): string[] {
  const out = execFileSync('ffprobe', ['-v', 'error', '-show_entries', 'stream=codec_type', '-of', 'csv=p=0', file])
  return out.toString().trim().split('\n').sort()
}

function pcm(file: string): Float32Array {
  const b = execFileSync(
    'ffmpeg',
    ['-v', 'error', '-i', file, '-map', '0:a:0', '-af', 'pan=mono|c0=c0', '-ar', '48000', '-f', 'f32le', '-'],
    {
      maxBuffer: 1 << 28,
    },
  )
  return new Float32Array(b.buffer.slice(b.byteOffset, b.byteOffset + b.length))
}

/** amplitude of the sine at f in x[from, from + n) (Goertzel) */
function amp(x: Float32Array, f: number, from = 0, n = x.length - from) {
  let re = 0
  let im = 0
  for (let i = 0; i < n; i++) {
    const p = (2 * Math.PI * f * i) / 48000
    re += x[from + i]! * Math.cos(p)
    im -= x[from + i]! * Math.sin(p)
  }
  return (2 * Math.hypot(re, im)) / n
}

/** what a recording carries, from its second second to one second before its end */
function audio(file: string) {
  const all = pcm(file)
  const x = all.subarray(48000, Math.max(48000, all.length - 48000))
  let peak = 0
  let sum = 0
  for (const v of x) {
    peak = Math.max(peak, Math.abs(v))
    sum += v * v
  }
  return {
    seconds: x.length / 48000,
    peak,
    rmsDb: dB(Math.sqrt(sum / x.length) || 1e-9),
    tones: Object.fromEntries([440, 660, 880].map((f) => [f, Math.round(dB(amp(x, f) || 1e-9) * 100) / 100])),
  }
}

test.describe('live controls', () => {
  let ctx: BrowserContext
  let studio: Page
  let bruno: { ctx: BrowserContext; page: Page }
  let b: { id: string; invite_url: string }
  const stamp = Date.now()
  const keys = { a: `a-${stamp}`, b: `b-${stamp}` }
  const names = { a: `Sink A ${stamp}`, b: `Sink B ${stamp}`, broken: `Broken ${stamp}` }
  const ids: Record<string, string> = {}
  const measured: Record<string, unknown> = {}

  test.beforeAll(async ({ browser }) => {
    test.setTimeout(90_000)
    ctx = await ownerContext(browser)
    b = await newBroadcast(ctx, 'Live controls')
    ids.a = (await newDestination(ctx, names.a, keys.a)).id
    ids.b = (await newDestination(ctx, names.b, keys.b)).id
    // a wrong address: the sink's host on a port where nothing listens
    ids.broken = (await newDestination(ctx, names.broken, `x-${stamp}`, sink.replace(/:\d+/, ':1936'))).id
    await api(ctx, 'PUT', `/broadcasts/${b.id}/destinations`, { ids: [ids.a, ids.b] })
    studio = await openStudio(ctx, b.id)
    bruno = await guestJoins(browser, b.invite_url, 'Bruno Reis', 660)
    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
    await studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }).click()
    await expect(bruno.page.locator('[data-guest-place]')).toHaveText(/On stage/, { timeout: 15_000 })
  })
  test.afterAll(async () => {
    console.log('measured', JSON.stringify(measured, null, 1))
    // the account keeps at most 20 destinations: leave none of this run's behind
    if (b) await ctx.request.fetch(`/api/v1/broadcasts/${b.id}/live`, { method: 'DELETE', headers: headers() })
    for (const id of Object.values(ids))
      await ctx.request.fetch(`/api/v1/destinations/${id}`, { method: 'DELETE', headers: headers() })
    await bruno?.ctx.close()
    await ctx?.close()
  })

  test('Go live by keyboard: starting, live with the timer, both destinations live, the guest sees on air', async () => {
    test.setTimeout(90_000)
    await expect(state(studio)).toHaveText(/Offline/)
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/Not on air/)
    await studio.locator('body').click({ position: { x: 5, y: 5 } })
    await tabTo(studio, studio.getByRole('button', { name: 'Go live' }))
    await studio.keyboard.press('Enter')
    await expect(state(studio)).toHaveText(/Starting|Live/, { timeout: 10_000 })
    await expect(state(studio)).toHaveText(/Live/, { timeout: 45_000 })
    await expect(studio.getByRole('timer')).toHaveText(/^00:00:\d\d$/)
    for (const n of [names.a, names.b])
      await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'live', { timeout: 30_000 })
    await expect(studio.getByRole('button', { name: 'End broadcast' })).toBeVisible()
    await expect(studio.locator('[data-resume]')).toHaveCount(0)
    // "Guests see on air"
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/On air/, { timeout: 10_000 })
  })

  test('Each source once: both destinations record video and audio with each tone once at its level', async () => {
    test.setTimeout(90_000)
    await studio.waitForTimeout(3000)
    for (const k of ['a', 'b'] as const) {
      const file = rec(`live/${keys[k]}`, 8)
      expect(streams(file)).toEqual(['audio', 'video'])
      const a = audio(file)
      measured[`each-source-once-${k}`] = { ...a, source_db: dB(LEVEL) }
      // the owner's 440 Hz and the guest's 660 Hz, each within 1 dB of the source (a copy counted twice is +6 dB)
      expect(Math.abs(a.tones[440]! - dB(LEVEL))).toBeLessThan(1)
      expect(Math.abs(a.tones[660]! - dB(LEVEL))).toBeLessThan(1)
      expect(a.tones[880]!).toBeLessThan(-50)
    }
  })

  test('Empty stage: the program keeps a silent audio track and the broadcast stays live', async () => {
    test.setTimeout(60_000)
    await api(ctx, 'PATCH', `/broadcasts/${b.id}/stage`, { sources: [] })
    await studio.waitForTimeout(2000)
    const file = rec(`live/${keys.a}`, 6)
    expect(streams(file)).toEqual(['audio', 'video'])
    const a = audio(file)
    measured['empty-stage'] = a
    expect(a.seconds).toBeGreaterThan(3)
    expect(a.rmsDb).toBeLessThan(-60)
    await expect(state(studio)).toHaveText(/Live/)
  })

  test('Several voices at full level do not clip', async () => {
    test.setTimeout(60_000)
    for (const p of [studio, bruno.page])
      await p.evaluate(() => ((window as unknown as { __tone: { g: GainNode } }).__tone.g.gain.value = 1))
    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
    await studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }).click()
    await studio.waitForTimeout(2000)
    const a = audio(rec(`live/${keys.a}`, 6))
    measured['two-full-voices'] = a
    expect(a.tones[440]!).toBeGreaterThan(-12)
    expect(a.peak).toBeLessThan(1)
    for (const p of [studio, bruno.page])
      await p.evaluate((l) => ((window as unknown as { __tone: { g: GainNode } }).__tone.g.gain.value = l), LEVEL)
  })

  test('Destination error is visible; add and remove a destination while live', async () => {
    test.setTimeout(60_000)
    await studio.getByRole('tab', { name: 'Destinations' }).click()
    await studio.locator(`[data-destination="${names.broken}"]`).check()
    await expect(chip(studio, names.broken)).toHaveAttribute('data-destination-chip', 'retrying', { timeout: 30_000 })
    await expect(chip(studio, names.broken)).toHaveClass(/border-err/)
    const item = studio.locator('.studio-item', { hasText: names.broken })
    await expect(item.locator('[data-destination-status]')).toHaveText(/Retrying/)
    await expect(item.locator('.field-error')).not.toBeEmpty()
    measured['broken-error'] = await item.locator('.field-error').textContent()
    await expect(chip(studio, names.broken)).toHaveAttribute('title', /.+/)
    for (const n of [names.a, names.b]) await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'live')
    await expect(state(studio)).toHaveText(/Live/)
    // removing it while live leaves the others live
    await studio.locator(`[data-destination="${names.broken}"]`).uncheck()
    await expect(chip(studio, names.broken)).toHaveCount(0)
    await studio.waitForTimeout(3500)
    for (const n of [names.a, names.b]) await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'live')
    // and the last one cannot be removed while live: the server's reason is shown
    await studio.locator(`[data-destination="${names.b}"]`).uncheck()
    await expect(chip(studio, names.b)).toHaveCount(0)
    await studio.locator(`[data-destination="${names.a}"]`).uncheck()
    await expect(studio.getByText('A live broadcast needs at least one destination')).toBeVisible()
    await expect(chip(studio, names.a)).toHaveCount(1)
    await expect(studio.locator(`[data-destination="${names.a}"]`)).toBeChecked()
    await studio.locator(`[data-destination="${names.b}"]`).check()
    await expect(chip(studio, names.b)).toHaveAttribute('data-destination-chip', 'live', { timeout: 30_000 })
  })

  test('axe and no page scrollbar in the live state, and the screenshot', async () => {
    await studio.getByRole('tab', { name: 'Destinations' }).click()
    const r = await new AxeBuilder({ page: studio }).analyze()
    expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(' ')}`)).toEqual([])
    const doc = await studio.evaluate(() => {
      const e = document.documentElement
      return { sw: e.scrollWidth, cw: e.clientWidth, sh: e.scrollHeight, ch: e.clientHeight }
    })
    expect(doc.sw).toBeLessThanOrEqual(doc.cw)
    expect(doc.sh).toBeLessThanOrEqual(doc.ch)
    if (shots) {
      mkdirSync(shots, { recursive: true })
      await studio.screenshot({ path: `${shots}/studio-live-1920x1080.png` })
    }
  })

  test('Publishing while hidden (emulated in the page): the tab title warns while live', async () => {
    const before = await studio.title()
    const hide = (hidden: boolean) =>
      studio.evaluate((h) => {
        Object.defineProperty(document, 'visibilityState', {
          configurable: true,
          get: () => (h ? 'hidden' : 'visible'),
        })
        document.dispatchEvent(new Event('visibilitychange'))
      }, hidden)
    await hide(true)
    await expect(studio).toHaveTitle('● On air from this tab: keep it open')
    await hide(false)
    await expect(studio).toHaveTitle(before)
  })

  test('Reload while live, audio allowed: the studio reconnects and publishes again by itself', async () => {
    test.setTimeout(90_000)
    // a page reloaded after the owner's clicks may start its audio at once (Chrome carries the activation over a
    // same-origin reload): the program goes out again without any action
    await studio.reload()
    await expect(studio.locator('[data-card]').first()).toBeVisible({ timeout: 20_000 })
    await expect(state(studio)).toHaveText(/Live/, { timeout: 45_000 })
    await expect(studio.locator('[data-resume]')).toHaveCount(0)
    for (const n of [names.a, names.b])
      await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'live', { timeout: 30_000 })
  })

  test('Reload while live in a fresh profile: "Resume broadcast" on one click, live again; the screen to share again', async ({
    browser,
  }) => {
    test.setTimeout(150_000)
    // a shared screen on stage before the reload
    await studio.getByRole('toolbar').getByRole('button', { name: 'Share screen' }).click()
    const screenCard = studio.getByRole('button', { name: "Add to stage: Ana Souza's screen" })
    await expect(screenCard).toBeVisible({ timeout: 15_000 })
    await screenCard.click()

    // the page goes away and comes back in a new profile with no autoplay exemption (emulated, see blockAutoplay)
    await ctx.close()
    ctx = await ownerContext(browser)
    await ctx.addInitScript(blockAutoplay)
    studio = await openStudio(ctx, b.id)
    const resume = studio.locator('[data-resume]')
    await expect(resume).toBeVisible({ timeout: 20_000 })
    await expect(resume).toContainText('After a reload the browser needs one click')
    await expect(studio.locator('[data-reshare]')).toBeVisible()
    // the room is joined without any action: the guest is there
    await expect(studio.locator('[data-card="Bruno Reis"]')).toBeVisible()
    // the program is lost: interrupted, and the guest no longer sees on air
    await expect(state(studio)).toHaveText(/Interrupted/, { timeout: 30_000 })
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/Not on air/, { timeout: 15_000 })

    await resume.getByRole('button', { name: 'Resume broadcast' }).click()
    await expect(resume).toHaveCount(0)
    await expect(state(studio)).toHaveText(/Live/, { timeout: 45_000 })
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/On air/, { timeout: 15_000 })
    for (const n of [names.a, names.b])
      await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'live', { timeout: 30_000 })
    await studio.waitForTimeout(3000)
    const file = rec(`live/${keys.a}`, 6)
    expect(streams(file)).toEqual(['audio', 'video'])
    const a = audio(file)
    measured['after-resume'] = a
    // the owner's microphone and the guest's are back in the program, once each
    expect(Math.abs(a.tones[440]! - dB(LEVEL))).toBeLessThan(1)
    expect(Math.abs(a.tones[660]! - dB(LEVEL))).toBeLessThan(1)
  })

  test('End needs confirmation: a themed dialog, then offline and the guest no longer on air', async () => {
    test.setTimeout(60_000)
    await studio.getByRole('button', { name: 'End broadcast' }).click()
    const dialog = studio.getByRole('dialog', { name: 'End broadcast' })
    await expect(dialog).toBeVisible()
    // 5 % of the window height below the top, once the rise animation ends
    await expect
      .poll(() => dialog.evaluate((d) => Math.abs(d.getBoundingClientRect().top - 0.05 * innerHeight)))
      .toBeLessThanOrEqual(1)
    await dialog.getByRole('button', { name: 'Keep broadcasting' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(state(studio)).toHaveText(/Live/)
    await studio.getByRole('button', { name: 'End broadcast' }).click()
    await studio.getByRole('dialog', { name: 'End broadcast' }).getByRole('button', { name: 'End broadcast' }).click()
    await expect(state(studio)).toHaveText(/Offline/, { timeout: 15_000 })
    await expect(studio.getByRole('timer')).toHaveCount(0)
    for (const n of [names.a, names.b])
      await expect(chip(studio, n)).toHaveAttribute('data-destination-chip', 'off', { timeout: 15_000 })
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/Not on air/, { timeout: 15_000 })
    await expect(studio.getByRole('button', { name: 'Go live' })).toBeEnabled()
  })

  test('Refusals: no destination, destination in use, server at its limit, each with the reason', async () => {
    test.setTimeout(90_000)
    const goLive = async (page: Page) => {
      await page.getByRole('button', { name: 'Go live' }).click()
    }
    // no destination
    const empty = await newBroadcast(ctx, 'No destination')
    const p1 = await openStudio(ctx, empty.id)
    await goLive(p1)
    await expect(p1.getByText('Choose at least one destination before going live.')).toBeVisible()
    await expect(state(p1)).toHaveText(/Offline/)
    await p1.close()

    // two broadcasts on air (output.max_concurrent = 2): the first holds Sink A
    const first = await newBroadcast(ctx, 'First')
    await api(ctx, 'PUT', `/broadcasts/${first.id}/destinations`, { ids: [ids.a] })
    await api(ctx, 'POST', `/broadcasts/${first.id}/live`)
    // destination in use
    const second = await newBroadcast(ctx, 'Second')
    await api(ctx, 'PUT', `/broadcasts/${second.id}/destinations`, { ids: [ids.a] })
    const p2 = await openStudio(ctx, second.id)
    await goLive(p2)
    await expect(p2.getByText(`The destination “${names.a}” is in use by another broadcast.`)).toBeVisible()
    await p2.close()
    // server at its limit
    await api(ctx, 'PUT', `/broadcasts/${second.id}/destinations`, { ids: [ids.b] })
    await api(ctx, 'POST', `/broadcasts/${second.id}/live`)
    const third = await newBroadcast(ctx, 'Third')
    await api(ctx, 'PUT', `/broadcasts/${third.id}/destinations`, { ids: [ids.broken] })
    const p3 = await openStudio(ctx, third.id)
    await goLive(p3)
    await expect(p3.getByText('The server is at its limit of 2 live broadcasts.')).toBeVisible()
    await p3.close()
    for (const x of [first, second]) await api(ctx, 'DELETE', `/broadcasts/${x.id}/live`)
  })
})

test('Unsupported browser: the studio says hosting needs a Chromium-based desktop browser; Go live is disabled', async ({
  browser,
}) => {
  const ctx = await browser.newContext({ viewport: FHD, baseURL: base, storageState: signedIn })
  await ctx.addInitScript(() => Object.defineProperty(navigator, 'userAgentData', { get: () => undefined }))
  const b = await newBroadcast(ctx, 'Not Chromium')
  const page = await openStudio(ctx, b.id)
  await expect(page.locator('[data-cannot-host]')).toContainText('Hosting needs a Chromium-based desktop browser')
  const goLive = page.getByRole('button', { name: 'Go live' })
  await expect(goLive).toBeDisabled()
  await expect(goLive).toHaveAccessibleDescription(
    'Hosting needs a Chromium-based desktop browser, such as Chrome or Edge.',
  )
  // the stage is still drawn, as a preview
  await expect.poll(() => page.locator('[data-program]').evaluate((c: HTMLCanvasElement) => c.width)).toBe(1280)
  await ctx.close()
})
