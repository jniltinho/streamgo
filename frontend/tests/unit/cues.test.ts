// The cue player of add-studio-interactions (design U7, requirement "Sound cues").
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  CUE_PEAK,
  CUE_PREFS_KEY,
  CUE_SECONDS,
  CuePlayer,
  DEFAULT_CUE_PREFS,
  loadCuePrefs,
  saveCuePrefs,
} from '@/core/cues'
import { asContext, FakeAudioContext } from './fakeAudio'

const on = { sounds: true, whileOnAir: false }

function player() {
  const ctx = new FakeAudioContext()
  const p = new CuePlayer({ createContext: () => asContext(ctx) })
  return { ctx, p }
}

describe('cue player', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    vi.useRealTimers()
    FakeAudioContext.initial = 'running'
  })

  it('Short and quiet: a cue lasts at most 250 ms and its gain never passes 0.15', () => {
    const { ctx, p } = player()
    p.start()
    for (const c of ['onAir', 'offAir', 'joined'] as const) expect(p.play(c, on, false)).toBe(true)
    expect(ctx.oscillators).toHaveLength(3)
    for (const o of ctx.oscillators) expect(o.stopped - o.started).toBeLessThanOrEqual(0.25)
    for (const g of ctx.gains) {
      expect(Math.max(...g.gain.calls.map((c) => c.value))).toBeLessThanOrEqual(0.15)
      expect(g.gain.calls.at(-1)!.value).toBe(0)
      expect(Math.max(...g.gain.calls.map((c) => c.at))).toBeLessThanOrEqual(CUE_SECONDS)
    }
    expect(CUE_PEAK).toBeLessThanOrEqual(0.15)
  })

  it('Not wired to the program: its own context, connected to that context’s output only', () => {
    const mix = new FakeAudioContext() // stands for the program mix's context
    const { ctx, p } = player()
    p.start()
    p.play('joined', on, false)
    expect(p.context).toBe(asContext(ctx))
    expect(p.context).not.toBe(asContext(mix))
    const [osc] = ctx.oscillators
    const [gain] = ctx.gains
    expect(osc!.outputs).toEqual([gain])
    expect(gain!.outputs).toEqual([ctx.destination])
    expect(mix.oscillators.length + mix.gains.length).toBe(0)
  })

  it('Many at once: five joins within a second and a sixth 1.2 s after the first: the first and the sixth play', () => {
    const { ctx, p } = player()
    p.start()
    const played: boolean[] = []
    for (let i = 0; i < 5; i++) {
      played.push(p.play(i % 2 ? 'left' : 'joined', on, false))
      vi.advanceTimersByTime(200) // 0, 200, 400, 600, 800 ms
    }
    vi.advanceTimersByTime(200) // 1200 ms after the first
    played.push(p.play('joined', on, false))
    expect(played).toEqual([true, false, false, false, false, true])
    expect(ctx.oscillators).toHaveLength(2)
  })

  it('the cooldown is rolling from the last cue played, and on air / off air are not held back by it', () => {
    const { p } = player()
    p.start()
    expect(p.play('joined', on, false)).toBe(true)
    vi.advanceTimersByTime(999)
    expect(p.play('joined', on, false)).toBe(false)
    expect(p.play('offAir', on, false)).toBe(true)
    vi.advanceTimersByTime(1)
    expect(p.play('left', on, false)).toBe(true)
  })

  it('Silent while on air: nothing plays while on air unless asked for, and nothing is played later', () => {
    const { ctx, p } = player()
    p.start()
    expect(p.play('onAir', on, true)).toBe(false)
    expect(p.play('joined', on, true)).toBe(false)
    vi.advanceTimersByTime(5000)
    expect(ctx.oscillators).toHaveLength(0)
    expect(p.play('offAir', on, false)).toBe(true)
    expect(p.play('joined', on, false)).toBe(true) // a suppressed cue did not start the cooldown
    expect(p.play('onAir', { sounds: true, whileOnAir: true }, true)).toBe(true)
  })

  it('Turned off: no cue at all', () => {
    const { ctx, p } = player()
    p.start()
    for (const c of ['joined', 'left', 'onAir', 'offAir'] as const)
      expect(p.play(c, { sounds: false, whileOnAir: true }, false)).toBe(false)
    expect(ctx.oscillators).toHaveLength(0)
  })

  it('Audio not started: nothing plays, nothing is queued and nothing is logged', async () => {
    const errors = vi.spyOn(console, 'error')
    const warns = vi.spyOn(console, 'warn')
    const none = new CuePlayer({ createContext: () => asContext(new FakeAudioContext()) })
    expect(none.play('joined', on, false)).toBe(false) // never started: no context at all
    FakeAudioContext.initial = 'suspended'
    const ctx = new FakeAudioContext()
    ctx.resume = () => Promise.resolve() // the browser refuses: no click yet
    const p = new CuePlayer({ createContext: () => asContext(ctx) })
    p.start()
    expect(p.play('joined', on, false)).toBe(false)
    ctx.state = 'running'
    vi.advanceTimersByTime(5000)
    expect(ctx.oscillators).toHaveLength(0) // nothing queued for later
    expect(p.play('joined', on, false)).toBe(true)
    expect(errors).not.toHaveBeenCalled()
    expect(warns).not.toHaveBeenCalled()
  })

  it('start without Web Audio throws nothing and play stays silent', () => {
    const p = new CuePlayer({
      createContext: () => {
        throw new Error('no Web Audio')
      },
    })
    expect(() => p.start()).not.toThrow()
    expect(p.play('joined', on, false)).toBe(false)
  })

  it('close closes the context', () => {
    const { ctx, p } = player()
    p.start()
    p.close()
    expect(ctx.state).toBe('closed')
    expect(p.context).toBeNull()
  })
})

describe('cue preferences', () => {
  it('Storage that cannot be used: blocked or holding something else gives sounds on, while on air off', () => {
    const blocked = {
      getItem: () => {
        throw new DOMException('blocked', 'SecurityError')
      },
    }
    expect(loadCuePrefs(blocked)).toEqual({ sounds: true, whileOnAir: false })
    expect(loadCuePrefs(null)).toEqual(DEFAULT_CUE_PREFS)
    for (const v of ['{', '42', '"x"', '{"sounds":"yes","whileOnAir":false}', '{"sounds":true}', 'null'])
      expect(loadCuePrefs({ getItem: () => v }), v).toEqual(DEFAULT_CUE_PREFS)
    expect(() =>
      saveCuePrefs(
        {
          setItem: () => {
            throw new DOMException('full', 'QuotaExceededError')
          },
        },
        on,
      ),
    ).not.toThrow()
  })

  it('a saved choice is read back (after a reload)', () => {
    const store = new Map<string, string>()
    const s = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => store.set(k, v) }
    saveCuePrefs(s, { sounds: false, whileOnAir: true })
    expect(store.has(CUE_PREFS_KEY)).toBe(true)
    expect(loadCuePrefs(s)).toEqual({ sounds: false, whileOnAir: true })
  })
})
