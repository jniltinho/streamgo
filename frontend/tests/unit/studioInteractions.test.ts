// The studio store's part of add-studio-interactions: the device switch (design U3), the microphone meter (U4) and
// the sound cues' wiring to the room and to stage.on_air (U7). LiveKit, the API and the program mix are replaced
// by fakes; the Web Audio API by tests/unit/fakeAudio.ts.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { FakeAudioContext } from './fakeAudio'

// ── a LiveKit stand-in: one room at a time, events emitted by the test ──────────────────────────────────────────
type Handler = (...a: unknown[]) => void
const lk = vi.hoisted(() => {
  class FakeTrack {
    readyState = 'live'
    constructor(
      readonly kind: string,
      readonly label = '',
    ) {}
    stop() {
      this.readyState = 'ended'
    }
  }
  return {
    FakeTrack,
    room: null as null | Record<string, unknown>,
    /** what switchActiveDevice answers for a device id: true, false, or 'throw' */
    switching: new Map<string, boolean | 'throw'>(),
    switched: [] as string[],
  }
})

vi.mock('livekit-client', () => {
  const RoomEvent = new Proxy({}, { get: (_, k) => String(k) })
  class Room {
    handlers = new Map<string, Handler[]>()
    state = 'connected'
    metadata = ''
    remoteParticipants = new Map()
    mic: { track: { mediaStreamTrack: InstanceType<typeof lk.FakeTrack> } | null; isMuted: boolean } = {
      track: null,
      isMuted: true,
    }
    localParticipant = {
      identity: 'me',
      name: 'Me',
      isSpeaking: false,
      getTrackPublication: (s: string) => (s === 'microphone' && this.mic.track ? this.mic : undefined),
      setMicrophoneEnabled: async (on: boolean) => {
        if (on && !this.mic.track) this.mic.track = { mediaStreamTrack: new lk.FakeTrack('audio') }
        this.mic.isMuted = !on
      },
      setCameraEnabled: async () => {},
    }
    constructor() {
      lk.room = this as unknown as Record<string, unknown>
    }
    on(ev: string, fn: Handler) {
      this.handlers.set(ev, [...(this.handlers.get(ev) ?? []), fn])
      return this
    }
    emit(ev: string, ...a: unknown[]) {
      for (const fn of this.handlers.get(ev) ?? []) fn(...a)
    }
    async connect() {
      this.emit('ParticipantConnected', { identity: 'already-here' }) // the first population
    }
    async disconnect() {
      this.state = 'disconnected'
      this.emit('Disconnected')
    }
    async switchActiveDevice(kind: string, id: string) {
      lk.switched.push(`${kind}:${id}`)
      const r = lk.switching.get(id) ?? true
      if (r === 'throw') throw new DOMException('refused', 'NotReadableError')
      // a working switch of the microphone replaces its capture, as LiveKit's restartTrack does
      if (r && kind === 'audioinput' && this.mic.track && !this.mic.isMuted)
        this.mic.track = { mediaStreamTrack: new lk.FakeTrack('audio', id) }
      return r
    }
    static getLocalDevices = async (kind: string) =>
      kind === 'audioinput'
        ? [
            { deviceId: 'm1', label: 'Mic One', kind },
            { deviceId: 'm2', label: 'Mic Two', kind },
          ]
        : [
            { deviceId: 'front', label: 'Front', kind },
            { deviceId: 'back', label: 'Back', kind },
          ]
  }
  return {
    Room,
    RoomEvent,
    ConnectionState: { Connected: 'connected', Reconnecting: 'reconnecting', Disconnected: 'disconnected' },
    DisconnectReason: {},
    Track: {
      Source: { Microphone: 'microphone', Camera: 'camera', ScreenShare: 'screen_share', ScreenShareAudio: 'ssa' },
      Kind: { Audio: 'audio', Video: 'video' },
    },
    VideoPreset: class {},
    createLocalTracks: async () => [],
  }
})
vi.mock('@/core/audioMix', () => ({
  AudioMix: class {
    running = true
    programStream = null
    resume = async () => {}
    close() {}
    setStage() {}
    set onStateChange(_: unknown) {}
  },
  stageAudio: () => [],
}))
vi.mock('@/api/client', () => ({
  ApiError: class extends Error {},
  api: async (method: string, path: string) =>
    path.startsWith('/join/') ? { url: 'wss://x', token: 't', identity: 'guest-1', title: 'Show' } : {},
}))

const { useStudio, HEARD_LEVEL } = await import('@/stores/studio')
const room = () => lk.room as unknown as { emit: (ev: string, ...a: unknown[]) => void; state: string; mic: any }

let stage = 0
const setStage = (onAir: boolean) => room().emit('RoomMetadataChanged', JSON.stringify(stageState(onAir)))
const stageState = (on_air: boolean) => ({
  v: ++stage,
  on_air,
  layout: 'grid',
  sources: [],
  banner: null,
  logo: null,
  accent: '#000000',
  hide_names: false,
})

/** the oscillators the cue player started: one per cue played */
const cuesPlayed = () => FakeAudioContext.all.reduce((n, c) => n + c.oscillators.length, 0)

async function join(mic = true) {
  const s = useStudio()
  await s.joinGuest('tok', 'Guest', { mic, cam: true })
  document.dispatchEvent(new Event('pointerdown')) // the audio started by a click
  await vi.runOnlyPendingTimersAsync()
  return s
}
const tick = () => vi.advanceTimersByTimeAsync(0)

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  FakeAudioContext.all = []
  lk.switching.clear()
  lk.switched = []
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal(
    'MediaStream',
    class {
      constructor(readonly tracks: unknown[]) {}
    },
  )
  localStorage.clear()
  Object.defineProperty(navigator, 'mediaDevices', { value: new EventTarget(), configurable: true })
})
afterEach(() => {
  useStudio().close()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('device switch (chooseDevice: the menus and the selects)', () => {
  it('Choose another microphone: the chosen device is in use and is what the menu and the select show', async () => {
    const s = await join()
    expect(await s.chooseDevice('audio', 'm2')).toBe('ok')
    expect(s.micId).toBe('m2')
    expect(lk.switched).toEqual(['audioinput:m2'])
  })

  it('A device that cannot be used: refused or reported as failed, the previous device is started again', async () => {
    const s = await join()
    await s.chooseDevice('video', 'front')
    for (const bad of [false, 'throw'] as const) {
      lk.switched = []
      lk.switching.set('back', bad)
      expect(await s.chooseDevice('video', 'back')).toBe('previous')
      expect(lk.switched).toEqual(['videoinput:back', 'videoinput:front'])
      expect(s.camId).toBe('front') // the menu's checked item and the select's value
      expect(s.cam).toBe(true)
    }
  })

  it('Neither device can be used: the control shows off and no device is claimed', async () => {
    const s = await join()
    await s.chooseDevice('audio', 'm1')
    lk.switching.set('m2', false)
    lk.switching.set('m1', 'throw')
    expect(await s.chooseDevice('audio', 'm2')).toBe('unavailable')
    expect(s.mic).toBe(false)
    expect(s.micId).toBe('m1') // the new one never became the choice
    expect(room().mic.isMuted).toBe(true)
  })

  it('Default is the browser default, not a device id that must exist exactly', async () => {
    const s = await join()
    await s.chooseDevice('audio', 'm2')
    expect(await s.chooseDevice('audio', '')).toBe('ok')
    expect(s.micId).toBe('')
  })

  it('Chosen while off: the device is kept for later and the camera stays off', async () => {
    const s = await join()
    await s.setCam(false)
    expect(await s.chooseDevice('video', 'back')).toBe('ok')
    expect(s.cam).toBe(false)
    expect(s.camId).toBe('back')
  })

  it('the device lists follow devicechange while the studio is open, and stop after it closes', async () => {
    const s = await join()
    s.devices = { audio: [], video: [] }
    navigator.mediaDevices.dispatchEvent(new Event('devicechange'))
    await vi.waitFor(() => expect(s.devices.audio).toHaveLength(2))
    s.close()
    s.devices = { audio: [], video: [] }
    navigator.mediaDevices.dispatchEvent(new Event('devicechange'))
    await tick()
    expect(s.devices.audio).toHaveLength(0)
  })
})

describe('microphone meter', () => {
  const analysers = () => FakeAudioContext.all.flatMap((c) => c.analysers)

  it('Heard: a steady tone at half of full scale shows more than a quarter of the range', async () => {
    const s = await join()
    analysers().at(-1)!.samples = 0.5
    await vi.advanceTimersByTimeAsync(1000)
    expect(s.micLevel).toBeGreaterThan(0.25)
    expect(s.micLevel >= HEARD_LEVEL).toBe(true)
  })

  it('Silent: below 0.05 says silent, from 0.05 up heard (the level is the RMS, without easing)', async () => {
    const s = await join()
    const a = analysers().at(-1)!
    a.samples = 0.049
    await vi.advanceTimersByTimeAsync(100)
    expect(s.micLevel).toBeCloseTo(0.049)
    expect(s.micLevel < HEARD_LEVEL).toBe(true)
    a.samples = 0.05
    await vi.advanceTimersByTimeAsync(100)
    expect(s.micLevel >= HEARD_LEVEL).toBe(true)
    a.samples = 0
    await vi.advanceTimersByTimeAsync(100)
    expect(s.micLevel).toBe(0) // no decay from the earlier reading
  })

  it('the analyser is connected to no destination', async () => {
    await join()
    const a = analysers().at(-1)!
    expect(a.outputs).toEqual([])
    expect(a.context.sources[0]!.outputs).toEqual([a])
  })

  it('Off: the meter is empty and its analyser stops sampling', async () => {
    const s = await join()
    const a = analysers().at(-1)!
    a.samples = 0.5
    await vi.advanceTimersByTimeAsync(200)
    await s.setMic(false)
    const reads = a.reads
    await vi.advanceTimersByTimeAsync(1000)
    expect(s.micLevel).toBe(0)
    expect(a.reads).toBe(reads)
    expect(a.context.state).toBe('closed')
  })

  it('Stopping the meter stops nothing else: hidden then shown, the analyser stops and starts, the capture stays', async () => {
    const s = await join()
    const capture = room().mic.track.mediaStreamTrack
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(analysers().at(-1)!.context.state).toBe('closed')
    expect(s.micLevel).toBe(0)
    expect(capture.readyState).toBe('live')
    expect(room().mic.isMuted).toBe(false) // still published
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(analysers().at(-1)!.context.state).not.toBe('closed')
    expect(analysers()).toHaveLength(2)
    hidden.mockRestore()
  })

  it('Leaving: the room disconnects or the person leaves, and nothing of the analyser remains', async () => {
    const s = await join()
    await s.leave()
    expect(FakeAudioContext.all.filter((c) => c.analysers.length).every((c) => c.state === 'closed')).toBe(true)
    await vi.advanceTimersByTimeAsync(1000)
    expect(s.micLevel).toBe(0)
  })

  it('Leaving: another tab takes the studio over (the room disconnects by itself)', async () => {
    await join()
    room().emit('Disconnected', 'DUPLICATE_IDENTITY')
    expect(FakeAudioContext.all.filter((c) => c.analysers.length).every((c) => c.state === 'closed')).toBe(true)
  })

  it('After a switch: the meter follows the microphone now in use, and again after a reconnection', async () => {
    const s = await join()
    const first = analysers().length
    await s.chooseDevice('audio', 'm2')
    expect(analysers()).toHaveLength(first + 1)
    room().mic.track = { mediaStreamTrack: new lk.FakeTrack('audio', 'again') }
    room().emit('Reconnected')
    expect(analysers()).toHaveLength(first + 2)
  })
})

describe('sound cues in the room', () => {
  const remote = (id: string) => ({ identity: id })

  it('Someone joins: the join cue plays once; the first population played nothing', async () => {
    await join()
    expect(cuesPlayed()).toBe(0) // "already-here" was in the room on arrival
    room().emit('ParticipantConnected', remote('bruno'))
    await tick()
    expect(cuesPlayed()).toBe(1)
  })

  it('a guest who leaves is heard; one’s own leaving is not', async () => {
    const s = await join()
    room().emit('ParticipantDisconnected', remote('bruno'))
    await tick()
    expect(cuesPlayed()).toBe(1)
    await vi.advanceTimersByTimeAsync(1500)
    await s.leave()
    room().emit('ParticipantDisconnected', remote('clara'))
    await tick()
    expect(cuesPlayed()).toBe(1)
  })

  it('Not a join: a reconnection, the participants it unwinds and the events it replays play nothing', async () => {
    await join()
    // a full reconnection: the participants are unwound while the room still says connected, then it reconnects
    room().emit('ParticipantDisconnected', remote('bruno'))
    room().state = 'reconnecting'
    room().emit('Reconnecting')
    await tick()
    room().state = 'connected'
    room().emit('Reconnected')
    room().emit('ParticipantConnected', remote('bruno')) // replayed in the same task
    await tick()
    expect(cuesPlayed()).toBe(0)
    // a screen share, a mute and a track are not presence events
    for (const ev of ['TrackPublished', 'TrackMuted', 'TrackUnmuted', 'LocalTrackPublished']) room().emit(ev)
    await tick()
    expect(cuesPlayed()).toBe(0)
    room().emit('ParticipantConnected', remote('clara')) // after the reconnection, a real join
    await tick()
    expect(cuesPlayed()).toBe(1)
  })

  it('Silent while on air: no cue while stage.on_air, none later; off air is heard, and a join after it', async () => {
    await join()
    setStage(false) // the state found on arrival plays nothing
    await tick()
    expect(cuesPlayed()).toBe(0)
    setStage(true)
    room().emit('ParticipantConnected', remote('bruno'))
    await vi.advanceTimersByTimeAsync(2000)
    expect(cuesPlayed()).toBe(0)
    setStage(false)
    expect(cuesPlayed()).toBe(1) // off air
    room().emit('ParticipantConnected', remote('clara'))
    await tick()
    expect(cuesPlayed()).toBe(2)
  })

  it('On air and off air: heard with "Sounds while on air"; arriving while on air plays nothing', async () => {
    const s = await join()
    s.setCuePrefs({ sounds: true, whileOnAir: true })
    setStage(true) // the first state received: found on arrival
    await tick()
    expect(cuesPlayed()).toBe(0)
    setStage(false)
    setStage(true)
    expect(cuesPlayed()).toBe(2)
    const freqs = FakeAudioContext.all.flatMap((c) => c.oscillators.map((o) => o.frequency.calls[0]!.value))
    expect(new Set(freqs).size).toBe(2) // two different tones
  })

  it('Turned off, from inside the studio: no cue, also after a reload', async () => {
    let s = await join()
    s.setCuePrefs({ sounds: false, whileOnAir: false })
    s.close()
    setActivePinia(createPinia()) // a reload: a new store reads the stored choice
    s = await join()
    expect(s.cuePrefs.sounds).toBe(false)
    setStage(false)
    room().emit('ParticipantConnected', remote('bruno'))
    setStage(true)
    setStage(false)
    await tick()
    expect(cuesPlayed()).toBe(0)
  })

  it('Audio not started: a cue due before any click plays nothing, and is not played after the click', async () => {
    FakeAudioContext.initial = 'suspended'
    const s = useStudio()
    await s.joinGuest('tok', 'Guest', { mic: false, cam: false })
    room().emit('ParticipantConnected', remote('bruno'))
    await tick()
    document.dispatchEvent(new Event('pointerdown'))
    await vi.advanceTimersByTimeAsync(2000)
    expect(cuesPlayed()).toBe(0)
    FakeAudioContext.initial = 'running'
  })

  it('the cue player is closed when the studio is left', async () => {
    const s = await join()
    room().emit('ParticipantConnected', remote('bruno'))
    await tick()
    const ctx = FakeAudioContext.all.find((c) => c.oscillators.length)!
    s.close()
    expect(ctx.state).toBe('closed')
  })
})
