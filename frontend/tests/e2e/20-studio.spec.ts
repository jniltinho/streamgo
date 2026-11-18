// The studio screens against a real StreamGo and LiveKit (tasks 4.1, 4.2 and 4.4; the output quality of
// add-output-quality). Run with a running stack whose studio.max_participants is 3 (the "Studio full" scenario fills
// it with the owner and two guests), whose output.max_resolution is 1440p and whose output.video_bitrate_kbps is 0
// (the bitrate follows the resolution):
//   E2E_BASE_URL=http://localhost:28190 E2E_OWNER_EMAIL=… E2E_OWNER_PASSWORD=… [E2E_SHOTS=dir] \
//     npx playwright test 20-studio
// Owner and guests are separate browser contexts with Chrome's fake camera and microphone. Skipped without the stack.
import AxeBuilder from '@axe-core/playwright'
import { mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test'
import { themeCatalog } from '../../src/themes/catalog'

const base = process.env.E2E_BASE_URL ?? ''
const email = process.env.E2E_OWNER_EMAIL ?? ''
const password = process.env.E2E_OWNER_PASSWORD ?? ''
const shots = process.env.E2E_SHOTS ?? ''

test.skip(!base || !email || !password, 'needs a running StreamGo and LiveKit (E2E_BASE_URL, E2E_OWNER_*)')
test.describe.configure({ mode: 'serial' })
test.use({
  launchOptions: { args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] },
})

const headers = () => ({ Origin: new URL(base).origin })
const FHD = { width: 1920, height: 1080 }
let signedIn: Awaited<ReturnType<BrowserContext['storageState']>>

// one sign-in for the whole file: the sign-in route allows 10 attempts a minute per address
test.beforeAll(async ({ browser }) => {
  if (!base) return
  const ctx = await browser.newContext({ baseURL: base })
  const r = await ctx.request.post('/api/v1/auth/login', { data: { email, password }, headers: headers() })
  expect(r.status()).toBe(200)
  signedIn = await ctx.storageState()
  await ctx.close()
})

const ownerContext = (browser: Browser, viewport = FHD) =>
  browser.newContext({ viewport, colorScheme: 'dark', baseURL: base, storageState: signedIn })

async function api(ctx: BrowserContext, method: string, path: string, data?: unknown) {
  const r = await ctx.request.fetch(`/api/v1${path}`, { method, data, headers: headers() })
  expect(r.ok(), `${method} ${path}: ${r.status()} ${await r.text()}`).toBe(true)
  return r.status() === 204 ? null : await r.json()
}

const newBroadcast = (ctx: BrowserContext, title: string) =>
  api(ctx, 'POST', '/broadcasts', { title }) as Promise<{ id: string; invite_url: string }>

async function openStudio(ctx: BrowserContext, id: string) {
  const page = await ctx.newPage()
  await page.goto(`/studio/${id}`)
  await expect(page.locator('[data-card]').first()).toBeVisible({ timeout: 20_000 })
  return page
}

async function guestJoins(browser: Browser, invite: string, name: string, viewport = { width: 1280, height: 800 }) {
  const ctx = await browser.newContext({ viewport, colorScheme: 'dark' })
  const page = await ctx.newPage()
  await page.goto(invite)
  await page.getByLabel('Your name').fill(name)
  await page.getByRole('button', { name: 'Enter studio' }).click()
  await expect(page.locator('[data-guest-place]')).toBeVisible({ timeout: 20_000 })
  return { ctx, page }
}

// recordConstraints keeps every applyConstraints call of the page's tracks in window.__constraints
function recordConstraints() {
  const calls: unknown[] = ((window as unknown as { __constraints: unknown[] }).__constraints = [])
  const native = MediaStreamTrack.prototype.applyConstraints
  MediaStreamTrack.prototype.applyConstraints = function (c?: MediaTrackConstraints) {
    calls.push(c)
    return native.call(this, c)
  }
}

const card = (page: Page, name: string) => page.locator(`[data-card="${name}"]`)
const place = (page: Page) => page.locator('[data-guest-place]')

// tabTo moves the focus with Tab only until the target holds it, and checks the focus indicator is drawn
async function tabTo(page: Page, target: Locator) {
  for (let i = 0; i < 150; i++) {
    await page.keyboard.press('Tab')
    if (await target.evaluate((el) => el === document.activeElement)) {
      const ring = await target.evaluate((el) => {
        const s = getComputedStyle(el)
        return s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0
      })
      expect(ring, 'a visible focus indicator').toBe(true)
      return
    }
  }
  throw new Error(`Tab never reached ${target}`)
}

/** pixels of the program canvas, sampled on a 16x9 grid, as [r, g, b] */
const sample = (page: Page) =>
  page.locator('[data-program]').evaluate((c: HTMLCanvasElement) => {
    const g = c.getContext('2d')!
    const out: number[][] = []
    for (let y = 0; y < 9; y++)
      for (let x = 0; x < 16; x++) {
        const d = g.getImageData(
          Math.round(((x + 0.5) * c.width) / 16),
          Math.round(((y + 0.5) * c.height) / 9),
          1,
          1,
        ).data
        out.push([d[0]!, d[1]!, d[2]!])
      }
    return out
  })

/** one pixel of the program canvas at fractions of its size */
const pixel = (page: Page, fx: number, fy: number) =>
  page.locator('[data-program]').evaluate(
    (c: HTMLCanvasElement, [fx, fy]) =>
      c
        .getContext('2d')!
        .getImageData(Math.round(c.width * fx!), Math.round(c.height * fy!), 1, 1)
        .data.slice(0, 3),
    [fx, fy],
  )

const setTheme = (page: Page, id: string) =>
  page.evaluate((id) => document.documentElement.setAttribute('data-theme', id), id)

// a 4x4 PNG drawn by the browser: the logo upload
const png = (page: Page) =>
  page.evaluate(
    () =>
      new Promise<number[]>((resolve) => {
        const c = document.createElement('canvas')
        c.width = c.height = 4
        c.toBlob((b) => void b!.arrayBuffer().then((a) => resolve([...new Uint8Array(a)])), 'image/png')
      }),
  )

test.describe('owner studio', () => {
  let ctx: BrowserContext
  let studio: Page
  let bruno: { ctx: BrowserContext; page: Page }
  let b: { id: string; invite_url: string }
  const destName = `YouTube ${Date.now()}`

  test.beforeAll(async ({ browser }) => {
    ctx = await ownerContext(browser)
    await ctx.addInitScript(recordConstraints)
    b = await newBroadcast(ctx, 'Weekly Product Live')
    const dest = await api(ctx, 'POST', '/destinations', {
      name: destName,
      address: 'rtmp://1.1.1.1/live2',
      key: 'k',
    })
    await api(ctx, 'PUT', `/broadcasts/${b.id}/destinations`, { ids: [dest.id] })
    studio = await openStudio(ctx, b.id)
    bruno = await guestJoins(browser, b.invite_url, 'Bruno Reis')
    await expect(card(studio, 'Bruno Reis')).toBeVisible({ timeout: 20_000 })
  })
  test.afterAll(async () => {
    await bruno?.ctx.close()
    await ctx?.close()
  })

  test('Regions: header, layout picker, backstage, side panel tabs and controls', async () => {
    await expect(studio.getByRole('heading', { name: 'Weekly Product Live' })).toBeVisible()
    await expect(studio.locator('[data-output-state]')).toHaveText(/Offline/)
    await expect(studio.locator('[data-destination-chip]')).toHaveCount(1)
    await expect(studio.locator('[data-destination-chip]')).toContainText('YouTube')
    await expect(studio.locator('[data-destination-chip]')).toHaveAttribute('data-destination-chip', 'off')
    await expect(studio.getByRole('button', { name: 'Go live' })).toBeEnabled()
    await expect(studio.getByRole('radiogroup', { name: 'Layout' }).getByRole('radio')).toHaveCount(6)
    for (const n of ['Banners', 'Brand', 'Destinations'])
      await expect(studio.getByRole('tab', { name: n })).toBeVisible()
    for (const n of ['Mic', 'Camera', 'Share screen', 'Invite', 'Device settings', 'Leave studio'])
      await expect(studio.getByRole('toolbar').getByRole('button', { name: n, exact: true })).toBeVisible()
    await expect(card(studio, 'Ana Souza')).toContainText('Host')
    await expect(card(studio, 'Bruno Reis')).toContainText('Guest')
    for (const n of ['Ana Souza', 'Bruno Reis']) await expect(card(studio, n)).toHaveAttribute('data-on-stage', 'false')
    // nothing is on stage yet: the stage says how to fill it, and each card shows the live picture of its camera
    await expect(studio.locator('[data-empty-stage]')).toContainText('Add to stage')
    for (const n of ['Ana Souza', 'Bruno Reis'])
      await expect
        .poll(() =>
          card(studio, n)
            .locator('[data-preview]')
            .evaluate((v: HTMLVideoElement) => v.videoWidth),
        )
        .toBeGreaterThan(0)
    // the invite link with copy and rotate, and the device settings, each in its own modal
    await studio.getByRole('toolbar').getByRole('button', { name: 'Invite', exact: true }).click()
    await expect(studio.locator('[data-invite-url]')).toHaveValue(b.invite_url)
    await studio.getByRole('button', { name: 'Close' }).last().click()
    await studio.getByRole('button', { name: 'Device settings' }).click()
    await expect(studio.getByLabel('Microphone', { exact: true })).toBeVisible()
    await expect(studio.getByLabel('Camera', { exact: true })).toBeVisible()
    await studio.getByRole('button', { name: 'Done' }).click()
  })

  // studio-interactions "A modal over a modal" (design U10): a confirmation over Invite closes alone
  test('A modal over a modal: a confirmation over Invite', async () => {
    await studio.getByRole('toolbar').getByRole('button', { name: 'Invite', exact: true }).click()
    const invite = studio.getByRole('dialog', { name: 'Invite guests' })
    await expect(invite).toBeVisible()
    const rotate = invite.getByRole('button', { name: 'New link' })
    await rotate.click()
    const confirm = studio.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(confirm).toBeVisible()
    await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused()
    const ids = await studio.locator('.pnl').evaluateAll((els) => els.map((el) => el.getAttribute('aria-labelledby')))
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
    await studio.keyboard.press('Escape')
    await expect(confirm).toBeHidden()
    await expect(invite).toBeVisible()
    await expect(rotate).toBeFocused()
    await expect(invite.locator('[data-invite-url]')).toHaveValue(b.invite_url) // not rotated
    // still modal: Tab stays inside
    for (let i = 0; i < 6; i++) {
      await studio.keyboard.press('Tab')
      expect(await invite.evaluate((el) => el.contains(document.activeElement))).toBe(true)
    }
    await studio.keyboard.press('Escape')
    await expect(invite).toBeHidden()
    await expect(studio.getByRole('toolbar').getByRole('button', { name: 'Invite', exact: true })).toBeFocused()
  })

  test('Keyboard only: add to stage, change the layout and show a banner, with a visible focus', async () => {
    await studio.locator('body').click({ position: { x: 5, y: 5 } }) // start from the top of the page
    await tabTo(studio, studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }))
    await studio.keyboard.press('Enter')
    await expect(card(studio, 'Bruno Reis')).toHaveAttribute('data-on-stage', 'true')
    await expect(card(studio, 'Bruno Reis')).toContainText('On stage')
    await expect(studio.locator('[data-empty-stage]')).toHaveCount(0)
    // "Add to stage": his camera is in the program (the fake camera is green), not the backdrop
    await expect.poll(async () => (await pixel(studio, 0.5, 0.5))[1]!).toBeGreaterThan(80)

    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
    await expect(card(studio, 'Ana Souza')).toHaveAttribute('data-on-stage', 'true')
    await studio.waitForTimeout(500)
    const grid = await sample(studio)
    await tabTo(studio, studio.locator('[data-layout=grid]'))
    await studio.keyboard.press('ArrowRight') // spotlight
    await studio.keyboard.press('ArrowRight') // sidebar
    await expect(studio.locator('[data-layout=sidebar]')).toHaveAttribute('aria-checked', 'true')
    await expect(studio.locator('[data-layout=grid]')).toHaveAttribute('aria-checked', 'false')
    // "Change layout": the stage rearranges (the right-hand strip holds a thumbnail, not half the picture)
    await expect.poll(async () => JSON.stringify(await sample(studio)) !== JSON.stringify(grid)).toBe(true)

    await tabTo(studio, studio.getByLabel('Add banner'))
    await studio.keyboard.type('Q3 roadmap: what ships next')
    await studio.keyboard.press('Enter')
    const show = studio.getByRole('button', { name: 'Show Q3 roadmap: what ships next' })
    await expect(show).toBeVisible()
    await tabTo(studio, show)
    await studio.keyboard.press('Enter')
    await expect(studio.locator('[data-banner="Q3 roadmap: what ships next"]')).toContainText('Showing')
    // the banner is drawn in the program: white, above the name tags (a point in its top padding, clear of the
    // text at 720p and at 1080p)
    await expect.poll(async () => Math.min(...(await pixel(studio, 0.2, 0.818)))).toBeGreaterThan(230)
  })

  test('Guest knows where they are: Backstage, On stage and back', async () => {
    await expect(place(bruno.page)).toHaveText(/On stage/, { timeout: 15_000 })
    await expect(bruno.page.locator('[data-on-air]')).toHaveText(/Not on air/)
    await expect(card(bruno.page, 'Ana Souza')).toBeVisible()
    // a guest sees the live picture of their own card only, never of someone else's source off stage
    await expect(card(bruno.page, 'Bruno Reis').locator('[data-preview]')).toHaveCount(1)
    await expect(card(bruno.page, 'Ana Souza').locator('[data-preview]')).toHaveCount(0)
    // the guest has no control that changes the stage
    await expect(bruno.page.getByRole('radiogroup', { name: 'Layout' })).toHaveCount(0)
    await expect(bruno.page.getByRole('button', { name: /Add to stage:|Take off the stage:/ })).toHaveCount(0)
    await studio.getByRole('button', { name: 'Take off the stage: Bruno Reis' }).click()
    await expect(place(bruno.page)).toHaveText(/Backstage/, { timeout: 15_000 })
    await studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }).click()
    await expect(place(bruno.page)).toHaveText(/On stage/, { timeout: 15_000 })
  })

  test('Camera off: the tile shows the initials and the name', async () => {
    await studio.locator('[data-layout=solo]').click()
    await api(ctx, 'PATCH', `/broadcasts/${b.id}/stage`, { sources: [] })
    await studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }).click()
    await bruno.page.locator('[data-control=camera]').click()
    await expect(card(studio, 'Bruno Reis').getByRole('img', { name: 'Camera off' })).toBeVisible({ timeout: 10_000 })
    // the placeholder circle in the accent colour (#3d41cf), left of the initials
    await expect
      .poll(async () => {
        const [r, g, bl] = await pixel(studio, 0.5 - 0.08, 0.5)
        return Math.abs(r! - 61) + Math.abs(g! - 65) + Math.abs(bl! - 207)
      })
      .toBeLessThan(30)
    await bruno.page.locator('[data-control=camera]').click()
    await expect(card(studio, 'Bruno Reis').getByRole('img', { name: 'Camera on' })).toBeVisible({ timeout: 10_000 })
    await studio.locator('[data-layout=grid]').click()
    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
  })

  test('Stage full: Add to stage is disabled with a reason', async ({ browser }) => {
    const joiner = await browser.newContext({ baseURL: base })
    const ids: string[] = []
    for (let i = 0; i < 5; i++) {
      await joiner.clearCookies()
      const r = await joiner.request.post(new URL(b.invite_url).pathname.replace(/^\/join/, '/api/v1/join'), {
        data: { name: `Extra ${i}` },
        headers: headers(),
      })
      expect(r.status()).toBe(200)
      ids.push((await r.json()).identity)
    }
    await joiner.close()
    const host = (await api(ctx, 'GET', `/broadcasts/${b.id}`)).stage.sources.find((s: { p: string }) =>
      s.p.startsWith('host-'),
    )
    await api(ctx, 'PATCH', `/broadcasts/${b.id}/stage`, {
      sources: [host, ...ids.map((p) => ({ p, k: 'camera' }))],
    })
    const add = studio.getByRole('button', { name: 'Add to stage: Bruno Reis' })
    await expect(add).toBeDisabled({ timeout: 15_000 })
    await expect(add).toHaveAttribute('title', 'The stage is full: 6 sources at most.')
    await expect(add).toHaveAccessibleDescription('The stage is full: 6 sources at most.')
    // back to the owner and Bruno
    await api(ctx, 'PATCH', `/broadcasts/${b.id}/stage`, { sources: [host] })
    await add.click()
    await expect(card(studio, 'Bruno Reis')).toHaveAttribute('data-on-stage', 'true')
  })

  test('Destinations and Brand tabs', async () => {
    await studio.getByRole('tab', { name: 'Destinations' }).click()
    const box = studio.locator(`[data-destination="${destName}"]`)
    await expect(box).toBeChecked()
    await expect(studio.locator('[data-destination-status]')).toHaveText(/Off/)
    await box.uncheck()
    await expect(studio.locator('[data-destination-chip]')).toHaveCount(0)
    await box.check()
    await expect(studio.locator('[data-destination-chip]')).toHaveCount(1)

    await studio.getByRole('tab', { name: 'Brand' }).click()
    await studio.getByRole('radio', { name: '#4ade80' }).click()
    await expect(studio.getByRole('radio', { name: '#4ade80' })).toHaveAttribute('aria-checked', 'true')
    await studio
      .locator('[data-logo-input]')
      .setInputFiles({ name: 'logo.png', mimeType: 'image/png', buffer: Buffer.from(await png(studio)) })
    await expect(studio.locator('img[data-logo]')).toBeVisible()
    await expect.poll(() => studio.locator('img[data-logo]').evaluate((i: HTMLImageElement) => i.naturalWidth)).toBe(4)
    await studio.getByRole('button', { name: 'Remove the logo' }).click()
    await expect(studio.locator('img[data-logo]')).toHaveCount(0)
    await studio.getByRole('radio', { name: '#3d41cf' }).click()
    await studio.getByRole('tab', { name: 'Banners' }).click()
  })

  test('Output quality: picked in the open studio, the canvas and a shared screen follow; locked while live', async () => {
    test.setTimeout(90_000)
    const program = studio.locator('[data-program]')
    const quality = studio.getByLabel('Output quality')
    const hint = studio.locator('#output-quality-hint')
    await studio.getByRole('tab', { name: 'Destinations' }).click()
    await expect(quality).toHaveValue('720p')
    await expect(quality.locator('option')).toHaveText(['720p (HD)', '1080p (Full HD)', '1440p (QHD)'])
    await expect(hint).toHaveText(/1280x720 at 30 fps; uses up to 8 Mbit\/s/)
    await expect(hint).not.toContainText('1080p')
    await expect.poll(() => program.evaluate((c: HTMLCanvasElement) => `${c.width}x${c.height}`)).toBe('1280x720')

    // a screen already shared is asked for the new size
    await studio.getByRole('toolbar').getByRole('button', { name: 'Share screen' }).click()
    await expect(studio.getByRole('toolbar').getByRole('button', { name: 'Stop sharing' })).toBeVisible({
      timeout: 15_000,
    })
    await quality.selectOption('1440p')
    await expect(studio.getByText('Output quality set to 1440p.')).toBeVisible()
    await expect.poll(() => program.evaluate((c: HTMLCanvasElement) => `${c.width}x${c.height}`)).toBe('2560x1440')
    await expect(hint).toContainText('2560x1440 at 30 fps; uses up to 24 Mbit/s')
    await expect(hint).toContainText('Some platforms accept at most 1080p.')
    await expect
      .poll(() => studio.evaluate(() => (window as unknown as { __constraints: unknown[] }).__constraints))
      .toContainEqual({ width: { ideal: 2560 }, height: { ideal: 1440 }, frameRate: 30 })
    const d = await api(ctx, 'GET', `/broadcasts/${b.id}`)
    expect(d.studio).toMatchObject({
      resolution: '1440p',
      program_width: 2560,
      program_height: 1440,
      video_bitrate_kbps: 12000,
    })
    await studio.getByRole('toolbar').getByRole('button', { name: 'Stop sharing' }).click()

    // not offline: the control is disabled and says why
    await studio.getByRole('button', { name: 'Go live' }).click()
    await expect(studio.locator('[data-output-state]')).toHaveText(/Starting|Live|Interrupted/, { timeout: 10_000 })
    await expect(quality).toBeDisabled()
    await expect(hint).toHaveText('The quality can change only while the broadcast is offline.')
    await api(ctx, 'DELETE', `/broadcasts/${b.id}/live`) // the studio's poll sees it within 3 s
    await expect(quality).toBeEnabled({ timeout: 10_000 })
    await quality.selectOption('720p')
    await expect.poll(() => program.evaluate((c: HTMLCanvasElement) => `${c.width}x${c.height}`)).toBe('1280x720')
    await studio.getByRole('tab', { name: 'Banners' }).click()
  })

  test('axe: the studio has no accessibility violations', async () => {
    await expect(studio.locator('.toast')).toHaveCount(0, { timeout: 15_000 })
    const r = await new AxeBuilder({ page: studio }).analyze()
    expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(' ')}`)).toEqual([])
  })

  test('Studio in every theme: labels, on-air and status marks meet the contrast matrix', async () => {
    // a toast of an earlier test that is still fading out has no contrast to measure
    await expect(studio.locator('.toast')).toHaveCount(0, { timeout: 15_000 })
    const failures: string[] = []
    for (const theme of themeCatalog) {
      await setTheme(studio, theme.id)
      const r = await new AxeBuilder({ page: studio }).withRules(['color-contrast']).analyze()
      for (const v of r.violations)
        for (const n of v.nodes) failures.push(`${theme.id}: ${n.target} ${n.failureSummary}`)
      // the status dots (on stage, backstage, destination, output state) are marks: 3:1 against what is behind them
      const marks = await studio.evaluate(() => {
        const rgb = (c: string) =>
          c
            .match(/[\d.]+/g)!
            .slice(0, 3)
            .map(Number)
        const lum = (c: number[]) => 0.2126 * ch(c[0]!) + 0.7152 * ch(c[1]!) + 0.0722 * ch(c[2]!)
        function ch(v: number) {
          const s = v / 255
          return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
        }
        const behind = (el: Element | null): string => {
          for (; el; el = el.parentElement) {
            const c = getComputedStyle(el).backgroundColor
            if (c && !/rgba\(.*, 0\)$/.test(c) && c !== 'transparent') return c
          }
          return getComputedStyle(document.body).backgroundColor
        }
        return [...document.querySelectorAll('.studio-dot')].map((d) => {
          const [a, b] = [lum(rgb(getComputedStyle(d).backgroundColor)), lum(rgb(behind(d.parentElement)))]
          return { cls: d.className, ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05) }
        })
      })
      for (const m of marks) if (m.ratio < 3) failures.push(`${theme.id}: mark ${m.cls} ${m.ratio.toFixed(2)} < 3`)
    }
    await setTheme(studio, 'streamgo-dark')
    expect(failures).toEqual([])
  })

  test('Theme independence: the program does not change with the UI theme', async () => {
    // a still program: no source on stage, the banner shown
    await api(ctx, 'PATCH', `/broadcasts/${b.id}/stage`, { sources: [] })
    await studio.waitForTimeout(500)
    const before = await sample(studio)
    for (const theme of ['streamgo-light', 'matrix', 'winamp']) {
      await setTheme(studio, theme)
      await studio.waitForTimeout(200)
      expect(await sample(studio)).toEqual(before)
    }
    await setTheme(studio, 'streamgo-dark')
  })

  test('screenshots at 1920x1080 with a guest on stage and a banner, dark and light', async () => {
    test.skip(!shots, 'E2E_SHOTS not set')
    mkdirSync(shots, { recursive: true })
    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
    await studio.getByRole('button', { name: 'Add to stage: Bruno Reis' }).click()
    await studio.locator('[data-layout=spotlight]').click()
    await studio.waitForTimeout(1500)
    for (const theme of ['streamgo-dark', 'streamgo-light']) {
      await setTheme(studio, theme)
      await studio.waitForTimeout(300)
      await studio.screenshot({ path: `${shots}/studio-1920x1080-${theme}.png` })
    }
    await setTheme(studio, 'streamgo-dark')
    await bruno.page.screenshot({ path: `${shots}/guest-1280x800-streamgo-dark.png` })
  })

  test('Removed: the owner removes a guest after a themed confirmation', async () => {
    await studio.getByRole('button', { name: 'Remove Bruno Reis from the studio' }).click()
    const dialog = studio.getByRole('alertdialog', { name: 'Remove guest' })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Remove' }).click()
    await expect(card(studio, 'Bruno Reis')).toHaveCount(0, { timeout: 15_000 })
    await expect(bruno.page.getByRole('heading', { name: 'You were removed from the studio' })).toBeVisible({
      timeout: 15_000,
    })
  })
})

// "Name tags": the Brand tab takes the name tags off the program and puts them back; the guest's view follows.
test('Name tags: shown by default, hidden and shown again from the Brand tab', async ({ browser }) => {
  const ctx = await ownerContext(browser)
  const b = await newBroadcast(ctx, 'Name tags')
  const studio = await openStudio(ctx, b.id)
  await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
  await expect(card(studio, 'Ana Souza')).toHaveAttribute('data-on-stage', 'true')
  // one source fills the program: its tag is the dark box at the bottom left, over the green fake camera; the point
  // is inside the box, between its accent bar and the name, at 720p and at 1080p. The fake camera is bright
  // green in that corner (about 250), and about 95 under the box
  const tag = async () => (await pixel(studio, 0.0185, 0.958))[1]!
  await expect.poll(tag).toBeLessThan(130)
  await studio.getByRole('tab', { name: 'Brand' }).click()
  const box = studio.locator('[data-name-tags]')
  await expect(box).toBeChecked()
  await box.uncheck()
  await expect.poll(tag).toBeGreaterThan(200)
  const detail = (await api(ctx, 'GET', `/broadcasts/${b.id}`)) as { stage: { hide_names: boolean } }
  expect(detail.stage.hide_names).toBe(true)
  await box.check()
  await expect.poll(tag).toBeLessThan(130)
  await ctx.close()
})

// "Speaking": Chrome's fake microphone beeps for a fraction of a second, which the media server does not always
// take for speech, so this guest's microphone is a steady tone made in the page.
test('Speaking: the microphone mark of a card follows a steady voice and its silence', async ({ browser }) => {
  const ctx = await ownerContext(browser)
  const b = await newBroadcast(ctx, 'Speaking')
  const studio = await openStudio(ctx, b.id)
  const guest = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await guest.addInitScript(() => {
    const md = navigator.mediaDevices
    const original = md.getUserMedia.bind(md)
    let audio: AudioContext | null = null
    md.getUserMedia = async (c?: MediaStreamConstraints) => {
      const out = new MediaStream()
      if (c?.video) for (const t of (await original({ video: c.video })).getVideoTracks()) out.addTrack(t)
      if (c?.audio) {
        audio ??= new AudioContext()
        const gain = new GainNode(audio, { gain: 0.5 })
        const to = audio.createMediaStreamDestination()
        const tone = new OscillatorNode(audio, { frequency: 440 })
        tone.connect(gain).connect(to)
        tone.start()
        ;(window as unknown as { __voice: GainNode }).__voice = gain
        out.addTrack(to.stream.getAudioTracks()[0]!)
      }
      return out
    }
    for (const ev of ['pointerdown', 'keydown']) addEventListener(ev, () => void audio?.resume(), true)
  })
  const page = await guest.newPage()
  await page.goto(b.invite_url)
  await page.getByLabel('Your name').fill('Clara Voz')
  await page.getByRole('button', { name: 'Enter studio' }).click()
  const clara = card(studio, 'Clara Voz')
  await expect(clara.getByRole('img', { name: 'Speaking' })).toBeVisible({ timeout: 30_000 })
  await page.evaluate(() => ((window as unknown as { __voice: GainNode }).__voice.gain.value = 0))
  await expect(clara.getByRole('img', { name: 'Microphone on' })).toBeVisible({ timeout: 15_000 })
  await guest.close()
  await ctx.close()
})

test('One hosting tab: a second tab takes over and the first neither reconnects nor stays', async ({ browser }) => {
  const ctx = await ownerContext(browser)
  const b = await newBroadcast(ctx, 'Two tabs')
  const a = await openStudio(ctx, b.id)
  const second = await openStudio(ctx, b.id)
  await expect(a.getByRole('heading', { name: 'The studio was opened elsewhere' })).toBeVisible({ timeout: 15_000 })
  await a.waitForTimeout(5000)
  await expect(a.getByRole('heading', { name: 'The studio was opened elsewhere' })).toBeVisible()
  await expect(a.locator('[data-program]')).toHaveCount(0)
  await expect(card(second, 'Ana Souza')).toBeVisible()
  await expect(second.getByRole('heading', { name: 'The studio was opened elsewhere' })).toHaveCount(0)
  await ctx.close()
})

test.describe('Screen sizes', () => {
  let id = ''
  test.beforeAll(async ({ browser }) => {
    const ctx = await ownerContext(browser)
    id = (await newBroadcast(ctx, 'Screen sizes')).id
    await ctx.close()
  })

  // the regions in rem (their rectangles divided by the root font size): the same at every size from Full HD up
  const regions = (page: Page) =>
    page.evaluate(() => {
      const rem = parseFloat(getComputedStyle(document.documentElement).fontSize)
      const r = (sel: string) => {
        const b = document.querySelector(sel)!.getBoundingClientRect()
        return [b.x, b.y, b.width, b.height].map((v) => Math.round((v / rem) * 10) / 10)
      }
      return {
        rem,
        header: r('.studio-head'),
        stage: r('.studio-stage'),
        program: r('[data-program]'),
        row: r('.studio-row'),
        back: r('.studio-back'),
        side: r('.studio-side'),
      }
    })

  const sizes = [
    [1280, 720],
    [1440, 900],
    [1920, 1080],
    [2560, 1440],
    [3840, 2160],
    [3440, 1440],
    [2340, 1170],
  ] as const
  const fhd: Record<string, number[]> = {}
  const rem = (w: number, h: number) => Math.min(Math.max(16, Math.min(w / 120, h / 67.5)), 32)

  for (const [width, height] of sizes)
    test(`${width}x${height}: no page scroll, every region and control visible, the program 16:9 and centred`, async ({
      browser,
    }) => {
      const ctx = await ownerContext(browser, { width, height })
      const page = await openStudio(ctx, id)
      await page.waitForTimeout(500)
      const doc = await page.evaluate(() => {
        const e = document.documentElement
        return { sw: e.scrollWidth, cw: e.clientWidth, sh: e.scrollHeight, ch: e.clientHeight }
      })
      expect(doc.sw).toBeLessThanOrEqual(doc.cw)
      expect(doc.sh).toBeLessThanOrEqual(doc.ch)
      // every control outside the sideways-scrolling backstage row lies fully inside the window
      const cut = await page.evaluate(
        ([w, h]) =>
          [...document.querySelectorAll('button, input, select, [role=radio], [role=tab], canvas')]
            .filter((el) => !el.closest('.studio-cards') && !el.closest('.studio-side-body'))
            .filter((el) => {
              const b = el.getBoundingClientRect()
              return b.width > 0 && (b.left < 0 || b.top < 0 || b.right > w! + 0.5 || b.bottom > h! + 0.5)
            })
            .map((el) => el.outerHTML.slice(0, 80)),
        [width, height],
      )
      expect(cut).toEqual([])
      const g = await regions(page)
      const [px, py, pw, ph] = g.program
      expect(Math.abs(pw! / ph! - 16 / 9)).toBeLessThan(0.02)
      const [sx, sy, sw, sh] = g.stage
      expect(Math.abs(px! - sx! - (sx! + sw! - px! - pw!))).toBeLessThan(0.3) // centred across
      expect(Math.abs(py! - sy! - (sy! + sh! - py! - ph!))).toBeLessThan(0.3) // and down
      if (width === 1920) Object.assign(fhd, g)
      // "Wide window": from 19:10 up the backstage stands above the side panel, the stage takes the whole height
      // and no empty band is left around the program
      const wide = width / height >= 1.9
      expect(g.back[0]! > g.stage[0]!, 'the backstage is in the right-hand column').toBe(wide)
      if (wide) {
        expect(Math.abs(g.back[1]! - g.stage[1]!)).toBeLessThan(0.3)
        expect(g.back[1]! + g.back[3]!).toBeLessThan(g.side[1]!)
        // the side panel is as tall as its content, at the bottom of the column; the backstage has the rest
        expect(Math.abs(g.side[1]! + g.side[3]! - (g.row[1]! + g.row[3]!))).toBeLessThan(0.3)
        expect(g.back[3]!).toBeGreaterThan(g.side[3]!)
        expect(sw! - pw!).toBeLessThan(2.4)
        expect(sh! - ph!).toBeLessThan(2.4)
        if (rem(width, height) > 16.5) expect(ph!).toBeGreaterThan(fhd.program![3]! * 1.15)
      }
      if (width === 2560 || width === 3840) {
        // the Full HD arrangement, larger: the same rectangles in rem, within 1 % (hairline borders stay 1 px)
        expect(g.rem).toBeCloseTo((width / 1920) * 16, 1)
        for (const k of ['header', 'stage', 'program', 'row', 'back', 'side'] as const)
          g[k].forEach((v, i) =>
            expect(Math.abs(v - fhd[k]![i]!), `${k}[${i}]`).toBeLessThanOrEqual(Math.max(0.25, fhd[k]![i]! * 0.01)),
          )
      }
      if (width === 3840) {
        // "Text stays readable at 4K": body text twice its Full HD size, nothing under 22 px
        const sizes = await page.evaluate(() => {
          const out: number[] = []
          const walk = document.createTreeWalker(document.querySelector('.studio')!, NodeFilter.SHOW_TEXT)
          for (let n = walk.nextNode(); n; n = walk.nextNode()) {
            const el = n.parentElement!
            if (!n.textContent!.trim() || el.closest('.sr-only') || !el.getClientRects().length) continue
            out.push(parseFloat(getComputedStyle(el).fontSize))
          }
          return out
        })
        expect(Math.min(...sizes)).toBeGreaterThanOrEqual(22)
        expect(await page.evaluate(() => parseFloat(getComputedStyle(document.body).fontSize))).toBe(26)
        // "Corners keep their proportion at 4K": 3 and 4 px at Full HD, twice that here
        const radii = await page.evaluate(() => ({
          control: parseFloat(getComputedStyle(document.querySelector('.studio .btn-ghost')!).borderRadius),
          container: parseFloat(getComputedStyle(document.querySelector('.studio .card')!).borderRadius),
          stage: parseFloat(getComputedStyle(document.querySelector('.studio-stage')!).borderRadius),
        }))
        expect(radii.control).toBeCloseTo(6, 2)
        expect(radii.container).toBeCloseTo(8, 2)
        expect(radii.stage).toBeCloseTo(8, 2)
      }
      if (shots && [1280, 1920, 2560, 3840, 3440, 2340].includes(width)) {
        mkdirSync(shots, { recursive: true })
        await page.screenshot({ path: `${shots}/sizes-${width}x${height}.png` })
      }
      await ctx.close()
    })

  test('Window too small: the owner is asked for a larger window', async ({ browser }) => {
    for (const viewport of [
      { width: 1279, height: 800 },
      { width: 1400, height: 719 },
    ]) {
      const ctx = await ownerContext(browser, viewport)
      const page = await ctx.newPage()
      await page.goto(`/studio/${id}`)
      await expect(page.getByText('Hosting needs a window of at least 1280 by 720 pixels.')).toBeVisible()
      await expect(page.locator('.studio-body')).toBeHidden()
      await ctx.close()
    }
  })
})

test.describe('Guest join page', () => {
  test('Invalid link: a page that says so, not an error', async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: base })
    const page = await ctx.newPage()
    await page.goto('/join/not-a-valid-token')
    await expect(page.getByRole('heading', { name: 'This invite link is not valid' })).toBeVisible()
    await ctx.close()
  })

  test('Preview before joining, Reload keeps the identity, Studio full, axe', async ({ browser }) => {
    test.setTimeout(120_000)
    const owner = await ownerContext(browser)
    const b = await newBroadcast(owner, 'Guest join')
    const studio = await openStudio(owner, b.id)

    const g = await browser.newContext({ colorScheme: 'dark' })
    const page = await g.newPage()
    await page.goto(b.invite_url)
    await expect(page.locator('[data-join-title]')).toHaveText('Guest join')
    // "Preview before joining": the camera plays and the devices can be picked
    await expect
      .poll(() => page.locator('[data-preview]').evaluate((v: HTMLVideoElement) => v.videoWidth))
      .toBeGreaterThan(0)
    await expect(page.getByLabel('Microphone', { exact: true }).locator('option')).not.toHaveCount(1)
    await expect(page.getByLabel('Camera', { exact: true }).locator('option')).not.toHaveCount(1)
    const axe = await new AxeBuilder({ page }).analyze()
    expect(axe.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(' ')}`)).toEqual([])
    await page.getByLabel('Your name').fill('Rafael Lima')
    await page.getByRole('button', { name: 'Enter studio' }).click()
    await expect(place(page)).toHaveText(/Backstage/, { timeout: 20_000 })
    const guestAxe = await new AxeBuilder({ page }).analyze()
    expect(guestAxe.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(' ')}`)).toEqual([])

    // "Reload keeps the identity": on stage, reload, still on stage and still one Rafael
    await studio.getByRole('button', { name: 'Add to stage: Rafael Lima' }).click()
    await expect(place(page)).toHaveText(/On stage/, { timeout: 15_000 })
    await page.reload()
    await expect(place(page)).toHaveText(/On stage/, { timeout: 20_000 })
    await expect(card(studio, 'Rafael Lima')).toHaveCount(1)
    await expect(card(studio, 'Rafael Lima')).toHaveAttribute('data-on-stage', 'true')

    // "Studio full" (studio.max_participants = 3): the owner, Rafael and Carla fill it
    const carla = await guestJoins(browser, b.invite_url, 'Carla Dias')
    const late = await browser.newContext()
    const latePage = await late.newPage()
    await latePage.goto(b.invite_url)
    await latePage.getByLabel('Your name').fill('Late Guest')
    await latePage.getByRole('button', { name: 'Enter studio' }).click()
    await expect(latePage.getByRole('heading', { name: 'The studio is full' })).toBeVisible({ timeout: 20_000 })
    await late.close()
    await carla.ctx.close()
    await g.close()
    await owner.close()
  })

  test('Permission denied: the page explains it and the guest joins without devices', async ({ browser }) => {
    const owner = await ownerContext(browser)
    const b = await newBroadcast(owner, 'No devices')
    const g = await browser.newContext()
    await g.addInitScript(() => {
      const deny = () => Promise.reject(new DOMException('Permission denied', 'NotAllowedError'))
      navigator.mediaDevices.getUserMedia = deny
    })
    const page = await g.newPage()
    await page.goto(b.invite_url)
    await expect(page.getByText('Camera and microphone blocked')).toBeVisible()
    await expect(page.getByText('You can also join without them.')).toBeVisible()
    await page.getByLabel('Your name').fill('Sem Câmera')
    await page.getByRole('button', { name: 'Enter studio' }).click()
    await expect(place(page)).toHaveText(/Backstage/, { timeout: 20_000 })
    await expect(page.locator('[data-control=mic]')).toHaveAttribute('aria-pressed', 'false')
    await expect(page.locator('[data-control=camera]')).toHaveAttribute('aria-pressed', 'false')
    await g.close()
    await owner.close()
  })
})

// ── add-studio-interactions: device menus, tooltips, the meter and the sound cues in a real room ────────────────

// A steady 440 Hz tone at half of full scale as every fake microphone of this part (a WAV Chrome loops): Chrome's
// own beeps are too short to hold a level, and a tone made in the page with Web Audio reaches no other audio context.
const toneFile = join(tmpdir(), 'streamgo-e2e-tone-440.wav')
{
  const rate = 48_000
  const pcm = Buffer.alloc(rate * 2)
  // four syllables a second, each a tone at half of full scale on its own pitch: a constant tone is taken for
  // noise by the browser's noise suppression and comes out of the capture at a tenth of its level
  for (let i = 0; i < rate; i++) {
    const syllable = Math.floor(i / (rate / 4))
    const on = i % (rate / 4) < rate * 0.18
    const f = [440, 330, 550, 390][syllable]!
    pcm.writeInt16LE(on ? Math.round(0.5 * 32767 * Math.sin((2 * Math.PI * f * i) / rate)) : 0, i * 2)
  }
  const head = Buffer.alloc(44)
  head.write('RIFF', 0)
  head.writeUInt32LE(36 + pcm.length, 4)
  head.write('WAVEfmt ', 8)
  head.writeUInt32LE(16, 16)
  head.writeUInt16LE(1, 20)
  head.writeUInt16LE(1, 22)
  head.writeUInt32LE(rate, 24)
  head.writeUInt32LE(rate * 2, 28)
  head.writeUInt16LE(2, 32)
  head.writeUInt16LE(16, 34)
  head.write('data', 36)
  head.writeUInt32LE(pcm.length, 40)
  writeFileSync(toneFile, Buffer.concat([head, pcm]))
}

// audioProbe counts the cue player's tones (window.__cues: oscillators made by createOscillator, which neither the
// mix nor the meter calls) and taps every MediaStreamAudioDestination, the program bus among them, with an analyser
// in the same context (window.__taps), so the program's output can be measured.
function audioProbe() {
  const w = window as unknown as { __cues: number; __taps: AnalyserNode[] }
  w.__cues = 0
  w.__taps = []
  const osc = AudioContext.prototype.createOscillator
  AudioContext.prototype.createOscillator = function () {
    w.__cues++
    return osc.call(this)
  }
  const connect = AudioNode.prototype.connect as (this: AudioNode, ...a: unknown[]) => unknown
  const tapOf = new WeakMap<AudioNode, AnalyserNode>()
  AudioNode.prototype.connect = function (this: AudioNode, ...a: unknown[]) {
    const to = a[0]
    if (to instanceof MediaStreamAudioDestinationNode) {
      let tap = tapOf.get(to)
      if (!tap) {
        tap = to.context.createAnalyser()
        tapOf.set(to, tap)
        w.__taps.push(tap)
      }
      connect.call(this, tap)
    }
    return connect.apply(this, a)
  } as typeof AudioNode.prototype.connect
}

/** the loudest sample on any program tap during `ms` */
const programPeak = (page: Page, ms: number) =>
  page.evaluate(async (ms) => {
    const w = window as unknown as { __taps: AnalyserNode[] }
    let peak = 0
    const end = performance.now() + ms
    while (performance.now() < end) {
      for (const a of w.__taps) {
        const d = new Float32Array(a.fftSize)
        a.getFloatTimeDomainData(d)
        for (const v of d) peak = Math.max(peak, Math.abs(v))
      }
      await new Promise((r) => setTimeout(r, 20))
    }
    return peak
  }, ms)
const cues = (page: Page) => page.evaluate(() => (window as unknown as { __cues: number }).__cues)
const resetCues = (page: Page) => page.evaluate(() => ((window as unknown as { __cues: number }).__cues = 0))

test.describe('Studio interactions', () => {
  // a browser of its own, whose fake microphones play the tone (launch options cannot change inside a group)
  let browser: Browser
  let ctx: BrowserContext
  let studio: Page
  let b: { id: string; invite_url: string }

  test.beforeAll(async ({ playwright }) => {
    browser = await playwright.chromium.launch({
      channel: 'chrome',
      args: [
        '--use-fake-ui-for-media-stream',
        '--use-fake-device-for-media-stream',
        `--use-file-for-fake-audio-capture=${toneFile}`,
      ],
    })
    ctx = await ownerContext(browser)
    await ctx.addInitScript(audioProbe)
    b = await newBroadcast(ctx, 'Interactions')
    studio = await openStudio(ctx, b.id)
  })
  test.afterAll(async () => {
    await ctx?.close()
    await browser?.close()
  })

  const micMenu = (page: Page) => page.locator('[data-device-menu=audio]')
  const camMenu = (page: Page) => page.locator('[data-device-menu=video]')

  test('Choose another microphone: the menu closes, focus is on the arrow, the menu and the select agree', async () => {
    await micMenu(studio).click()
    const items = studio.getByRole('menuitemradio')
    await expect(items.first()).toHaveText('Default')
    await expect(items.first()).toHaveAttribute('aria-checked', 'true')
    // the browser has given permission: the real names, not numbers
    const names = await items.allTextContents()
    expect(names.length).toBeGreaterThanOrEqual(3)
    expect(names.slice(1).some((n) => /^Microphone \d+$/.test(n.trim()))).toBe(false)
    const second = names[2]!.trim()
    await items.nth(2).click()
    await expect(studio.getByRole('menu')).toHaveCount(0)
    await expect(micMenu(studio)).toBeFocused()
    await expect(studio.locator('[data-control=mic]')).toHaveAttribute('aria-pressed', 'true')
    await micMenu(studio).click()
    await expect(studio.getByRole('menuitemradio', { name: second })).toHaveAttribute('aria-checked', 'true')
    await studio.keyboard.press('Escape')
    await studio.getByRole('button', { name: 'Device settings' }).click()
    const select = studio.getByLabel('Microphone', { exact: true })
    expect(await select.evaluate((s: HTMLSelectElement) => s.selectedOptions[0]!.textContent!.trim())).toBe(second)
    await studio.getByRole('button', { name: 'Done' }).click()
    await expect(studio.locator('.toast')).toHaveCount(0)
  })

  test('Keyboard on the camera arrow: Enter opens on Default, Escape closes and changes nothing', async () => {
    await camMenu(studio).focus()
    await studio.keyboard.press('Enter')
    await expect(studio.getByRole('menuitemradio', { name: 'Default' })).toBeFocused()
    await studio.keyboard.press('ArrowDown')
    await studio.keyboard.press('Escape')
    await expect(studio.getByRole('menu')).toHaveCount(0)
    await expect(camMenu(studio)).toBeFocused()
    await studio.keyboard.press('Enter')
    await expect(studio.getByRole('menuitemradio', { name: 'Default' })).toHaveAttribute('aria-checked', 'true')
    await studio.keyboard.press('Escape')
  })

  test('Chosen while off: the camera stays off and turning it on uses the chosen one', async () => {
    await studio.locator('[data-control=camera]').click()
    await expect(studio.locator('[data-control=camera]')).toHaveAttribute('aria-pressed', 'false')
    await camMenu(studio).click()
    const items = studio.getByRole('menuitemradio')
    const last = (await items.allTextContents()).at(-1)!.trim()
    await items.last().click()
    await expect(studio.locator('[data-control=camera]')).toHaveAttribute('aria-pressed', 'false')
    await studio.locator('[data-control=camera]').click()
    await expect(studio.locator('[data-control=camera]')).toHaveAttribute('aria-pressed', 'true')
    await camMenu(studio).click()
    await expect(studio.getByRole('menuitemradio', { name: last })).toHaveAttribute('aria-checked', 'true')
    await studio.keyboard.press('Escape')
  })

  test('Microphone meter: Heard with a tone at half of full scale, empty and silent when off', async () => {
    const meter = studio.getByRole('meter', { name: 'Microphone level' })
    await studio.locator('body').click({ position: { x: 5, y: 5 } }) // the audio starts with a click
    await expect.poll(() => meter.getAttribute('aria-valuenow').then(Number), { timeout: 5000 }).toBeGreaterThan(25)
    await expect(meter).toHaveAttribute('aria-valuetext', 'Heard')
    await studio.locator('[data-control=mic]').click()
    await expect(meter).toHaveAttribute('aria-valuenow', '0')
    await expect(meter).toHaveAttribute('aria-valuetext', 'Silent')
    await studio.locator('[data-control=mic]').click()
    await expect(meter).toHaveAttribute('aria-valuetext', 'Heard')
  })

  test('Tooltips: Device settings on focus, a layout thumbnail on hover; buttons with text and reasons have none', async () => {
    const settings = studio.getByRole('button', { name: 'Device settings' })
    await expect(settings).not.toHaveAttribute('title')
    await tabTo(studio, settings)
    await expect(studio.getByRole('tooltip', { name: 'Device settings' })).toBeVisible()
    await studio.keyboard.press('Escape')
    await expect(studio.getByRole('tooltip', { name: 'Device settings' })).toBeHidden()
    const thumb = studio.locator('[data-layout=spotlight]')
    await expect(thumb).not.toHaveAttribute('title')
    await thumb.hover()
    await expect(studio.getByRole('tooltip', { name: 'Spotlight' })).toBeVisible({ timeout: 2000 })
    await studio.mouse.move(5, 5)
    await expect(studio.getByRole('tooltip', { name: 'Spotlight' })).toBeHidden()
    for (const sel of ['[data-control=mic]', '[data-control=camera]', '[data-control=share]'])
      expect(await studio.locator(sel).getAttribute('aria-labelledby')).toBeNull()
    await expect(card(studio, 'Ana Souza').getByRole('img', { name: /Microphone|Speaking/ })).toBeVisible()
  })

  test('Sound cues: a guest’s join is heard once by the owner and not by the guest; the program is unchanged', async () => {
    // the program bus is silent with an empty stage; it is measured while the cue plays
    await studio.locator('body').click({ position: { x: 5, y: 5 } })
    await resetCues(studio)
    expect(await programPeak(studio, 300)).toBe(0)
    const guest = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
    await guest.addInitScript(audioProbe)
    const page = await guest.newPage()
    await page.goto(b.invite_url)
    await page.getByLabel('Your name').fill('Dora Som')
    await page.getByRole('button', { name: 'Enter studio' }).click()
    const peak = programPeak(studio, 2500)
    await expect(card(studio, 'Dora Som')).toBeVisible({ timeout: 20_000 })
    expect(await peak).toBe(0)
    expect(await cues(studio)).toBe(1)
    expect(await cues(page)).toBe(0)
    // the taps do measure the program: with the host on stage it carries the tone
    await studio.getByRole('button', { name: 'Add to stage: Ana Souza' }).click()
    await expect.poll(() => programPeak(studio, 300), { timeout: 5000 }).toBeGreaterThan(0.1)
    await studio.getByRole('button', { name: 'Take off the stage: Ana Souza' }).click()
    // the guest's menus and meter work as the owner's
    await page.locator('[data-device-menu=audio]').click()
    await expect(page.getByRole('menuitemradio').first()).toHaveText('Default')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('meter', { name: 'Microphone level' })).toHaveAttribute('aria-valuetext', 'Heard', {
      timeout: 5000,
    })
    await guest.close()
    await expect(card(studio, 'Dora Som')).toHaveCount(0, { timeout: 20_000 })
    await studio.waitForTimeout(500)
    expect(await cues(studio)).toBe(2) // the leave cue
  })

  test('Sounds turned off in Device settings: no cue, also after a reload', async () => {
    await studio.getByRole('button', { name: 'Device settings' }).click()
    const sounds = studio.locator('[data-sounds-on]')
    await expect(sounds).toBeChecked()
    await expect(studio.locator('[data-sounds-on-air]')).not.toBeChecked()
    await sounds.uncheck()
    await studio.getByRole('button', { name: 'Done' }).click()
    await studio.reload()
    await expect(studio.locator('[data-card]').first()).toBeVisible({ timeout: 20_000 })
    await studio.locator('body').click({ position: { x: 5, y: 5 } })
    await resetCues(studio)
    const guest = await guestJoins(browser, b.invite_url, 'Eva Mudo')
    await expect(card(studio, 'Eva Mudo')).toBeVisible({ timeout: 20_000 })
    await studio.waitForTimeout(1000)
    expect(await cues(studio)).toBe(0)
    await guest.ctx.close()
    await studio.getByRole('button', { name: 'Device settings' }).click()
    await studio.locator('[data-sounds-on]').check()
    await studio.getByRole('button', { name: 'Done' }).click()
  })

  test('In every theme: a menu open, a tooltip shown and the meter running meet the contrast rules', async () => {
    await expect(studio.locator('.toast')).toHaveCount(0, { timeout: 15_000 })
    await micMenu(studio).focus()
    await studio.keyboard.press('Enter')
    await studio.keyboard.press('ArrowDown')
    await studio.getByRole('button', { name: 'Device settings' }).hover()
    await expect(studio.getByRole('tooltip', { name: 'Device settings' })).toBeVisible({ timeout: 2000 })
    await expect(studio.getByRole('menu')).toBeVisible()
    const failures: string[] = []
    for (const theme of themeCatalog) {
      await setTheme(studio, theme.id)
      const r = await new AxeBuilder({ page: studio }).analyze()
      for (const v of r.violations) for (const n of v.nodes) failures.push(`${theme.id}: ${v.id} ${n.target}`)
    }
    await setTheme(studio, 'streamgo-dark')
    await studio.keyboard.press('Escape')
    expect(failures).toEqual([])
  })

  for (const [name, viewport, touch] of [
    ['1280x720', { width: 1280, height: 720 }, false],
    ['an ultrawide window', { width: 3440, height: 1440 }, false],
    ['a narrow touch window', { width: 412, height: 860 }, true],
  ] as const)
    test(`The open menu in ${name}: inside the window, no page scroll`, async () => {
      const g = await browser.newContext({ viewport, hasTouch: touch, isMobile: touch, colorScheme: 'dark' })
      const page = await g.newPage()
      await page.goto(b.invite_url)
      await page.getByLabel('Your name').fill(`Guest ${viewport.width}`)
      await page.getByRole('button', { name: 'Enter studio' }).click()
      await expect(place(page)).toBeVisible({ timeout: 20_000 })
      const arrow = page.locator('[data-device-menu=audio]')
      if (touch) await arrow.tap()
      else await arrow.click()
      const menu = page.getByRole('menu')
      await expect(menu).toBeVisible()
      const fit = await menu.evaluate((e) => {
        const r = e.getBoundingClientRect()
        const d = document.documentElement
        return {
          inside: r.left >= 0 && r.top >= 0 && r.right <= innerWidth + 0.5 && r.bottom <= innerHeight + 0.5,
          page: d.scrollHeight <= d.clientHeight && d.scrollWidth <= d.clientWidth,
        }
      })
      expect(fit).toEqual({ inside: true, page: true })
      await g.close()
    })

  test('1280x720: the dock with its arrows and meter still fits its row beside the layout picker', async () => {
    const c = await ownerContext(browser, { width: 1280, height: 720 })
    const page = await openStudio(c, b.id)
    const fit = await page.evaluate(() => {
      const row = document.querySelector('.studio-row')!
      const dock = document.querySelector('[role=toolbar]')!
      return [row.scrollWidth <= row.clientWidth, dock.scrollWidth <= dock.clientWidth]
    })
    expect(fit).toEqual([true, true])
    await c.close()
  })

  test('screenshots of the interactions at 1440x810 in three themes', async () => {
    test.skip(!shots, 'E2E_SHOTS not set')
    mkdirSync(shots, { recursive: true })
    const c = await ownerContext(browser, { width: 1440, height: 810 })
    await c.addInitScript(audioProbe)
    const page = await openStudio(c, b.id)
    await page.locator('body').click({ position: { x: 5, y: 5 } })
    const dock = page.locator('[role=toolbar]')
    for (const theme of ['streamgo-dark', 'macos', 'classic-gray']) {
      await setTheme(page, theme)
      await page.locator('[data-device-menu=audio]').click()
      await page.keyboard.press('ArrowDown')
      await page.waitForTimeout(300)
      await page.screenshot({ path: `${shots}/dock-menu-${theme}.png` })
      await page.keyboard.press('Escape')
      await tabTo(page, page.getByRole('button', { name: 'Device settings' }))
      await expect(page.getByRole('tooltip', { name: 'Device settings' })).toBeVisible()
      await page.screenshot({ path: `${shots}/dock-tooltip-${theme}.png` })
      await page.locator('body').click({ position: { x: 5, y: 5 } })
      await expect(page.getByRole('meter', { name: 'Microphone level' })).toHaveAttribute('aria-valuetext', 'Heard')
      await dock.screenshot({ path: `${shots}/dock-meter-${theme}.png` })
      await page.screenshot({ path: `${shots}/meter-${theme}.png` })
      await page.getByRole('button', { name: 'Device settings' }).click()
      await expect(page.locator('[data-sounds]')).toBeVisible()
      await page.waitForTimeout(400) // the dialog's entry
      await page.screenshot({ path: `${shots}/sounds-${theme}.png` })
      await page.getByRole('button', { name: 'Done' }).click()
    }
    await c.close()
  })
})
