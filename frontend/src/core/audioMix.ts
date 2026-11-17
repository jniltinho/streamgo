// The program audio of design D2, in one AudioContext:
// - each on-stage audio track (a camera's microphone, a screen's audio) enters once, through its own gain node;
// - program bus (the owner only): every on-stage track, the owner's own included, through a limiter whose fixed
//   make-up gain is taken back, into one MediaStreamDestination that lives for the whole session (silent when the
//   stage is empty);
// - monitor bus: every on-stage track except the listener's own, to the speakers.
// Chrome feeds a remote WebRTC track to WebAudio only while the track also plays in a media element (silence
// otherwise, spike 1.1), so each remote track is attached to a muted, playing <audio> element: heard only once,
// through the monitor bus. Backstage tracks are simply not given to the mix.

export interface StageAudio {
  track: MediaStreamTrack
  /** the listener's own track (their microphone or shared screen): in the program, not in their monitor */
  local: boolean
}

/** a participant's audio as the studio sees it: their microphone and their shared screen's audio, when published */
export interface AudioPeer {
  identity: string
  /** the listener themselves */
  local: boolean
  mic: MediaStreamTrack | null
  screenAudio: MediaStreamTrack | null
}

// stageAudio picks the tracks the mix hears from the stage sources: a camera brings its participant's microphone (also
// with the camera off), a screen its audio; an absent participant's or a backstage participant's track is left out,
// and a track is listed once even when its participant has two sources on stage.
export function stageAudio(sources: readonly { p: string; k: 'camera' | 'screen' }[], peers: readonly AudioPeer[]) {
  const byId = new Map(peers.map((p) => [p.identity, p]))
  const out: StageAudio[] = []
  for (const s of sources) {
    const p = byId.get(s.p)
    const track = s.k === 'camera' ? p?.mic : p?.screenAudio
    if (p && track && !out.some((x) => x.track === track)) out.push({ track, local: p.local })
  }
  return out
}

// The limiter: a DynamicsCompressorNode, which adds a fixed make-up gain that Chromium computes as 0.6 x the gain
// reduction at full scale (+1.71 dB with the spike's threshold of -3 dB, as measured there). The spike's -3 dB and
// 100 ms release let six full-level voices peak at 1.14 to 1.19 (tests/e2e/04-core-audio.spec.ts); -6 dB and 250 ms
// keep them below 0.8.
const LIMITER = { threshold: -6, knee: 0, ratio: 20, attack: 0.003, release: 0.25 }
const MAKEUP_DB = 0.6 * -(LIMITER.threshold - LIMITER.threshold / LIMITER.ratio)

interface Input {
  source: MediaStreamAudioSourceNode
  gain: GainNode
  element: HTMLAudioElement | null
}

export class AudioMix {
  readonly context = new AudioContext()
  /** the program's audio ('program' mixes only): one track for the whole session */
  readonly programStream: MediaStream | null = null
  /** the monitor bus, before the speakers */
  readonly monitor: GainNode
  private limiter: DynamicsCompressorNode | null = null
  private inputs = new Map<MediaStreamTrack, Input>()

  /** program: true for the owner (program and monitor buses), false for a guest (monitor only) */
  constructor(program: boolean) {
    const ctx = this.context
    this.monitor = new GainNode(ctx)
    this.monitor.connect(ctx.destination)
    if (program) {
      this.limiter = new DynamicsCompressorNode(ctx, LIMITER)
      const takeBack = new GainNode(ctx, { gain: 10 ** (-MAKEUP_DB / 20) })
      const out = ctx.createMediaStreamDestination()
      this.limiter.connect(takeBack).connect(out)
      this.programStream = out.stream
    }
  }

  /** whether the context runs; a new page starts suspended until a user gesture, and nothing may be published then */
  get running(): boolean {
    return this.context.state === 'running'
  }

  /** resume must be called from a user gesture (a click) */
  resume(): Promise<void> {
    return this.context.resume()
  }

  /** onStateChange is called when the context starts or stops running */
  set onStateChange(fn: (() => void) | null) {
    this.context.onstatechange = fn
  }

  /** setStage replaces the set of on-stage audio tracks; a track no longer listed (left, replaced, reconnected) is removed */
  setStage(tracks: StageAudio[]) {
    const wanted = new Map<MediaStreamTrack, boolean>()
    for (const t of tracks) if (!wanted.has(t.track)) wanted.set(t.track, t.local)
    for (const [track, input] of this.inputs) if (!wanted.has(track)) this.remove(track, input)
    for (const [track, local] of wanted) if (!this.inputs.has(track)) this.add(track, local)
  }

  close() {
    for (const [track, input] of this.inputs) this.remove(track, input)
    void this.context.close()
  }

  private add(track: MediaStreamTrack, local: boolean) {
    const ctx = this.context
    const stream = new MediaStream([track])
    let element: HTMLAudioElement | null = null
    if (!local) {
      element = new Audio()
      element.muted = true
      element.srcObject = stream
      void element.play().catch(() => {})
    }
    const source = ctx.createMediaStreamSource(stream)
    const gain = new GainNode(ctx)
    source.connect(gain)
    if (this.limiter) gain.connect(this.limiter)
    if (!local) gain.connect(this.monitor)
    this.inputs.set(track, { source, gain, element })
  }

  private remove(track: MediaStreamTrack, input: Input) {
    input.source.disconnect()
    input.gain.disconnect()
    if (input.element) {
      input.element.pause()
      input.element.srcObject = null
    }
    this.inputs.delete(track)
  }
}
