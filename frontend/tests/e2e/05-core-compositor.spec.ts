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

// name-tags (add-name-tag-styles): drawNameTag on a grey 640x360 tile of a 1280x720 canvas. With the tile's height
// the font is 19 px and the padding 9.5 px, so the plates sit at known rows (see drawNameTag).
const GREY: RGB = [128, 128, 128]
const DARK_PLATE: RGB = [49, 49, 49] // TAG (62 % black) over grey
const STRIP_DARK: RGB = [51, 51, 51] // 60 % black over grey
const ORANGE: RGB = [255, 136, 0]

const TAGS = `
  const tag = (style, opts = {}) => {
    const canvas = Object.assign(document.createElement('canvas'), { width: 1280, height: 720 })
    const g = canvas.getContext('2d')
    g.fillStyle = '#808080'; g.fillRect(0, 0, 1280, 720)
    L.drawNameTag(g, { x: 0, y: 0, w: 640, h: 360 }, opts.name ?? 'Ana Souza', opts.tagline ?? '', opts.accent ?? '#ff8800', style)
    const at = (x, y) => [...g.getImageData(x, y, 1, 1).data.slice(0, 3)]
    // the brightest and darkest pixels (by their lowest and highest channel) in a box
    const box = (x0, y0, x1, y1) => {
      const d = g.getImageData(x0, y0, x1 - x0, y1 - y0).data
      let hi = 0, lo = 255
      for (let i = 0; i < d.length; i += 4) {
        hi = Math.max(hi, Math.min(d[i], d[i + 1], d[i + 2]))
        lo = Math.min(lo, Math.max(d[i], d[i + 1], d[i + 2]))
      }
      return { hi, lo }
    }
    return { g, at, box, data: () => [...g.getImageData(0, 0, 640, 360).data] }
  }
  await document.fonts.load("600 19px 'Inter'")
`

test('Each style draws what it says', async ({ page }) => {
  const r = await run<Record<string, unknown>>(
    page,
    `${TAGS}
    const bar = tag('bar'), solid = tag('solid'), strip = tag('strip'), text = tag('text')
    const two = tag('two-line', { tagline: 'Head of Product' })
    return {
      barStripe: bar.at(16, 330), barPlate: bar.at(25, 330), barText: bar.box(29, 316, 140, 343).hi,
      solidPlate: solid.at(20, 330),
      stripLeft: strip.at(3, 350), stripRight: strip.at(636, 350), stripAbove: strip.at(636, 300),
      textPlate: text.at(300, 330), textLine: text.at(16, 339), textLetters: text.box(14, 314, 120, 334).hi,
      twoStripeTop: two.at(16, 300), twoStripeBottom: two.at(16, 340), twoTop: two.at(25, 297),
      twoName: two.box(29, 300, 160, 317).hi, twoTagline: two.box(29, 320, 200, 335).hi,
      barTop: bar.at(25, 310),
    }
  `,
  )
  test.info().annotations.push({ type: 'pixels', description: JSON.stringify(r) })
  const px = r as Record<string, RGB>
  // bar: a dark plate with a strip in the brand colour at its left
  expect(near(px.barStripe!, ORANGE, 4)).toBe(true)
  expect(near(px.barPlate!, DARK_PLATE, 6)).toBe(true)
  expect(r.barText).toBeGreaterThanOrEqual(240)
  expect(near(px.barTop!, GREY, 4)).toBe(true) // a one-line plate
  // solid: a plate in the brand colour
  expect(near(px.solidPlate!, ORANGE, 4)).toBe(true)
  // strip: a translucent dark strip as wide as the tile at its bottom
  expect(near(px.stripLeft!, STRIP_DARK, 6)).toBe(true)
  expect(near(px.stripRight!, STRIP_DARK, 6)).toBe(true)
  expect(near(px.stripAbove!, GREY, 4)).toBe(true)
  // text: no plate, white letters and a line in the brand colour under the name
  expect(near(px.textPlate!, GREY, 4)).toBe(true)
  expect(near(px.textLine!, ORANGE, 4)).toBe(true)
  expect(r.textLetters).toBeGreaterThanOrEqual(240)
  // two lines: the name above the tagline on one (taller) plate
  expect(near(px.twoStripeTop!, ORANGE, 4)).toBe(true)
  expect(near(px.twoStripeBottom!, ORANGE, 4)).toBe(true)
  expect(near(px.twoTop!, DARK_PLATE, 6)).toBe(true)
  expect(r.twoName).toBeGreaterThanOrEqual(240)
  expect(r.twoTagline).toBeGreaterThanOrEqual(180)
})

test('Readable on any brand colour', async ({ page }) => {
  const r = await run<{ light: string; dark: string; ratios: number[]; lightText: number; darkText: number }>(
    page,
    `${TAGS}
    const lum = (hex) => {
      const c = [1, 3, 5].map((i) => { const v = parseInt(hex.slice(i, i + 2), 16) / 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4 })
      return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
    }
    const ratio = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05) }
    // the presets, extremes, the white/black crossover (#777777, #7a7a7a) and every grey
    const greys = Array.from({ length: 256 }, (_, i) => '#' + i.toString(16).padStart(2, '0').repeat(3))
    const colours = ['#f2f4fb', '#fbbf24', '#ffff00', '#101030', '#000000', '#3d41cf', '#808080', '#4ade80', '#f0464f', '#b3b8ff', '#777777', '#7a7a7a', ...greys]
    return {
      light: L.inkOn('#f2f4fb'), dark: L.inkOn('#101030'),
      ratios: colours.map((c) => ratio(c, L.inkOn(c))),
      lightText: tag('solid', { accent: '#f2f4fb' }).box(27, 316, 120, 343).lo,
      darkText: tag('solid', { accent: '#101030' }).box(27, 316, 120, 343).hi,
    }
  `,
  )
  test.info().annotations.push({ type: 'contrast', description: JSON.stringify(r) })
  expect(r.light).toBe('#000000') // black on the light one
  expect(r.dark).toBe('#ffffff') // white on the dark one
  expect(Math.min(...r.ratios)).toBeGreaterThanOrEqual(4.5)
  expect(r.lightText).toBeLessThanOrEqual(30)
  expect(r.darkText).toBeGreaterThanOrEqual(240)
})

test('A long name', async ({ page }) => {
  // every layout's smallest tile, from one to six sources (studio.max_on_stage), in the smallest program frame
  // (720p) and in a guest's preview (960x540), each drawn in every style
  const r = await run<{ where: string; texts: string[]; outside: number }[]>(
    page,
    `
    await document.fonts.load("600 19px 'Inter'")
    const out = []
    const long = 'Maria Aparecida dos Santos Albuquerque de Oliveira '.repeat(6).trim() // wider than any tile
    for (const [W, H] of [[1280, 720], [960, 540]]) {
      const canvas = Object.assign(document.createElement('canvas'), { width: W, height: H })
      const g = canvas.getContext('2d', { willReadFrequently: true })
      const texts = []
      const fill = g.fillText.bind(g)
      g.fillText = (t, x, y) => { texts.push(t); fill(t, x, y) }
      for (const layout of L.LAYOUTS) {
        const tiles = []
        for (let n = 1; n <= 6; n++) {
          const sources = Array.from({ length: n }, (_, i) => ({ p: 'p' + i, k: i === n - 1 ? 'screen' : 'camera' }))
          tiles.push(...L.layoutRects(layout, sources, W, H).map((x) => x.rect))
        }
        const t = tiles.reduce((a, b) => (b.w * b.h < a.w * a.h ? b : a))
        for (const style of L.NAME_STYLES) {
          g.fillStyle = '#00ff00'; g.fillRect(0, 0, W, H)
          g.fillStyle = '#808080'; g.fillRect(t.x, t.y, t.w, t.h)
          texts.length = 0
          L.drawNameTag(g, t, long, long, '#ff8800', style)
          const d = g.getImageData(0, 0, W, H).data
          let outside = 0
          for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
            if (x >= t.x && x < t.x + t.w && y >= t.y && y < t.y + t.h) continue
            const i = (y * W + x) * 4
            if (d[i] !== 0 || d[i + 1] !== 255 || d[i + 2] !== 0) outside++
          }
          out.push({ where: W + 'x' + H + ' ' + layout + ' ' + JSON.stringify(t) + ' ' + style, texts: [...texts], outside })
        }
      }
    }
    return out
  `,
  )
  expect(r).toHaveLength(2 * 6 * 5)
  for (const { where, texts, outside } of r) {
    expect(outside, where).toBe(0) // nothing drawn outside the tile
    expect(texts.length, where).toBeGreaterThan(0)
    for (const t of texts) expect(t.endsWith('…'), where).toBe(true)
  }
})

test('No second line, and Another style ignores it', async ({ page }) => {
  const r = await run<{ noSecondLine: boolean; ignored: boolean; twoDiffers: boolean }>(
    page,
    `${TAGS}
    const same = (a, b) => a.length === b.length && a.every((v, i) => v === b[i])
    const bar = tag('bar').data()
    return {
      noSecondLine: same(tag('two-line').data(), bar),
      ignored: same(tag('bar', { tagline: 'Head of Product' }).data(), bar),
      twoDiffers: !same(tag('two-line', { tagline: 'Head of Product' }).data(), bar),
    }
  `,
  )
  expect(r.noSecondLine).toBe(true) // two lines without a tagline: the name alone on one line
  expect(r.ignored).toBe(true) // bar ignores the tagline
  expect(r.twoDiffers).toBe(true)
})

// A camera (red) and a screen (green) of the same person in a grid, and a screen not being shared: the name tag's
// bar is looked for at the bottom left of each tile.
const SCREENS = `
  await document.fonts.load("600 19px 'Inter'")
  const canvas = Object.assign(document.createElement('canvas'), { width: 1280, height: 720 })
  const c = new L.Compositor(canvas, 'program')
  const red = L.videoSource('#ff0000'), green = L.videoSource('#00ff00')
  const sources = [{ p: 'a', k: 'camera' }, { p: 'a', k: 'screen' }, { p: 'b', k: 'screen' }]
  const show = async (extra) => {
    c.setStage(L.parseStage({ v: 1, on_air: false, layout: 'grid', accent: '#ff8800', logo: null, banner: null,
      sources, taglines: { a: 'Head of Product' }, ...extra }))
    await new Promise((r) => setTimeout(r, 400))
  }
  c.setSources([
    { identity: 'a', kind: 'camera', name: 'Ana Souza', track: red.track },
    { identity: 'a', kind: 'screen', name: 'Ana Souza', track: green.track },
    { identity: 'b', kind: 'screen', name: 'Bruno Lima', track: null },
  ])
  const g = canvas.getContext('2d')
  const at = (x, y) => [...g.getImageData(Math.round(x), Math.round(y), 1, 1).data.slice(0, 3)]
  const rects = L.layoutRects('grid', sources, 1280, 720)
  const stripe = (i) => {
    const r = rects[i].rect
    const fs = Math.max(10, Math.round(Math.min(720 * 0.026, r.h * 0.08)))
    return at(r.x + fs * 0.75 + 1, r.y + r.h - fs * 0.75 - fs * 0.3)
  }
  const centre = (i) => at(rects[i].rect.x + rects[i].rect.w / 2, rects[i].rect.y + rects[i].rect.h / 2)
  const done = () => { c.stop(); red.stop(); green.stop() }
`

test('Off the screens, and The placeholder stays', async ({ page }) => {
  const r = await run<Record<string, RGB>>(
    page,
    `${SCREENS}
    await show({})
    const out = { camOn: stripe(0), screenOn: stripe(1) }
    await show({ hide_screen_names: true })
    Object.assign(out, { camOff: stripe(0), screenOff: stripe(1), placeholder: centre(2) })
    done()
    return out
  `,
  )
  test.info().annotations.push({ type: 'pixels', description: JSON.stringify(r) })
  expect(near(r.camOn!, ORANGE, 4)).toBe(true)
  expect(near(r.screenOn!, ORANGE, 4)).toBe(true)
  expect(near(r.camOff!, ORANGE, 4)).toBe(true) // the camera keeps its name
  expect(near(r.screenOff!, GREEN)).toBe(true) // the screen's tile has none
  expect(near(r.placeholder!, ORANGE)).toBe(true) // the initials of a screen not shared are not a name tag
})

test('Hidden names win', async ({ page }) => {
  const r = await run<Record<string, RGB>>(
    page,
    `${SCREENS}
    const out = {}
    for (const style of L.NAME_STYLES) {
      await show({ hide_names: true, name_style: style })
      out[style] = stripe(0)
    }
    await show({ hide_names: false, name_style: 'solid' })
    out.shownAgain = stripe(0)
    done()
    return out
  `,
  )
  test.info().annotations.push({ type: 'pixels', description: JSON.stringify(r) })
  for (const style of ['bar', 'solid', 'strip', 'text', 'two-line']) expect(near(r[style]!, RED)).toBe(true)
  expect(near(r.shownAgain!, ORANGE, 4)).toBe(true) // the style chosen is kept
})
