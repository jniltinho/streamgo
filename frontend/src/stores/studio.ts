// Portions adapted from MeetHub (MIT): frontend/src/components/meetings/call.ts
// The studio (studio-ui spec) for the owner or a guest: one LiveKit connection, the stage state (room metadata and
// the answer of every stage-changing call, newest version wins), the people in the room, the compositor drawing
// the stage canvas and the audio mix. LiveKit, compositor and mix objects stay out of Vue's reactivity (markRaw):
// the reactive state is rebuilt from the room after each of its events.
import { defineStore } from 'pinia'
import { computed, markRaw, ref, shallowRef } from 'vue'
import {
  createLocalTracks,
  DisconnectReason,
  type LocalAudioTrack,
  type LocalTrack,
  type LocalVideoTrack,
  type Participant,
  Room,
  RoomEvent,
  Track,
  VideoPreset,
} from 'livekit-client'
import { api, ApiError } from '@/api/client'
import { errorMessage, t } from '@/i18n'
import type {
  Banner,
  BannerList,
  BroadcastDestination,
  BroadcastDestinationList,
  BroadcastDetail,
  GuestJoin,
  InviteLink,
  Join,
  LiveAnswer,
  Output,
  StageAnswer,
  StagePatch,
  StudioSettings,
} from '@/api/types'
import { AudioMix, stageAudio } from '@/core/audioMix'
import { AudioSuspendedError, WhipPublisher, type WhipState } from '@/core/whip'
import { Compositor, type Source } from '@/core/compositor'
import { type Layout, StageFeed, type StageSource, type StageState } from '@/core/stageState'

// the server's defaults (studio.max_on_stage, output.resolution), until GET /broadcasts/:id answers with its own
const DEFAULT_SETTINGS: StudioSettings = {
  max_on_stage: 6,
  program_width: 1280,
  program_height: 720,
  video_bitrate_kbps: 4000,
}
const PREVIEW_SIZE = { width: 960, height: 540 }
const POLL_MS = 3000
const OWNER_ATTEMPTS = 3

export interface Person {
  identity: string
  name: string
  local: boolean
  host: boolean
  mic: boolean
  cam: boolean
  screen: boolean
  /** the media server hears them right now: the proof that a microphone works */
  speaking: boolean
}

/** why the studio closed: left on purpose, removed by the owner, opened in another tab, connection lost */
export type Ended = 'left' | 'removed' | 'elsewhere' | 'lost'
/** what the guest's preview found: devices, the browser refused them, or there are none */
export type DeviceState = 'checking' | 'ok' | 'denied' | 'none'

// Cameras go out at 30 fps on every simulcast layer (design D2: LiveKit's presets send 20 and 25 fps on the lower
// ones); adaptive stream stays off so subscriptions never pause with hidden video elements.
const roomOptions = () => ({
  adaptiveStream: false,
  dynacast: false,
  videoCaptureDefaults: { resolution: { width: 1280, height: 720, frameRate: 30 } },
  publishDefaults: {
    simulcast: true,
    videoEncoding: { maxBitrate: 1_700_000, maxFramerate: 30 },
    videoSimulcastLayers: [new VideoPreset(320, 180, 160_000, 30), new VideoPreset(640, 360, 450_000, 30)],
  },
})

/** isFull: the join route refused a guest because the room holds as many people as it can */
export const isFull = (e: unknown) => e instanceof ApiError && e.code === 'studio_full'

/** a destination's status as words and as the colour of its dot */
export const destinationLabel = (d: BroadcastDestination) =>
  ({ off: t('Off'), connecting: t('Connecting'), live: t('Live'), retrying: t('Retrying') })[d.status]
export const destinationDot = (d: BroadcastDestination) =>
  ({ off: 'bg-backstage', connecting: 'bg-warn', live: 'bg-ok', retrying: 'bg-err' })[d.status]

/** studioError is the message for a failed studio request */
export const studioError = (e: unknown) =>
  e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')

// the live output's refusals say why (design, "API shapes"); each known reason is translated, another one is shown
// as the server wrote it, which is safe to show
const REFUSALS: [RegExp, (m: RegExpMatchArray) => string][] = [
  [/^Choose at least one destination/, () => t('Choose at least one destination before going live.')],
  [/limit of (\d+) live broadcasts/, (m) => t('The server is at its limit of {n} live broadcasts.', { n: m[1]! })],
  [
    /^The destination "(.*)" is in use/,
    (m) => t('The destination “{name}” is in use by another broadcast.', { name: m[1]! }),
  ],
  [/^A destination is in use/, () => t('A destination is in use by another broadcast.')],
  [
    /^A live broadcast needs at least one destination/,
    () => t('A live broadcast needs at least one destination; end the broadcast to remove the last one.'),
  ],
  [/^The broadcast is already/, () => t('The broadcast is already on air.')],
  [/^The server is stopping/, () => t('The server is stopping. Try again in a moment.')],
]

/** liveError is the message for a refused go live, end or destination change: the server's reason when it gives one */
export function liveError(e: unknown): string {
  if (!(e instanceof ApiError) || e.code !== 'conflict') return studioError(e)
  for (const [re, say] of REFUSALS) {
    const m = e.message.match(re)
    if (m) return say(m)
  }
  return e.message
}

type UserAgentData = { brands: { brand: string }[]; mobile: boolean }

/** canHost: hosting needs a Chromium-based desktop browser, whose track processor and generator compose the program */
export function canHost(): boolean {
  const ua = (navigator as Navigator & { userAgentData?: UserAgentData }).userAgentData
  return (
    !!ua &&
    !ua.mobile &&
    ua.brands.some((b) => b.brand === 'Chromium') &&
    typeof MediaStreamTrackProcessor !== 'undefined' &&
    typeof MediaStreamTrackGenerator !== 'undefined'
  )
}

export const useStudio = defineStore('studio', () => {
  const role = ref<'owner' | 'guest'>('owner')
  const broadcastId = ref('')
  const detail = ref<BroadcastDetail | null>(null)
  const title = ref('')
  const identity = ref('')
  const stage = ref<StageState | null>(null)
  const people = ref<Person[]>([])
  const phase = ref<'idle' | 'connecting' | 'joined' | 'ended'>('idle')
  const ended = ref<Ended | null>(null)
  const mic = ref(false)
  const cam = ref(false)
  const sharing = ref(false)
  const micId = ref('')
  const camId = ref('')
  const banners = ref<Banner[]>([])
  const devices = shallowRef<{ audio: MediaDeviceInfo[]; video: MediaDeviceInfo[] }>({ audio: [], video: [] })
  const preview = shallowRef<LocalTrack[]>([])
  const micLevel = ref(0)
  /** whether this browser can host (go live); computed once per page */
  const hostable = canHost()
  /** the program's WHIP connection: 'idle' when this studio is not publishing */
  const whip = ref<WhipState | 'idle'>('idle')
  /** why publishing the program failed, shown with "Resume broadcast" */
  const publishError = ref('')

  let room: Room | null = null
  let publisher: WhipPublisher | null = null
  let leaving = false
  let compositor: Compositor | null = null
  let mix: AudioMix | null = null
  let feed = new StageFeed()
  let poll: ReturnType<typeof setTimeout> | undefined
  let meter: { ctx: AudioContext; timer: ReturnType<typeof setInterval> } | null = null

  const onStage = computed(() => stage.value?.sources ?? [])
  const isOnStage = (s: StageSource) => onStage.value.some((x) => x.p === s.p && x.k === s.k)
  const settings = computed(() => detail.value?.studio ?? DEFAULT_SETTINGS)
  const maxOnStage = computed(() => settings.value.max_on_stage)
  const stageFull = computed(() => onStage.value.length >= maxOnStage.value)
  /** the broadcast is on (starting, live or interrupted): this studio should be publishing its program */
  const onAir = computed(() => !!detail.value && detail.value.output.state !== 'offline')
  /** on, but this studio does not publish: after a reload, until the audio runs, or after a failure */
  const needsResume = computed(() => role.value === 'owner' && hostable && onAir.value && whip.value === 'idle')
  /** a screen of the owner is on stage but no longer shared (after a reload): it must be shared again */
  const reshare = computed(
    () => !sharing.value && onStage.value.some((s) => s.p === identity.value && s.k === 'screen'),
  )
  /** a guest's own place: on stage when any of their sources is */
  const meOnStage = computed(() => onStage.value.some((s) => s.p === identity.value))

  // the AudioContext starts suspended on a new page: the first click or key press anywhere starts it
  const resumeAudio = () => void mix?.resume().catch(() => {})

  function applyStage(raw: unknown) {
    if (!feed.accept(raw)) return
    stage.value = feed.current
    compositor?.setStage(feed.current!)
    syncAudio()
  }

  const participants = (): Participant[] => (room ? [room.localParticipant, ...room.remoteParticipants.values()] : [])

  function videoOf(p: Participant, source: Track.Source): MediaStreamTrack | null {
    const pub = p.getTrackPublication(source)
    return pub?.track && !pub.isMuted ? pub.track.mediaStreamTrack : null
  }

  /** the live video of a camera or a shared screen, for a preview outside the stage; it changes with `people` */
  function videoTrack(s: StageSource): MediaStreamTrack | null {
    const p = participants().find((x) => x.identity === s.p)
    return p ? videoOf(p, s.k === 'screen' ? Track.Source.ScreenShare : Track.Source.Camera) : null
  }

  function refresh() {
    const all = participants()
    people.value = all.map((p) => {
      const m = p.getTrackPublication(Track.Source.Microphone)
      return {
        identity: p.identity,
        name: p.name || p.identity,
        local: p === room?.localParticipant,
        host: p.identity.startsWith('host-'),
        mic: !!m?.track && !m.isMuted,
        cam: !!videoOf(p, Track.Source.Camera),
        screen: !!p.getTrackPublication(Track.Source.ScreenShare)?.track,
        speaking: p.isSpeaking,
      }
    })
    compositor?.setSources(
      all.flatMap((p): Source[] => {
        const name = p.name || p.identity
        const screen = videoOf(p, Track.Source.ScreenShare)
        return [
          { identity: p.identity, kind: 'camera', name, track: videoOf(p, Track.Source.Camera) },
          ...(screen ? [{ identity: p.identity, kind: 'screen' as const, name, track: screen }] : []),
        ]
      }),
    )
    syncAudio()
  }

  // the program and monitor buses hear only what is on stage (remote tracks reach the speakers only through the mix)
  function syncAudio() {
    const audio = (p: Participant, s: Track.Source) => p.getTrackPublication(s)?.track?.mediaStreamTrack ?? null
    mix?.setStage(
      stageAudio(
        stage.value?.sources ?? [],
        participants().map((p) => ({
          identity: p.identity,
          local: p === room?.localParticipant,
          mic: audio(p, Track.Source.Microphone),
          screenAudio: audio(p, Track.Source.ScreenShareAudio),
        })),
      ),
    )
  }

  /** attachCanvas draws the stage on this canvas: the program for the owner, a preview for a guest */
  function attachCanvas(canvas: HTMLCanvasElement) {
    compositor?.stop()
    const owner = role.value === 'owner'
    canvas.width = owner ? settings.value.program_width : PREVIEW_SIZE.width
    canvas.height = owner ? settings.value.program_height : PREVIEW_SIZE.height
    // a browser that cannot host still shows the stage, drawn as a guest's preview is
    compositor = markRaw(new Compositor(canvas, owner && hostable ? 'program' : 'preview'))
    if (feed.current) compositor.setStage(feed.current)
    refresh()
  }

  function detachCanvas() {
    compositor?.stop()
    compositor = null
  }

  function reset(as: 'owner' | 'guest') {
    disconnect() // the canvas, mounted by now, stays
    role.value = as
    feed = new StageFeed()
    stage.value = null
    ended.value = null
    detail.value = null
    banners.value = []
    phase.value = 'connecting'
    publishError.value = ''
    const m = markRaw(new AudioMix(as === 'owner'))
    mix = m
    // the program goes out only while the audio runs: once it starts (any click), an on broadcast is published
    m.onStateChange = () => {
      if (mix === m && m.running) void ensurePublishing()
    }
    document.addEventListener('pointerdown', resumeAudio, true)
    document.addEventListener('keydown', resumeAudio, true)
  }

  async function connect(j: Join, publish: { mic: boolean; cam: boolean }) {
    const r = markRaw(new Room(roomOptions()))
    for (const ev of [
      RoomEvent.ParticipantConnected,
      RoomEvent.ParticipantDisconnected,
      RoomEvent.ParticipantNameChanged,
      RoomEvent.TrackSubscribed,
      RoomEvent.TrackUnsubscribed,
      RoomEvent.TrackMuted,
      RoomEvent.TrackUnmuted,
      RoomEvent.TrackPublished,
      RoomEvent.TrackUnpublished,
      RoomEvent.LocalTrackPublished,
      RoomEvent.Reconnected,
    ])
      r.on(ev, refresh)
    // who speaks changes several times a second: only the mark is updated, not the sources or the mix
    r.on(RoomEvent.ActiveSpeakersChanged, (speakers) => {
      const ids = new Set(speakers.map((p) => p.identity))
      people.value = people.value.map((p) => ({ ...p, speaking: ids.has(p.identity) }))
    })
    r.on(RoomEvent.LocalTrackUnpublished, (pub) => {
      if (pub.source === Track.Source.ScreenShare) sharing.value = false
      refresh()
    })
    r.on(RoomEvent.RoomMetadataChanged, (md) => applyStage(md))
    let connected = false
    r.on(RoomEvent.Disconnected, (reason) => {
      if (!connected || room !== r) return
      ended.value = leaving
        ? 'left'
        : reason === DisconnectReason.DUPLICATE_IDENTITY
          ? 'elsewhere'
          : reason === DisconnectReason.PARTICIPANT_REMOVED || reason === DisconnectReason.ROOM_DELETED
            ? 'removed'
            : 'lost'
      cleanup()
    })
    await r.connect(j.url, j.token)
    connected = true
    room = r
    identity.value = j.identity
    applyStage(r.metadata)
    stopPreview()
    mic.value = await r.localParticipant
      .setMicrophoneEnabled(publish.mic, micId.value ? { deviceId: micId.value } : undefined)
      .then(
        () => publish.mic,
        () => false,
      )
    cam.value = await r.localParticipant
      .setCameraEnabled(publish.cam, camId.value ? { deviceId: camId.value } : undefined)
      .then(
        () => publish.cam,
        () => false,
      )
    phase.value = 'joined'
    refresh()
    void listDevices()
  }

  /** openOwner opens the owner's studio of a broadcast; the room may be full, so it tries up to three tokens */
  async function openOwner(id: string) {
    reset('owner')
    broadcastId.value = id
    try {
      const d = await api<BroadcastDetail>('GET', `/broadcasts/${id}`)
      detail.value = d
      title.value = d.title
      // the canvas was attached before the server's program size was known
      const c = compositor?.canvas
      if (c && (c.width !== d.studio.program_width || c.height !== d.studio.program_height)) attachCanvas(c)
      applyStage(d.stage)
      void loadBanners().catch(() => {})
      schedulePoll()
      let last: unknown
      for (let i = 0; i < OWNER_ATTEMPTS; i++) {
        const j = await api<Join>('POST', `/broadcasts/${id}/studio-token`)
        try {
          await connect(j, { mic: true, cam: true })
          // "Recovery after a reload": a broadcast that is on is published again, at once when the audio runs
          void ensurePublishing()
          return
        } catch (e) {
          last = e
        }
      }
      throw last
    } catch (e) {
      phase.value = 'idle'
      throw e
    }
  }

  // the owner follows the output state every 3 s while it is not offline (REST contract)
  function schedulePoll() {
    clearTimeout(poll)
    if (!detail.value || detail.value.output.state === 'offline') return
    poll = setTimeout(async () => {
      await refreshDetail().catch(() => {}) // the next poll tries again
      schedulePoll()
    }, POLL_MS)
  }

  async function refreshDetail() {
    const d = await api<BroadcastDetail>('GET', `/broadcasts/${broadcastId.value}`)
    detail.value = d
    applyStage(d.stage)
    // ended elsewhere or past its deadline: stop sending; still on: make sure the program goes out
    if (d.output.state === 'offline') await stopPublishing()
    else void ensurePublishing()
  }

  function setOutput(o: Output) {
    if (detail.value) detail.value.output = o
    schedulePoll()
  }

  // ── the live output (design D3, D5) ──────────────────────────────────────────────────────────────────────────
  /** ensurePublishing publishes the program while the broadcast is on, the audio runs and nothing is published */
  async function ensurePublishing() {
    const video = compositor?.stream?.getVideoTracks()[0]
    const audio = mix?.programStream?.getAudioTracks()[0]
    if (role.value !== 'owner' || !onAir.value || publisher || !video || !audio || !mix?.running) return
    const p = markRaw(
      new WhipPublisher({
        broadcastId: broadcastId.value,
        video,
        audio,
        maxBitrate: settings.value.video_bitrate_kbps * 2000, // twice the transcoder's target (D2)
        audioRunning: () => !!mix?.running,
        onState: (s, err) => {
          if (publisher !== p) return
          whip.value = s
          if (s !== 'stopped') return
          publisher = null
          whip.value = 'idle'
          if (err) publishError.value = liveError(err)
        },
      }),
    )
    publisher = p
    publishError.value = ''
    try {
      await p.start()
    } catch (e) {
      if (publisher !== p) return
      publisher = null
      whip.value = 'idle'
      if (!(e instanceof AudioSuspendedError)) publishError.value = liveError(e)
    }
  }

  async function stopPublishing() {
    const p = publisher
    publisher = null
    whip.value = 'idle'
    await p?.stop()
  }

  /** goLive starts the output and publishes the program; the click that calls it also starts the audio */
  async function goLive() {
    await mix?.resume().catch(() => {})
    setOutput((await api<LiveAnswer>('POST', `${base()}/live`)).output)
    await ensurePublishing()
  }

  /** resumeBroadcast publishes again after a reload: the click starts the audio */
  async function resumeBroadcast() {
    await mix?.resume()
    await ensurePublishing()
  }

  /** endBroadcast ends the output, then stops publishing */
  async function endBroadcast() {
    setOutput((await api<LiveAnswer>('DELETE', `${base()}/live`)).output)
    await stopPublishing()
    await refreshDetail().catch(() => {}) // the destinations' statuses go off
  }

  /** joinGuest joins with an invite link; the guest cookie keeps the same identity on a reload */
  async function joinGuest(token: string, name: string, publish: { mic: boolean; cam: boolean }) {
    reset('guest')
    try {
      const join = () => api<GuestJoin>('POST', `/join/${encodeURIComponent(token)}`, { name })
      const j = await join()
      title.value = j.title
      try {
        await connect(j, publish)
      } catch (e) {
        // Two guests can pass the server's check for the last place; the media server then refuses one. Asking
        // again tells that from a network failure: the place is taken now, and the answer is "studio_full".
        await join().catch((again) => {
          if (isFull(again)) throw again
        })
        throw e
      }
    } catch (e) {
      disconnect()
      throw e
    }
  }

  function cleanup() {
    clearTimeout(poll)
    void stopPublishing() // the studio closes: another tab, or this one once reconnected, publishes
    room = null
    mix?.setStage([])
    people.value = []
    sharing.value = false
    phase.value = 'ended'
  }

  /** close disconnects and releases the compositor and the audio; nothing is shown as ended */
  function close() {
    disconnect()
    detachCanvas()
  }

  function disconnect() {
    clearTimeout(poll)
    void stopPublishing()
    stopPreview()
    const r = room
    room = null
    void r?.disconnect()
    mix?.close()
    mix = null
    document.removeEventListener('pointerdown', resumeAudio, true)
    document.removeEventListener('keydown', resumeAudio, true)
    people.value = []
    phase.value = 'idle'
  }

  /** leave disconnects on purpose */
  async function leave() {
    if (!room) return close()
    leaving = true
    try {
      await room.disconnect()
    } finally {
      leaving = false
    }
  }

  // ── own devices ──────────────────────────────────────────────────────────────────────────────────────────────
  async function setMic(on: boolean) {
    if (!room) {
      for (const t of preview.value) if (t.kind === Track.Kind.Audio) await (on ? t.unmute() : t.mute())
      mic.value = on
      return
    }
    await room.localParticipant.setMicrophoneEnabled(on, micId.value ? { deviceId: micId.value } : undefined)
    mic.value = on
    refresh()
  }

  async function setCam(on: boolean) {
    if (!room) {
      for (const t of preview.value) if (t.kind === Track.Kind.Video) await (on ? t.unmute() : t.mute())
      cam.value = on
      preview.value = [...preview.value]
      return
    }
    await room.localParticipant.setCameraEnabled(on, camId.value ? { deviceId: camId.value } : undefined)
    cam.value = on
    refresh()
  }

  async function setSharing(on: boolean) {
    if (!room) return
    await room.localParticipant.setScreenShareEnabled(on, { audio: true, contentHint: 'detail' })
    sharing.value = on
    refresh()
  }

  async function listDevices() {
    const [audio, video] = await Promise.all([
      Room.getLocalDevices('audioinput', false).catch(() => []),
      Room.getLocalDevices('videoinput', false).catch(() => []),
    ])
    devices.value = { audio, video }
  }

  /** chooseDevice switches the microphone or camera, in the preview before joining or in the room */
  async function chooseDevice(kind: 'audio' | 'video', id: string) {
    if (room) {
      await room.switchActiveDevice(kind === 'audio' ? 'audioinput' : 'videoinput', id)
    } else {
      const track = preview.value.find((t) => t.kind === kind) as LocalAudioTrack | LocalVideoTrack | undefined
      await track?.restartTrack({ deviceId: id || undefined })
      preview.value = [...preview.value]
      if (kind === 'audio') startMeter()
    }
    if (kind === 'audio') micId.value = id
    else camId.value = id
    refresh()
  }

  /** startPreview opens the camera and microphone before a guest joins */
  async function startPreview(): Promise<DeviceState> {
    stopPreview()
    let refused = false
    for (const want of [
      { audio: true, video: true },
      { audio: true, video: false },
      { audio: false, video: true },
    ]) {
      try {
        const tracks = await createLocalTracks({
          audio: want.audio && (micId.value ? { deviceId: micId.value } : true),
          video: want.video && (camId.value ? { deviceId: camId.value } : true),
        })
        preview.value = tracks.map((t) => markRaw(t))
        mic.value = want.audio
        cam.value = want.video
        startMeter()
        await listDevices()
        return 'ok'
      } catch (e) {
        refused ||= e instanceof Error && (e.name === 'NotAllowedError' || e.name === 'SecurityError')
      }
    }
    mic.value = false
    cam.value = false
    return refused ? 'denied' : 'none'
  }

  function stopPreview() {
    stopMeter()
    for (const t of preview.value) t.stop()
    preview.value = []
  }

  function startMeter() {
    stopMeter()
    const t = preview.value.find((x) => x.kind === Track.Kind.Audio)
    if (!t) return
    const ctx = new AudioContext()
    const analyser = ctx.createAnalyser()
    analyser.fftSize = 512
    ctx.createMediaStreamSource(new MediaStream([t.mediaStreamTrack])).connect(analyser)
    const silent = ctx.createGain() // the graph runs only when it reaches the output: through silence
    silent.gain.value = 0
    analyser.connect(silent).connect(ctx.destination)
    void ctx.resume().catch(() => {})
    const data = new Float32Array(analyser.fftSize)
    const timer = setInterval(() => {
      analyser.getFloatTimeDomainData(data)
      let sum = 0
      for (const v of data) sum += v * v
      const db = 20 * Math.log10(Math.sqrt(sum / data.length) || 1e-9)
      micLevel.value = mic.value ? Math.max(Math.min(1, Math.max(0, (db + 60) / 60)), micLevel.value * 0.85) : 0
    }, 100)
    meter = { ctx, timer }
  }

  function stopMeter() {
    if (!meter) return
    clearInterval(meter.timer)
    void meter.ctx.close().catch(() => {})
    meter = null
    micLevel.value = 0
  }

  // ── the owner's changes: each answers with the stage state, applied at once ───────────────────────────────────
  const base = () => `/broadcasts/${broadcastId.value}`
  const answer = async (p: Promise<StageAnswer>) => applyStage((await p).stage)

  const patchStage = (p: StagePatch) => answer(api<StageAnswer>('PATCH', `${base()}/stage`, p))
  const addToStage = (s: StageSource) => patchStage({ sources: [...onStage.value, s] })
  const takeOffStage = (s: StageSource) =>
    patchStage({ sources: onStage.value.filter((x) => !(x.p === s.p && x.k === s.k)) })
  const setLayout = (layout: Layout) => patchStage({ layout })
  const showBanner = (id: string | null) => patchStage({ banner_id: id })
  const setAccent = (accent: string) => patchStage({ accent })
  const removeGuest = (who: string) =>
    answer(api<StageAnswer>('DELETE', `${base()}/participants/${encodeURIComponent(who)}`))

  async function loadBanners() {
    banners.value = (await api<BannerList>('GET', `${base()}/banners`)).banners
  }
  async function addBanner(text: string) {
    banners.value.push(await api<Banner>('POST', `${base()}/banners`, { text }))
  }
  async function deleteBanner(id: string) {
    await answer(api<StageAnswer>('DELETE', `${base()}/banners/${id}`))
    banners.value = banners.value.filter((b) => b.id !== id)
  }

  function uploadLogo(file: File) {
    const form = new FormData()
    form.append('file', file)
    return answer(api<StageAnswer>('POST', `${base()}/logo`, form))
  }
  const deleteLogo = () => answer(api<StageAnswer>('DELETE', `${base()}/logo`))

  async function rotateInvite() {
    const r = await api<InviteLink>('POST', `${base()}/invite`)
    if (detail.value) detail.value.invite_url = r.invite_url
  }

  async function setDestinations(ids: string[]) {
    const r = await api<BroadcastDestinationList>('PUT', `${base()}/destinations`, { ids })
    if (detail.value) detail.value.destinations = r.destinations
  }

  return {
    hostable,
    whip,
    publishError,
    maxOnStage,
    onAir,
    needsResume,
    reshare,
    goLive,
    resumeBroadcast,
    endBroadcast,
    role,
    broadcastId,
    detail,
    title,
    identity,
    stage,
    people,
    phase,
    ended,
    mic,
    cam,
    sharing,
    micId,
    camId,
    banners,
    devices,
    preview,
    micLevel,
    onStage,
    isOnStage,
    stageFull,
    meOnStage,
    videoTrack,
    attachCanvas,
    detachCanvas,
    openOwner,
    joinGuest,
    close,
    leave,
    setMic,
    setCam,
    setSharing,
    listDevices,
    chooseDevice,
    startPreview,
    stopPreview,
    patchStage,
    addToStage,
    takeOffStage,
    setLayout,
    showBanner,
    setAccent,
    removeGuest,
    loadBanners,
    addBanner,
    deleteBanner,
    uploadLogo,
    deleteLogo,
    rotateInvite,
    setDestinations,
  }
})
