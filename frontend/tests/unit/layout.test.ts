// studio-ui "Layout rectangles" and "Absent participant", as properties over every layout, every source count from
// 0 to 6 and both program sizes.
import { describe, expect, it } from 'vitest'
import { layoutRects, presentSources, type Rect } from '@/core/layout'
import { LAYOUTS, type StageSource } from '@/core/stageState'

const SIZES = [
  [1280, 720],
  [1920, 1080],
] as const

const overlap = (a: Rect, b: Rect) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

// stage sources p0..p(n-1); `screenAt` makes that one a screen share
const stage = (n: number, screenAt = -1): StageSource[] =>
  Array.from({ length: n }, (_, i) => ({ p: `p${i}`, k: i === screenAt ? 'screen' : 'camera' }))

describe('layout rectangles', () => {
  for (const layout of LAYOUTS)
    for (const [W, H] of SIZES)
      for (let n = 0; n <= 6; n++)
        for (const screenAt of [-1, ...Array.from({ length: n }, (_, i) => i)]) {
          const name = `${layout}, ${n} sources${screenAt >= 0 ? `, screen at ${screenAt}` : ''}, ${W}x${H}`
          it(name, () => {
            const src = stage(n, screenAt)
            const out = layoutRects(layout, src, W, H)

            // what is drawn
            const drawn = out.map((o) => o.source.p)
            const ids = src.map((s) => s.p)
            const expected =
              n === 0
                ? []
                : layout === 'solo'
                  ? ids.slice(0, 1)
                  : layout === 'pip'
                    ? ids.slice(0, 2)
                    : layout === 'screen' && n > 1 && screenAt >= 0
                      ? [ids[screenAt]!, ...ids.filter((_, i) => i !== screenAt)]
                      : ids
            expect(drawn).toEqual(expected)

            // inside the frame, not empty
            for (const { rect: r } of out) {
              expect(r.w).toBeGreaterThan(0)
              expect(r.h).toBeGreaterThan(0)
              expect(r.x).toBeGreaterThanOrEqual(0)
              expect(r.y).toBeGreaterThanOrEqual(0)
              expect(r.x + r.w).toBeLessThanOrEqual(W)
              expect(r.y + r.h).toBeLessThanOrEqual(H)
            }

            // no overlap, except the inset of pip
            if (layout !== 'pip')
              for (let i = 0; i < out.length; i++)
                for (let j = i + 1; j < out.length; j++) expect(overlap(out[i]!.rect, out[j]!.rect)).toBe(false)

            // the same result for the same input
            expect(layoutRects(layout, stage(n, screenAt), W, H)).toEqual(out)

            // the shapes of the table
            const [first, ...others] = out.map((o) => o.rect)
            if (n === 0) return
            if (layout === 'solo' || n === 1) return expect(first).toEqual({ x: 0, y: 0, w: W, h: H })
            if (layout === 'grid') {
              const [cols, rows] = n === 2 ? [2, 1] : n <= 4 ? [2, 2] : [3, 2]
              for (const r of out.map((o) => o.rect)) {
                expect(Math.abs(r.w - W / cols)).toBeLessThanOrEqual(1)
                expect(Math.abs(r.h - H / rows)).toBeLessThanOrEqual(1)
              }
            }
            if (layout === 'spotlight') {
              expect(first).toEqual({ x: 0, y: 0, w: Math.round(W * 0.7), h: H })
              for (const r of others) expect(r.x).toBe(Math.round(W * 0.7))
              expect(others.reduce((a, r) => a + r.h, 0)).toBe(H)
            }
            if (layout === 'sidebar' || layout === 'screen') {
              expect(first).toEqual({ x: 0, y: 0, w: Math.round(W * 0.82), h: H })
              for (const r of others) {
                expect(r.x).toBeGreaterThanOrEqual(Math.round(W * 0.82))
                expect(Math.abs(r.w / r.h - 16 / 9)).toBeLessThan(0.02)
              }
            }
            if (layout === 'pip') {
              expect(first).toEqual({ x: 0, y: 0, w: W, h: H })
              const inset = others[0]!
              expect(Math.abs(inset.w - W * 0.22)).toBeLessThanOrEqual(1)
              expect(inset.x + inset.w).toBeGreaterThan(W * 0.9)
              expect(inset.y + inset.h).toBeGreaterThan(H * 0.9)
            }
          })
        }
})

describe('absent participant', () => {
  for (const layout of LAYOUTS)
    for (const [W, H] of SIZES)
      for (const gone of [0, 1, 2])
        it(`${layout} at ${W}x${H}, source ${gone} of 3 absent`, () => {
          const sources = stage(3, 2)
          const all = new Set(sources.map((s) => s.p))
          const left = new Set([...all].filter((p) => p !== `p${gone}`))

          const drawn = layoutRects(layout, presentSources(sources, left), W, H)
          const two = sources.filter((s) => s.p !== `p${gone}`)
          expect(drawn).toEqual(layoutRects(layout, two, W, H))
          expect(drawn.some((o) => o.source.p === `p${gone}`)).toBe(false)

          // the guest rejoins: the stage state never lost the source
          expect(layoutRects(layout, presentSources(sources, all), W, H)).toEqual(layoutRects(layout, sources, W, H))
        })
})
