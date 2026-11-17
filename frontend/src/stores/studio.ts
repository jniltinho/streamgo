// Portions adapted from MeetHub (MIT): frontend/src/components/meetings/call.ts
// The studio (studio-ui spec) for the owner or a guest: one LiveKit connection, the stage state (room metadata and
// the answer of every stage-changing call, newest version wins), the people in the room, the compositor drawing
// the stage canvas and the audio mix. LiveKit, compositor and mix objects stay out of Vue's reactivity (markRaw):
// the reactive state is rebuilt from the room after each of its events.
import { defineStore } from 'pinia'
import { computed, markRaw, ref, shallowRef } from 'vue'
import {
  ConnectionError,
  ConnectionErrorReason,
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
  StageAnswer,
  StagePatch,
} from '@/api/types'
import { AudioMix, stageAudio } from '@/core/audioMix'
import { Compositor, type Source } from '@/core/compositor'
import { type Layout, StageFeed, type StageSource, type StageState } from '@/core/stageState'

// shortcut: the API does not expose studio.max_on_stage, so the studio assumes its largest value (6); a lower
// setting is still enforced by the server (422, shown as an error). Upgrade when GET /broadcasts/:id carries it.
export const MAX_ON_STAGE = 6
// shortcut: the program size is output.resolution, which the API does not expose; 720p is its default. Upgrade
// with the live-output step, which needs it for the WHIP sender too.
export const PROGRAM_SIZE = { width: 1280, height: 720 }
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

/** whether a LiveKit connection was refused because the room is at studio.max_participants */
export const isFull = (e: unknown) => e instanceof StudioFullError

export class StudioFullError extends Error {
  constructor() {
    super('The studio is full.')
    this.name = 'StudioFullError'
  }
}

// refusedAsFull: LiveKit refuses a join to a room at its participant limit by failing the WebSocket, and neither
// livekit-client nor the validate endpoint says why (v1.13.10). When the validate endpoint still accepts the token
// (server up, token valid, room open), the refusal is read as a full studio.
// shortcut: another WebSocket failure with LiveKit up also reads as "full"; upgrade when LiveKit or the join route
// reports a full room.
async function refusedAsFull(e: unknown, j: Join): Promise<boolean> {
  if (!(e instanceof ConnectionError) || e.reason === ConnectionErrorReason.NotAllowed) return false
  const u = new URL(j.url.replace(/^ws/, 'http'))
  u.pathname = `${u.pathname.replace(/\/$/, '')}/rtc/validate`
  u.searchParams.set('access_token', j.token)
  try {
    const r = await fetch(u)
    return r.ok && (await r.text()).trim() === 'success'
  } catch {
    return false
  }
}

/** a destination's status as words and as the colour of its dot */
export const destinationLabel = (d: BroadcastDestination) =>
  ({ off: t('Off'), connecting: t('Connecting'), live: t('Live'), retrying: t('Retrying') })[d.status]
export const destinationDot = (d: BroadcastDestination) =>
  ({ off: 'bg-backstage', connecting: 'bg-warn', live: 'bg-ok', retrying: 'bg-err' })[d.status]

/** studioError is the message for a failed studio request */
export const studioError = (e: unknown) =>
  e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')

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

  let room: Room | null = null
  let leaving = false
  let compositor: Compositor | null = null
  let mix: AudioMix | null = null
  let feed = new StageFeed()
  let poll: ReturnType<typeof setTimeout> | undefined
  let meter: { ctx: AudioContext; timer: ReturnType<typeof setInterval> } | null = null

  const onStage = computed(() => stage.value?.sources ?? [])
  const isOnStage = (s: StageSource) => onStage.value.some((x) => x.p === s.p && x.k === s.k)
  const stageFull = computed(() => onStage.value.length >= MAX_ON_STAGE)
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
    const size = role.value === 'owner' ? PROGRAM_SIZE : PREVIEW_SIZE
    canvas.width = size.width
    canvas.height = size.height
    compositor = markRaw(new Compositor(canvas, role.value === 'owner' ? 'program' : 'preview'))
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
    mix = markRaw(new AudioMix(as === 'owner'))
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
      applyStage(d.stage)
      void loadBanners().catch(() => {})
      schedulePoll()
      let last: unknown
      for (let i = 0; i < OWNER_ATTEMPTS; i++) {
        const j = await api<Join>('POST', `/broadcasts/${id}/studio-token`)
        try {
          await connect(j, { mic: true, cam: true })
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
      try {
        const d = await api<BroadcastDetail>('GET', `/broadcasts/${broadcastId.value}`)
        detail.value = d
        applyStage(d.stage)
      } catch {
        /* the next poll tries again */
      }
      schedulePoll()
    }, POLL_MS)
  }

  /** joinGuest joins with an invite link; the guest cookie keeps the same identity on a reload */
  async function joinGuest(token: string, name: string, publish: { mic: boolean; cam: boolean }) {
    reset('guest')
    try {
      const j = await api<GuestJoin>('POST', `/join/${encodeURIComponent(token)}`, { name })
      title.value = j.title
      try {
        await connect(j, publish)
      } catch (e) {
        throw (await refusedAsFull(e, j)) ? new StudioFullError() : e
      }
    } catch (e) {
      disconnect()
      throw e
    }
  }

  function cleanup() {
    clearTimeout(poll)
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
