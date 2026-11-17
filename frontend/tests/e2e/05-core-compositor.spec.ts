// studio-ui "The stage is the program": src/core/compositor.ts in a real Chrome with generated sources. The pixels
// of known rectangles for a layout (sources, placeholder, absent participant, name tag, logo, banner), and the
// program's frame pacing and freshness over a few seconds with the tab visible. The five-minute hidden-tab run
// through LiveKit is the spike's (docs/spike-media-path.md, 1.3) and is repeated end to end in task 4.3.
import { expect, test, type Page } from '@playwright/test'

const run = <T>(page: Page, fn: string) =>
  page.evaluate(`(async () => { const L = window.lab; ${fn} })()`) as Promise<T>

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/e2e/fixtures/core-lab.html')
  await page.waitForFunction(() => (window as unknown as { labReady?: boolean }).labReady)
})

type RGB = [number, number, number]
const near = (got: RGB, want: RGB, tol = 40) => got.every((v, i) => Math.abs(v - want[i]!) <= tol)

const RED: RGB = [255, 0, 0]
const GREEN: RGB = [0, 255, 0]
const BLUE: RGB = [0, 0, 255]
const ACCENT: RGB = [255, 136, 0]
const WHITE: RGB = [255, 255, 255]
const TILE: RGB = [0x1b, 0x1d, 0x24]

test('draws the layout rectangles, a placeholder, the name tags, the logo and the banner', async ({ page }) => {
  // spotlight at 1280x720 with four sources on stage, one of a participant who left:
  // a (red video) featured at 0..896; b (green video) at 896..1280 x 0..360; c (camera off) at 896..1280 x 360..720
  const px = await run<Record<string, RGB>>(
    page,
    `
    const canvas = Object.assign(document.createElement('canvas'), { width: 1280, height: 720 })
    document.body.append(canvas)
    const c = new L.Compositor(canvas, 'program')
    const red = L.videoSource('#ff0000'), green = L.videoSource('#00ff00')
    const logo = Object.assign(document.createElement('canvas'), { width: 100, height: 100 })
    const lg = logo.getContext('2d'); lg.fillStyle = '#0000ff'; lg.fillRect(0, 0, 100, 100)
    const stage = { v: 1, on_air: true, layout: 'spotlight', accent: '#ff8800', logo: logo.toDataURL(),
      banner: { id: 'b1', text: 'Q3 roadmap: what ships next' },
      sources: [{ p: 'a', k: 'camera' }, { p: 'gone', k: 'camera' }, { p: 'b', k: 'camera' }, { p: 'c', k: 'camera' }] }
    c.setStage(L.parseStage(stage))
    const people = [
      { identity: 'a', kind: 'camera', name: 'Ana Souza', track: red.track },
      { identity: 'b', kind: 'camera', name: 'Bruno Lima', track: green.track },
      { identity: 'c', kind: 'camera', name: 'Carla Dias', track: null },
    ]
    c.setSources(people)
    await new Promise((r) => setTimeout(r, 1000))
    const g = canvas.getContext('2d')
    const at = (x, y) => [...g.getImageData(x, y, 1, 1).data.slice(0, 3)]
    const out = {
      featured: at(450, 200), second: at(1150, 100), placeholder: at(1088, 540), tile: at(906, 370),
      tag: at(16, 690), logo: at(1230, 50), stripe: at(55, 610), banner: at(64, 588),
    }
    // the participant who left comes back, camera off: the right column now has three tiles
    c.setSources([...people, { identity: 'gone', kind: 'camera', name: 'Gil', track: null }])
    await new Promise((r) => setTimeout(r, 300))
    out.returned = at(1088, 120)
    out.secondMoved = at(1150, 300)
    window.stopAll = () => { c.stop(); red.stop(); green.stop() }
    return out
  `,
  )
  test.info().annotations.push({ type: 'pixels', description: JSON.stringify(px) })
  expect(near(px.featured!, RED)).toBe(true)
  expect(near(px.second!, GREEN)).toBe(true) // the absent source is not drawn: b has the top half
  expect(near(px.placeholder!, ACCENT)).toBe(true) // initials on an accent disc
  expect(near(px.tile!, TILE, 4)).toBe(true)
  expect(near(px.tag!, ACCENT, 4)).toBe(true) // the name tag's accent bar
  expect(near(px.logo!, BLUE, 4)).toBe(true)
  expect(near(px.stripe!, ACCENT, 4)).toBe(true)
  expect(near(px.banner!, WHITE, 4)).toBe(true)
  expect(near(px.returned!, ACCENT)).toBe(true) // the returning participant's placeholder, top of the column
  expect(near(px.secondMoved!, GREEN)).toBe(true) // b moved to the middle third
})

test('the program has a frame every 1/30 s on an exact grid, with a fresh picture of the source in each', async ({
  page,
}) => {
  const r = await run<{ perSecond: number[]; freshPerSecond: number[]; offGrid: number; frames: number }>(
    page,
    `
    const canvas = Object.assign(document.createElement('canvas'), { width: 1280, height: 720 })
    const c = new L.Compositor(canvas, 'program')
    const src = L.videoSource('#204060')
    c.setStage(L.parseStage({ v: 1, on_air: true, layout: 'solo', accent: '#3d41cf', logo: null, banner: null,
      sources: [{ p: 'a', k: 'camera' }] }))
    c.setSources([{ identity: 'a', kind: 'camera', name: 'Ana', track: src.track }])
    await new Promise((r) => setTimeout(r, 1000)) // start-up
    // read the program as the WebRTC sender would; the counter corner (0..64) is drawn at 0..128
    const rd = new MediaStreamTrackProcessor({ track: c.stream.getVideoTracks()[0] }).readable.getReader()
    const oc = new OffscreenCanvas(1280, 720), og = oc.getContext('2d', { willReadFrequently: true })
    const seen = []
    const end = performance.now() + 4000
    while (performance.now() < end) {
      const { value: f } = await rd.read()
      og.drawImage(f, 0, 0)
      seen.push({ ts: f.timestamp, v: Math.round(og.getImageData(64, 64, 1, 1).data[0] / 32) })
      f.close()
    }
    rd.cancel(); c.stop(); src.stop()
    const step = 1e6 / 30, t0 = seen[0].ts
    const perSecond = [0, 0, 0], freshPerSecond = [0, 0, 0]
    let offGrid = 0
    seen.forEach((s, i) => {
      const k = (s.ts - t0) / step
      if (Math.abs(k - Math.round(k)) * step > 1) offGrid++
      const sec = Math.floor(Math.round(k) / 30)
      if (sec > 2) return
      perSecond[sec]++
      if (i > 0 && s.v !== seen[i - 1].v) freshPerSecond[sec]++
    })
    return { perSecond, freshPerSecond, offGrid, frames: seen.length }
  `,
  )
  test.info().annotations.push({ type: 'pacing', description: JSON.stringify(r) })
  expect(r.offGrid).toBe(0)
  for (const n of r.perSecond) expect(n).toBeGreaterThanOrEqual(29)
  // the spec's bar for the destination is 25 fresh pictures per second; the spike measured 28 or more
  for (const n of r.freshPerSecond) expect(n).toBeGreaterThanOrEqual(25)
})

test('a guest preview draws <video> elements and captures nothing', async ({ page }) => {
  const r = await run<{ pixel: RGB; stream: boolean }>(
    page,
    `
    const canvas = Object.assign(document.createElement('canvas'), { width: 640, height: 360 })
    const c = new L.Compositor(canvas, 'preview')
    const src = L.videoSource('#ff0000')
    c.setStage(L.parseStage({ v: 1, on_air: false, layout: 'solo', accent: '#3d41cf', logo: null, banner: null,
      sources: [{ p: 'a', k: 'camera' }] }))
    c.setSources([{ identity: 'a', kind: 'camera', name: 'Ana', track: src.track }])
    await new Promise((r) => setTimeout(r, 1000))
    const pixel = [...canvas.getContext('2d').getImageData(320, 180, 1, 1).data.slice(0, 3)]
    c.stop(); src.stop()
    return { pixel, stream: c.stream !== null }
  `,
  )
  expect(near(r.pixel, RED)).toBe(true)
  expect(r.stream).toBe(false)
})
