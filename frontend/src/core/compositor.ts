// The compositor of design D2: draws the program on a canvas from the stage state and the participants' sources.
// The program does not follow the UI theme, so its colours are literal here (the style check excludes this file).
//
// Two modes, as the spike proved (docs/spike-media-path.md, 1.3; spike/web/owner.html with gen=1&proc=2):
// - 'program' (the owner): each source is read through a MediaStreamTrackProcessor into a small jitter buffer, a
//   Web Worker timer drives the ticks, and every tick is emitted through a MediaStreamTrackGenerator with a
//   timestamp on an exact 30 fps grid. This keeps fresh pictures in a hidden tab; `stream` is what gets published.
// - 'preview' (guests): <video> elements drawn on requestAnimationFrame, nothing captured.
import { layoutRects, presentSources, type Rect } from './layout'
import type { NameStyle, SourceKind, StageSource, StageState } from './stageState'

export const PROGRAM_FPS = 30

/** one source a participant offers; the studio feeds every present participant's camera and screen */
export interface Source {
  identity: string
  kind: SourceKind
  /** the participant's display name, for the name tag and the placeholder */
  name: string
  /** null when the camera is off or the screen is not shared: drawn as a placeholder */
  track: MediaStreamTrack | null
}

export type CompositorMode = 'program' | 'preview'

const BACKDROP = '#0e0f13'
const TILE = '#1b1d24'
const DEFAULT_ACCENT = '#3d41cf'
/** the accent colours the studio's Brand tab offers (program colours, not the UI theme's) */
export const ACCENTS = [DEFAULT_ACCENT, '#b3b8ff', '#4ade80', '#fbbf24', '#f0464f', '#f2f4fb'] as const
const WHITE = '#ffffff'
const INK = '#111318'
const BLACK = '#000000'
const TAG = 'rgba(0, 0, 0, 0.62)'
const STRIP = 'rgba(0, 0, 0, 0.6)'
const SHADOW = 'rgba(0, 0, 0, 0.9)'
const SUB = '#d6d9e4' // the second line, a step below the name
// the bundled face, so name tags and banners look the same from any host's computer
const FONT = "'Inter', sans-serif"

interface Picture {
  img: CanvasImageSource
  w: number
  h: number
}

interface Feed {
  tick(n: number): void
  picture(): Picture | null
  close(): void
}

// FrameFeed reads a track's frames and shows each one about two ticks after it arrived: a jitter buffer that keeps
// a 30 fps source at 30 fresh pictures per second (one taken per tick), at the cost of about 66 ms. A frame also
// goes out once it has waited two ticks, so a slow source (a still screen share) is not held back.
class FrameFeed implements Feed {
  private queue: { frame: VideoFrame; at: number }[] = []
  private shown: VideoFrame | null = null
  private now = 0
  private scratch = new OffscreenCanvas(1, 1)
  private scratchG = this.scratch.getContext('2d')!
  private reader: ReadableStreamDefaultReader<VideoFrame>

  constructor(track: MediaStreamTrack) {
    this.reader = new MediaStreamTrackProcessor({ track }).readable.getReader()
    void this.pump()
  }

  private async pump() {
    for (;;) {
      const r = await this.reader.read().catch(() => null)
      if (!r || r.done) return
      this.queue.push({ frame: this.copy(r.value), at: this.now })
      while (this.queue.length > 4) this.queue.shift()!.frame.close()
    }
  }

  // A camera lends its frames from a small pool: holding a few of them in the queue starved the owner's own camera
  // to half its rate (15 of 30 fps measured, task 4.3). Each frame is copied and given back at once.
  private copy(f: VideoFrame): VideoFrame {
    const c = this.scratch
    if (c.width !== f.displayWidth || c.height !== f.displayHeight) {
      c.width = f.displayWidth
      c.height = f.displayHeight
    }
    this.scratchG.drawImage(f, 0, 0)
    const out = new VideoFrame(c, { timestamp: f.timestamp })
    f.close()
    return out
  }

  tick(n: number) {
    this.now = n
    const head = this.queue[0]
    if (head && (this.queue.length > 2 || !this.shown || n - head.at >= 2)) {
      this.shown?.close()
      this.shown = this.queue.shift()!.frame
    }
  }

  picture() {
    const f = this.shown
    return f ? { img: f, w: f.displayWidth, h: f.displayHeight } : null
  }

  close() {
    void this.reader.cancel().catch(() => {})
    for (const q of this.queue) q.frame.close()
    this.queue = []
    this.shown?.close()
    this.shown = null
  }
}

class VideoFeed implements Feed {
  private video = document.createElement('video')

  constructor(track: MediaStreamTrack) {
    this.video.muted = true
    this.video.playsInline = true
    this.video.srcObject = new MediaStream([track])
    void this.video.play().catch(() => {})
  }

  tick() {}

  picture() {
    const v = this.video
    return v.readyState >= 2 && v.videoWidth > 0 ? { img: v, w: v.videoWidth, h: v.videoHeight } : null
  }

  close() {
    this.video.pause()
    this.video.srcObject = null
  }
}

/** initials of a display name: the first letters of its first and last words */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  const first = [...words[0]!][0]!
  const last = words.length > 1 ? [...words[words.length - 1]!][0]! : ''
  return (first + last).toUpperCase()
}

export class Compositor {
  /** the program's video track ('program' mode only): publish this */
  readonly stream: MediaStream | null = null
  /** pictures drawn since the start, for diagnostics */
  frames = 0

  private g: CanvasRenderingContext2D
  private stage: StageState | null = null
  private sources: Source[] = []
  private feeds = new Map<MediaStreamTrack, Feed>()
  private logo: { url: string; img: HTMLImageElement } | null = null
  private writer: WritableStreamDefaultWriter<VideoFrame> | null = null
  private worker: Worker | null = null
  private raf = 0
  private t0 = 0

  // The canvas's width and height are the program's size (1280x720 or 1920x1080 for the owner, smaller for a
  // guest's preview); set them before creating the compositor.
  constructor(
    readonly canvas: HTMLCanvasElement,
    private readonly mode: CompositorMode,
  ) {
    this.g = canvas.getContext('2d')!
    // a canvas draws with a web font only once it is loaded: ask for the weight the program uses
    void document.fonts?.load(`600 16px ${FONT}`)
    if (mode === 'program') {
      const gen = new MediaStreamTrackGenerator({ kind: 'video' })
      this.writer = gen.writable.getWriter()
      this.stream = new MediaStream([gen])
      this.worker = new Worker(new URL('./compositorClock.ts', import.meta.url), { type: 'module' })
      this.worker.onmessage = (e: MessageEvent<number>) => this.tick(e.data)
      this.worker.postMessage({ fps: PROGRAM_FPS })
    } else {
      let n = 0
      const loop = () => {
        this.tick(n++)
        this.raf = requestAnimationFrame(loop)
      }
      this.raf = requestAnimationFrame(loop)
    }
  }

  /** setStage applies a new stage state (from the API's answer or the room metadata, through StageFeed) */
  setStage(stage: StageState) {
    this.stage = stage
    if (stage.logo !== (this.logo?.url ?? null)) {
      this.logo = null
      if (stage.logo) {
        const img = new Image()
        img.src = stage.logo
        this.logo = { url: stage.logo, img }
      }
    }
    this.sync()
  }

  /** setSources replaces the sources of the participants in the room; a participant with no entry is absent */
  setSources(sources: Source[]) {
    this.sources = [...sources]
    this.sync()
  }

  stop() {
    this.worker?.terminate()
    cancelAnimationFrame(this.raf)
    for (const f of this.feeds.values()) f.close()
    this.feeds.clear()
    void this.writer?.close().catch(() => {})
    this.stream?.getTracks().forEach((t) => t.stop())
  }

  // sync reads only the tracks that are on stage; a replaced or removed track's reader is closed.
  private sync() {
    const wanted = new Set(this.onStage().flatMap((s) => (this.entry(s)?.track ? [this.entry(s)!.track!] : [])))
    for (const [track, feed] of this.feeds)
      if (!wanted.has(track)) {
        feed.close()
        this.feeds.delete(track)
      }
    for (const track of wanted)
      if (!this.feeds.has(track))
        this.feeds.set(track, this.mode === 'program' ? new FrameFeed(track) : new VideoFeed(track))
  }

  private onStage(): StageSource[] {
    return this.stage ? presentSources(this.stage.sources, new Set(this.sources.map((s) => s.identity))) : []
  }

  private entry(s: StageSource) {
    return this.sources.find((x) => x.identity === s.p && x.kind === s.k)
  }

  private tick(n: number) {
    for (const f of this.feeds.values()) f.tick(n)
    this.draw()
    this.frames++
    if (this.writer) {
      this.t0 ||= performance.now() * 1000
      const f = new VideoFrame(this.canvas, { timestamp: Math.round(this.t0 + (n * 1e6) / PROGRAM_FPS) })
      this.writer.write(f).catch(() => {})
      f.close()
    }
  }

  private draw() {
    const g = this.g
    const { width: W, height: H } = this.canvas
    g.fillStyle = BACKDROP
    g.fillRect(0, 0, W, H)
    const stage = this.stage
    if (!stage) return
    const accent = stage.accent || DEFAULT_ACCENT
    const placed = layoutRects(stage.layout, this.onStage(), W, H)
    for (const { source, rect } of placed)
      this.drawTile(source, rect, accent, !stage.hide_names && !(source.k === 'screen' && stage.hide_screen_names))

    const logo = this.logo?.img
    if (logo?.complete && logo.naturalWidth > 0) {
      const lh = placed.length === 0 ? H * 0.3 : H * 0.08
      const lw = (logo.naturalWidth / logo.naturalHeight) * lh
      if (placed.length === 0) g.drawImage(logo, (W - lw) / 2, (H - lh) / 2, lw, lh)
      else g.drawImage(logo, W - lw - H * 0.03, H * 0.03, lw, lh)
    }
    if (stage.banner) this.drawBanner(stage.banner.text, accent)
  }

  private drawTile(s: StageSource, r: Rect, accent: string, tag: boolean) {
    const g = this.g
    const name = this.sources.find((x) => x.identity === s.p)?.name ?? ''
    const track = this.entry(s)?.track
    const pic = track ? this.feeds.get(track)?.picture() : null
    g.save()
    g.beginPath()
    g.rect(r.x, r.y, r.w, r.h)
    g.clip()
    g.fillStyle = TILE
    g.fillRect(r.x, r.y, r.w, r.h)
    if (pic && pic.w > 0 && pic.h > 0) {
      // cameras fill their tile; a screen is shown whole
      const scale = (s.k === 'screen' ? Math.min : Math.max)(r.w / pic.w, r.h / pic.h)
      const w = pic.w * scale
      const h = pic.h * scale
      g.drawImage(pic.img, r.x + (r.w - w) / 2, r.y + (r.h - h) / 2, w, h)
    } else {
      const rad = Math.min(r.w, r.h) * 0.2
      g.fillStyle = accent
      g.beginPath()
      g.arc(r.x + r.w / 2, r.y + r.h / 2, rad, 0, Math.PI * 2)
      g.fill()
      g.fillStyle = WHITE
      g.font = `600 ${Math.round(rad * 0.8)}px ${FONT}`
      g.textAlign = 'center'
      g.textBaseline = 'middle'
      g.fillText(initials(name), r.x + r.w / 2, r.y + r.h / 2)
    }
    g.restore()
    if (tag) drawNameTag(g, r, name, this.stage?.taglines[s.p] ?? '', accent, this.stage?.name_style ?? 'bar')
  }

  private drawBanner(text: string, accent: string) {
    const g = this.g
    const { width: W, height: H } = this.canvas
    const h = H * 0.09
    const x = W * 0.04
    const y = H - H * 0.1 - h // above the name tags
    const fs = Math.round(h * 0.42)
    g.font = `600 ${fs}px ${FONT}`
    const shown = fit(g, text, W - 2 * x - h * 0.9)
    const w = Math.min(W - 2 * x, g.measureText(shown).width + h * 0.9)
    g.fillStyle = WHITE
    g.fillRect(x, y, w, h)
    g.fillStyle = accent
    g.fillRect(x, y, h * 0.14, h)
    g.fillStyle = INK
    g.textAlign = 'left'
    g.textBaseline = 'middle'
    g.fillText(shown, x + h * 0.45, y + h / 2)
  }
}

// relative luminance of a #rrggbb colour (WCAG 2)
function luminance(hex: string): number {
  const c = [1, 3, 5].map((i) => {
    const v = parseInt(hex.slice(i, i + 2), 16) / 255
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!
}

/**
 * inkOn is the text colour for a plate of colour bg: white or black, whichever contrasts more. With pure black the
 * better of the two is always 4.5:1 or more (the worst plate, at the crossover, gives about 4.58:1); a near black
 * is not enough there.
 */
export function inkOn(bg: string): string {
  const l = luminance(/^#[0-9a-f]{6}$/i.test(bg) ? bg : DEFAULT_ACCENT)
  return 1.05 / (l + 0.05) >= (l + 0.05) / 0.05 ? WHITE : BLACK
}

/**
 * drawNameTag draws a name tag in a style at the bottom left of tile r (design N3): the program calls it for each
 * tile and the Brand tab's previews on small canvases. Sizes derive from the tile's height (capped by the canvas's),
 * text that does not fit ends with an ellipsis, and nothing is drawn outside the tile. Only 'two-line' draws the
 * tagline, under the name; without one it is the name alone, as 'bar'.
 */
export function drawNameTag(
  g: CanvasRenderingContext2D,
  r: Rect,
  name: string,
  tagline: string,
  accent: string,
  style: NameStyle,
) {
  if (!name) return
  const fs = Math.max(10, Math.round(Math.min(g.canvas.height * 0.026, r.h * 0.08)))
  const pad = fs * 0.5
  const font = `600 ${fs}px ${FONT}`
  const small = `400 ${Math.round(fs * 0.8)}px ${FONT}`
  g.save()
  g.beginPath()
  g.rect(r.x, r.y, r.w, r.h)
  g.clip()
  g.textAlign = 'left'
  g.textBaseline = 'middle'
  g.font = font
  const x = r.x + pad * 1.5
  if (style === 'strip') {
    const sh = fs + pad * 2.4
    g.fillStyle = STRIP
    g.fillRect(r.x, r.y + r.h - sh, r.w, sh)
    g.fillStyle = WHITE
    g.fillText(fit(g, name, r.w - pad * 4), r.x + pad * 2, r.y + r.h - sh / 2)
  } else if (style === 'text') {
    const text = fit(g, name, r.w - pad * 4)
    const line = Math.max(2, Math.round(fs * 0.14))
    const y = r.y + r.h - pad * 2 - line - pad * 0.5 - fs / 2
    g.fillStyle = accent
    g.fillRect(x, y + fs / 2 + pad * 0.5, g.measureText(text).width, line)
    g.shadowColor = SHADOW
    g.shadowBlur = fs * 0.35
    g.fillStyle = WHITE
    g.fillText(text, x, y)
    g.fillText(text, x, y) // twice: a denser shadow under thin letters
  } else {
    // bar, solid and two-line: a plate
    const solid = style === 'solid'
    const indent = solid ? pad * 1.3 : pad * 1.6
    const text = fit(g, name, r.w - pad * 6)
    let sub = ''
    let tw = g.measureText(text).width
    if (style === 'two-line' && tagline) {
      g.font = small
      sub = fit(g, tagline, r.w - pad * 6)
      tw = Math.max(tw, g.measureText(sub).width)
    }
    const th = fs + pad * 1.4 + (sub ? fs * 0.8 + pad * 0.4 : 0)
    const y = r.y + r.h - th - pad * 1.5
    g.fillStyle = solid ? accent : TAG
    g.fillRect(x, y, tw + indent + pad, th)
    if (!solid) {
      g.fillStyle = accent
      g.fillRect(x, y, pad * 0.5, th)
    }
    g.fillStyle = solid ? inkOn(accent) : WHITE
    g.font = font
    g.fillText(text, x + indent, y + pad * 0.7 + fs / 2)
    if (sub) {
      g.font = small
      g.fillStyle = SUB
      g.fillText(sub, x + indent, y + pad * 1.1 + fs + (fs * 0.8) / 2)
    }
  }
  g.restore()
}

/** drawNameTagPreview draws a name tag on an empty tile filling canvas c: the Brand tab's previews */
export function drawNameTagPreview(
  c: HTMLCanvasElement,
  name: string,
  tagline: string,
  accent: string,
  style: NameStyle,
) {
  const g = c.getContext('2d')
  if (!g) return
  const draw = () => {
    g.fillStyle = TILE
    g.fillRect(0, 0, c.width, c.height)
    drawNameTag(g, { x: 0, y: 0, w: c.width, h: c.height }, name, tagline, accent, style)
  }
  draw()
  // a canvas draws with a web font only once it is loaded
  if (document.fonts && !document.fonts.check(`600 10px ${FONT}`))
    void document.fonts.load(`600 10px ${FONT}`).then(draw)
}

// fit shortens text with an ellipsis until it fits in max pixels with the current font.
function fit(g: CanvasRenderingContext2D, text: string, max: number): string {
  if (g.measureText(text).width <= max) return text
  const chars = [...text]
  while (chars.length > 0 && g.measureText(chars.join('') + '…').width > max) chars.pop()
  return chars.join('') + '…'
}
