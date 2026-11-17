// The layout function of design D1: where each present source is drawn. Pure; the compositor and the tests use it.
import type { Layout, StageSource } from './stageState'

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export interface Placed<S> {
  source: S
  rect: Rect
}

// rect turns fractional edges into whole pixels, so neighbours share an edge and never overlap.
const rect = (x0: number, y0: number, x1: number, y1: number): Rect => {
  const x = Math.round(x0)
  const y = Math.round(y0)
  return { x, y, w: Math.round(x1) - x, h: Math.round(y1) - y }
}

// presentSources keeps the stage sources whose participant is in the room: an absent participant's source stays
// in the stage state and is drawn again when they return.
export const presentSources = <S extends Pick<StageSource, 'p'>>(sources: readonly S[], present: ReadonlySet<string>) =>
  sources.filter((s) => present.has(s.p))

// layoutRects places the sources whose participant is present (in stage order) in a width x height frame, in
// drawing order. A lone source of spotlight, sidebar or screen fills the frame.
export function layoutRects<S extends Pick<StageSource, 'k'>>(
  layout: Layout,
  present: readonly S[],
  width: number,
  height: number,
): Placed<S>[] {
  const n = present.length
  if (n === 0) return []
  const W = width
  const H = height
  const full = rect(0, 0, W, H)
  if (layout === 'solo' || n === 1) return [{ source: present[0]!, rect: full }]

  if (layout === 'grid') {
    const cols = n <= 4 ? 2 : 3 // 2x1, 2x2, 3x2
    const rows = Math.ceil(n / cols)
    return present.map((source, i) => {
      const c = i % cols
      const r = Math.floor(i / cols)
      return { source, rect: rect((c * W) / cols, (r * H) / rows, ((c + 1) * W) / cols, ((r + 1) * H) / rows) }
    })
  }

  if (layout === 'pip') {
    const w = W * 0.22
    const h = Math.min((w * 9) / 16, H * 0.4)
    const m = W * 0.02
    return [
      { source: present[0]!, rect: full },
      { source: present[1]!, rect: rect(W - m - w, H - m - h, W - m, H - m) },
    ]
  }

  if (layout === 'spotlight') {
    const split = W * 0.7
    const rest = present.slice(1)
    return [
      { source: present[0]!, rect: rect(0, 0, split, H) },
      ...rest.map((source, i) => ({
        source,
        rect: rect(split, (i * H) / rest.length, W, ((i + 1) * H) / rest.length),
      })),
    ]
  }

  // sidebar and screen: the featured source at 82 %, the others as 16:9 thumbnails stacked, centred, in the strip
  let featured = 0
  if (layout === 'screen')
    featured = Math.max(
      0,
      present.findIndex((s) => s.k === 'screen'),
    )
  const rest = present.filter((_, i) => i !== featured)
  const split = W * 0.82
  const th = Math.min(((W - split) * 9) / 16, H / rest.length)
  const tw = (th * 16) / 9
  const x0 = split + (W - split - tw) / 2
  const y0 = (H - th * rest.length) / 2
  return [
    { source: present[featured]!, rect: rect(0, 0, split, H) },
    ...rest.map((source, i) => ({ source, rect: rect(x0, y0 + i * th, x0 + tw, y0 + (i + 1) * th) })),
  ]
}
