// The stage state of design D1: parsing ignores unknown fields; an older version is dropped.
import { describe, expect, it } from 'vitest'
import { parseStage, StageFeed } from '@/core/stageState'

const state = (v: number, extra: object = {}) => ({
  v,
  on_air: true,
  layout: 'spotlight',
  sources: [
    { p: 'host-01J', k: 'camera' },
    { p: 'guest-01J', k: 'screen' },
  ],
  banner: { id: '01JB', text: 'Q3 roadmap' },
  logo: '/assets/9f3c.png',
  accent: '#3d41cf',
  hide_names: false,
  name_style: 'bar',
  hide_screen_names: false,
  taglines: {},
  ...extra,
})

describe('parseStage', () => {
  it('reads the room metadata string and the API object alike', () => {
    expect(parseStage(JSON.stringify(state(12)))).toEqual(state(12))
    expect(parseStage(state(12))).toEqual(state(12))
  })

  it('ignores unknown fields, at the top and inside', () => {
    const raw = state(3, {
      future: { x: 1 },
      sources: [{ p: 'host-1', k: 'camera', volume: 3 }],
      banner: { id: 'b', text: 't', style: 'x' },
    })
    expect(parseStage(raw)).toEqual(
      state(3, { sources: [{ p: 'host-1', k: 'camera' }], banner: { id: 'b', text: 't' } }),
    )
  })

  // name-tags "Bar by default": a state from before the name styles, or with values it does not know
  it('reads the name tag fields, with their defaults when missing or unknown', () => {
    const old = { ...state(2) } as Record<string, unknown>
    delete old.name_style
    delete old.hide_screen_names
    delete old.taglines
    expect(parseStage(old)).toMatchObject({ name_style: 'bar', hide_screen_names: false, taglines: {} })
    expect(parseStage(state(2, { name_style: 'neon', taglines: [], hide_screen_names: 'yes' }))).toMatchObject({
      name_style: 'bar',
      hide_screen_names: false,
      taglines: {},
    })
    expect(
      parseStage(state(2, { name_style: 'two-line', hide_screen_names: true, taglines: { a: 'CEO', b: 3 } })),
    ).toMatchObject({ name_style: 'two-line', hide_screen_names: true, taglines: { a: 'CEO' } })
  })

  it('accepts no banner and no logo', () => {
    expect(parseStage(state(1, { banner: null, logo: null, sources: [] }))).toMatchObject({ banner: null, logo: null })
  })

  it.each([
    ['not JSON', '{'],
    ['an array', '[]'],
    ['no version', { ...state(1), v: undefined }],
    ['a negative version', state(-1)],
    ['a fractional version', state(1.5)],
    ['an unknown layout', state(1, { layout: 'mosaic' })],
    ['an unknown source kind', state(1, { sources: [{ p: 'x', k: 'mic' }] })],
    ['a source without identity', state(1, { sources: [{ k: 'camera' }] })],
    ['a malformed banner', state(1, { banner: { id: 1 } })],
    ['a missing on_air', { ...state(1), on_air: undefined }],
  ])('refuses %s', (_, raw) => {
    expect(parseStage(raw)).toBeNull()
  })
})

describe('StageFeed', () => {
  it('drops a state with a lower version than the last seen, keeps equal and newer ones', () => {
    const feed = new StageFeed()
    expect(feed.accept(state(5))).toBe(true)
    expect(feed.accept(state(4, { layout: 'grid' }))).toBe(false)
    expect(feed.current?.layout).toBe('spotlight')
    expect(feed.accept(state(5, { layout: 'solo' }))).toBe(true)
    expect(feed.accept(JSON.stringify(state(6, { layout: 'pip' })))).toBe(true)
    expect(feed.current).toMatchObject({ v: 6, layout: 'pip' })
  })

  it('keeps the current state when the input is invalid', () => {
    const feed = new StageFeed()
    feed.accept(state(2))
    expect(feed.accept('garbage')).toBe(false)
    expect(feed.current?.v).toBe(2)
  })
})
