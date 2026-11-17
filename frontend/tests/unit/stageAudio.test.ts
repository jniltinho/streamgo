// studio-ui "Program audio", the studio's side: which tracks the mix hears ("Backstage is silent", "Each source
// once", "Guest reconnects", "Empty stage", "Owner's own voice", and "Camera off" keeping the audio). The mix itself
// (levels, limiter, buses) is tested in the browser (tests/e2e/04-core-audio.spec.ts).
import { describe, expect, it } from 'vitest'
import { stageAudio, type AudioPeer } from '@/core/audioMix'

const track = (id: string) => ({ id }) as unknown as MediaStreamTrack
const ownerMic = track('owner-mic')
const brunoMic = track('bruno-mic')
const carlaMic = track('carla-mic')
const carlaScreen = track('carla-screen-audio')

const peers: AudioPeer[] = [
  { identity: 'host-1', local: true, mic: ownerMic, screenAudio: null },
  { identity: 'guest-b', local: false, mic: brunoMic, screenAudio: null },
  { identity: 'guest-c', local: false, mic: carlaMic, screenAudio: carlaScreen },
]

describe('stage audio', () => {
  it('leaves backstage participants out', () => {
    expect(stageAudio([{ p: 'guest-c', k: 'camera' }], peers)).toEqual([{ track: carlaMic, local: false }])
  })

  it('takes each on-stage source once: microphones and a screen with its audio', () => {
    const got = stageAudio(
      [
        { p: 'host-1', k: 'camera' },
        { p: 'guest-c', k: 'camera' },
        { p: 'guest-c', k: 'screen' },
      ],
      peers,
    )
    expect(got).toEqual([
      { track: ownerMic, local: true },
      { track: carlaMic, local: false },
      { track: carlaScreen, local: false },
    ])
  })

  it("marks the owner's own microphone local: in the program, not in their monitor", () => {
    expect(stageAudio([{ p: 'host-1', k: 'camera' }], peers)).toEqual([{ track: ownerMic, local: true }])
  })

  it('is empty with an empty stage, and leaves out an absent participant', () => {
    expect(stageAudio([], peers)).toEqual([])
    expect(stageAudio([{ p: 'guest-gone', k: 'camera' }], peers)).toEqual([])
  })

  it('follows a reconnected guest to their new track only', () => {
    const fresh = track('bruno-mic-2')
    const after = peers.map((p) => (p.identity === 'guest-b' ? { ...p, mic: fresh } : p))
    expect(stageAudio([{ p: 'guest-b', k: 'camera' }], after)).toEqual([{ track: fresh, local: false }])
  })

  it('a screen without audio adds nothing, and a duplicate track is listed once', () => {
    expect(stageAudio([{ p: 'guest-b', k: 'screen' }], peers)).toEqual([])
    const shared = [{ identity: 'guest-x', local: false, mic: brunoMic, screenAudio: brunoMic }]
    expect(
      stageAudio(
        [
          { p: 'guest-x', k: 'camera' },
          { p: 'guest-x', k: 'screen' },
        ],
        shared,
      ),
    ).toEqual([{ track: brunoMic, local: false }])
  })
})
