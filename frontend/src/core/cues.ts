// Sound cues (design U7 of add-studio-interactions): four short tones synthesised with the Web Audio API (an
// oscillator and a gain envelope; no audio file), played on the listener's own device. The player has an
// AudioContext of its own, connected to the device's output only: it shares no node with the program mix
// (core/audioMix.ts), so a cue is never wired into the program. Speakers or a shared screen can still carry one into
// a microphone, which is why cues are silent while the program is on air unless the listener asks for them.

export type Cue = 'joined' | 'left' | 'onAir' | 'offAir'

/** the listener's choices, kept in this browser (they belong to the device, not to the account) */
export interface CuePrefs {
  /** cues at all (on by default) */
  sounds: boolean
  /** cues while the program is on air, stage.on_air (off by default) */
  whileOnAir: boolean
}

export const DEFAULT_CUE_PREFS: Readonly<CuePrefs> = { sounds: true, whileOnAir: false }
export const CUE_PREFS_KEY = 'streamgo.cues'
/** no cue lasts longer, in seconds, and none is louder */
export const CUE_SECONDS = 0.25
export const CUE_PEAK = 0.15
/** a joined or left cue plays only when this many milliseconds have passed since the last one played */
export const PRESENCE_GAP_MS = 1000

// each cue: two notes (Hz) glided from the first to the second
const TONES: Record<Cue, [number, number]> = {
  joined: [660, 880],
  left: [880, 587],
  onAir: [784, 1175],
  offAir: [698, 466],
}

/** loadCuePrefs reads the choices; storage that is blocked, empty or holds something else gives the defaults */
export function loadCuePrefs(storage: Pick<Storage, 'getItem'> | null): CuePrefs {
  try {
    const v: unknown = JSON.parse(storage?.getItem(CUE_PREFS_KEY) ?? 'null')
    if (v && typeof v === 'object') {
      const { sounds, whileOnAir } = v as Record<string, unknown>
      if (typeof sounds === 'boolean' && typeof whileOnAir === 'boolean') return { sounds, whileOnAir }
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

// a cue is wanted with sounds on, and while the program is on air only with whileOnAir on
const wanted = (prefs: CuePrefs, onAir: boolean) => prefs.sounds && (!onAir || prefs.whileOnAir)

export class CuePlayer {
  private ctx: AudioContext | null = null
  private lastPresence = -Infinity
  // the tones still sounding, so a change that silences cues cuts them instead of letting them finish
  private readonly playing = new Set<{ osc: OscillatorNode; gain: GainNode }>()
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
   * play plays a cue when the listener wants it: sounds on, and while the program is on air only with whileOnAir
   * on. A context that is not running drops it (nothing is queued). A joined or left cue plays only when a second
   * has passed since the last joined or left cue played. It reports whether the cue was played.
   */
  play(cue: Cue, prefs: CuePrefs, onAir: boolean): boolean {
    if (!wanted(prefs, onAir)) return false
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
    const tone = { osc, gain }
    this.playing.add(tone)
    osc.onended = () => {
      gain.disconnect()
      this.playing.delete(tone)
    }
    osc.start(t)
    osc.stop(end)
    return true
  }

  /** hush stops at once the tones still sounding when the listener no longer wants cues: sounds off, or on air */
  hush(prefs: CuePrefs, onAir: boolean): void {
    if (wanted(prefs, onAir)) return
    for (const { osc, gain } of this.playing) {
      gain.disconnect()
      try {
        osc.stop()
      } catch {
        /* already stopped */
      }
    }
    this.playing.clear()
  }

  /** close closes the context; the player can be started again */
  close(): void {
    this.playing.clear()
    void this.ctx?.close().catch(() => {})
    this.ctx = null
    this.lastPresence = -Infinity
  }
}
