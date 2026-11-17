// The WHIP client of design D3: publishes the program to POST /api/v1/broadcasts/:id/program/whip and ends with a
// DELETE on the Location it answered. It offers only VP8 or H.264 with Opus, asks for maintain-resolution and a
// bitrate ceiling, and gathers its ICE candidates before sending the offer (no trickle, no PATCH: MediaMTX treats
// a changed ICE fragment as a restart). When the connection fails it publishes again; the server replaces the
// previous ingest session. It never offers while the audio context is suspended: such an offer loses its audio
// track for good (spike 1.7).
import { ApiError } from '@/api/client'
import type { ErrorEnvelope } from '@/api/types'

export type WhipState = 'connecting' | 'connected' | 'retrying' | 'stopped'

export interface WhipOptions {
  broadcastId: string
  /** the compositor's program track */
  video: MediaStreamTrack
  /** the audio mix's program track */
  audio: MediaStreamTrack
  /** 'vp8' unless the studio prefers H.264 */
  codec?: 'vp8' | 'h264'
  /** the sender's bitrate ceiling in bit/s, above the transcoder's target (D2) */
  maxBitrate: number
  /** whether the audio context runs (AudioMix.running) */
  audioRunning: () => boolean
  /** state changes; with 'stopped' after a failure, the error that stopped it */
  onState?: (state: WhipState, error?: Error) => void
}

/** thrown by start() while the audio context is suspended: resume it on a user gesture first */
export class AudioSuspendedError extends Error {
  constructor() {
    super('The audio context is suspended; resume it with a click before publishing.')
    this.name = 'AudioSuspendedError'
  }
}

const RETRY_MS = 2000
const GATHER_TIMEOUT_MS = 5000

// gathered waits until the ICE candidates are in the local description, or a timeout (then sends what it has).
function gathered(pc: RTCPeerConnection): Promise<void> {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete') return resolve()
    const timer = setTimeout(resolve, GATHER_TIMEOUT_MS)
    pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState !== 'complete') return
      clearTimeout(timer)
      resolve()
    })
  })
}

function only(kind: 'audio' | 'video', mime: string): RTCRtpCodec[] {
  return (RTCRtpSender.getCapabilities(kind)?.codecs ?? []).filter(
    (c) => c.mimeType === mime && (mime !== 'video/H264' || /packetization-mode=1/.test(c.sdpFmtpLine ?? '')),
  )
}

export class WhipPublisher {
  /** the current connection, for statistics */
  pc: RTCPeerConnection | null = null
  private session: string | null = null
  private stopped = true
  private timer: ReturnType<typeof setTimeout> | undefined

  constructor(private readonly o: WhipOptions) {}

  /** start publishes once; it throws AudioSuspendedError or the server's ApiError (409 when not live, 502) */
  async start(): Promise<void> {
    this.stopped = false
    try {
      await this.publish()
    } catch (e) {
      this.stopped = true
      throw e
    }
  }

  /** stop closes the connection and ends the ingest session */
  async stop(): Promise<void> {
    this.stopped = true
    clearTimeout(this.timer)
    this.pc?.close()
    this.pc = null
    const session = this.session
    this.session = null
    this.o.onState?.('stopped')
    if (session) await fetch(session, { method: 'DELETE', credentials: 'same-origin' }).catch(() => {})
  }

  private async publish() {
    if (!this.o.audioRunning()) throw new AudioSuspendedError()
    this.o.onState?.('connecting')
    this.pc?.close()
    const pc = new RTCPeerConnection()
    this.pc = pc
    const video = pc.addTransceiver(this.o.video, { direction: 'sendonly' })
    const audio = pc.addTransceiver(this.o.audio, { direction: 'sendonly' })
    video.setCodecPreferences(only('video', this.o.codec === 'h264' ? 'video/H264' : 'video/VP8'))
    audio.setCodecPreferences(only('audio', 'audio/opus'))
    const p = video.sender.getParameters()
    p.degradationPreference = 'maintain-resolution'
    for (const e of p.encodings) e.maxBitrate = this.o.maxBitrate
    await video.sender.setParameters(p)
    pc.onconnectionstatechange = () => {
      if (pc !== this.pc || this.stopped) return
      if (pc.connectionState === 'connected') this.o.onState?.('connected')
      if (pc.connectionState === 'failed') this.again()
    }

    await pc.setLocalDescription(await pc.createOffer())
    await gathered(pc)
    let res: Response
    try {
      res = await fetch(`/api/v1/broadcasts/${encodeURIComponent(this.o.broadcastId)}/program/whip`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/sdp' },
        credentials: 'same-origin',
        body: pc.localDescription!.sdp,
      })
    } catch {
      pc.close()
      throw new ApiError(0, 'network', 'The server could not be reached.')
    }
    const location = res.headers.get('Location')
    if (res.status !== 201 || !location) {
      pc.close()
      const err = ((await res.json().catch(() => null)) as Partial<ErrorEnvelope> | null)?.error
      throw new ApiError(res.status, err?.code ?? 'internal', err?.message ?? res.statusText)
    }
    this.session = new URL(location, window.location.href).href
    await pc.setRemoteDescription({ type: 'answer', sdp: await res.text() })
  }

  // again publishes anew after a failed connection, retrying while the server is unreachable or answers 5xx.
  private again() {
    this.o.onState?.('retrying')
    clearTimeout(this.timer)
    this.timer = setTimeout(() => {
      if (this.stopped) return
      this.publish().catch((e: Error) => {
        if (this.stopped) return
        if (e instanceof ApiError && (e.status === 0 || e.status >= 500)) return this.again()
        this.stopped = true
        this.pc?.close()
        this.o.onState?.('stopped', e)
      })
    }, RETRY_MS)
  }
}
