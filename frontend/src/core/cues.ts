// Sound cues (design U7 of add-studio-interactions): four short tones synthesised with the Web Audio API (an
// oscillator and a gain envelope; no audio file), played on the listener's own device. The player has an
// AudioContext of its own, connected to the device's output only: it shares no node with the program mix
// (core/audioMix.ts), so a cue is never wired into the program. Speakers or a shared screen can still carry one into
// a microphone, which is why cues are silent during a broadcast unless the listener asks for them.

export type Cue = 'joined' | 'left' | 'live' | 'ended'

/** the listener's choices, kept in this browser (they belong to the device, not to the account) */
export interface CuePrefs {
  /** cues at all (on by default) */
  sounds: boolean
  /** cues while a broadcast is starting, live or interrupted (off by default) */
  duringBroadcast: boolean
}

export const DEFAULT_CUE_PREFS: Readonly<CuePrefs> = { sounds: true, duringBroadcast: false }
export const CUE_PREFS_KEY = 'streamgo.cues'
/** no cue lasts longer, in seconds, and none is louder */
export const CUE_SECONDS = 0.25
export const CUE_PEAK = 0.15
/** at most one joined or left cue in this many milliseconds: the first plays, the others are dropped */
export const PRESENCE_GAP_MS = 1000

// each cue: two notes (Hz) glided from the first to the second
const TONES: Record<Cue, [number, number]> = {
  joined: [660, 880],
  left: [880, 587],
  live: [784, 1175],
  ended: [698, 466],
}

/** loadCuePrefs reads the choices; storage that is blocked, empty or holds something else gives the defaults */
export function loadCuePrefs(storage: Pick<Storage, 'getItem'> | null): CuePrefs {
  try {
    const v: unknown = JSON.parse(storage?.getItem(CUE_PREFS_KEY) ?? 'null')
    if (v && typeof v === 'object') {
      const { sounds, duringBroadcast } = v as Record<string, unknown>
      if (typeof sounds === 'boolean' && typeof duringBroadcast === 'boolean') return { sounds, duringBroadcast }
    }
  } catch {
    /* blocked or not JSON */
  }
  return { ...DEFAULT_CUE_PREFS }
}

/** saveCuePrefs keeps the choices; storage that cannot be written keeps them for this page only */
export function saveCuePrefs(storage: Pick<Storage, 'setItem'> | null, prefs: CuePrefs): void {
  try {
    storage?.setItem(CUE_PREFS_KEY, JSON.stringify(prefs))
  } catch {
    /* blocked */
  }
}

export interface CuePlayerOptions {
  /** makes the player's context (the browser's AudioContext by default) */
  createContext?: () => AudioContext
  /** the current time in milliseconds (Date.now by default) */
  now?: () => number
}

export class CuePlayer {
  private ctx: AudioContext | null = null
  private lastPresence = -Infinity
  private readonly createContext: () => AudioContext
  private readonly now: () => number

  constructor(opts: CuePlayerOptions = {}) {
    this.createContext = opts.createContext ?? (() => new AudioContext())
    this.now = opts.now ?? Date.now
  }

  /** the player's context, once started (for tests and checks) */
  get context(): AudioContext | null {
    return this.ctx
  }

  /** start creates and resumes the context; call it from a user gesture (a click, a key) */
  start(): void {
    try {
      this.ctx ??= this.createContext()
      if (this.ctx.state === 'suspended') void this.ctx.resume().catch(() => {})
    } catch {
      /* no Web Audio: cues stay silent */
    }
  }

  /**
   * play plays a cue when the listener wants it: sounds on, and during a broadcast only with duringBroadcast on. A
   * context that is not running drops it (nothing is queued). Joined and left cues share a window: the first in
   * a second plays, the others in that second are dropped. It reports whether the cue was played.
   */
  play(cue: Cue, prefs: CuePrefs, onAir: boolean): boolean {
    if (!prefs.sounds || (onAir && !prefs.duringBroadcast)) return false
    const ctx = this.ctx
    if (!ctx || ctx.state !== 'running') return false
    const presence = cue === 'joined' || cue === 'left'
    if (presence) {
      const now = this.now()
      if (now - this.lastPresence < PRESENCE_GAP_MS) return false
      this.lastPresence = now
    }
    const [from, to] = TONES[cue]
    const t = ctx.currentTime
    const end = t + CUE_SECONDS
    const osc = ctx.createOscillator()
    osc.type = 'sine'
    osc.frequency.setValueAtTime(from, t)
    osc.frequency.linearRampToValueAtTime(to, t + CUE_SECONDS / 2)
    const gain = ctx.createGain()
    gain.gain.setValueAtTime(0, t)
    gain.gain.linearRampToValueAtTime(CUE_PEAK, t + 0.01)
    gain.gain.linearRampToValueAtTime(0, end - 0.01)
    osc.connect(gain).connect(ctx.destination)
    osc.onended = () => gain.disconnect()
    osc.start(t)
    osc.stop(end)
    return true
  }

  /** close closes the context; the player can be started again */
  close(): void {
    void this.ctx?.close().catch(() => {})
    this.ctx = null
    this.lastPresence = -Infinity
  }
}
