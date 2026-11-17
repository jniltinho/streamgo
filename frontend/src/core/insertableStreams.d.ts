// Chromium's insertable streams for media tracks (design D2), not in TypeScript's DOM library yet.
declare class MediaStreamTrackProcessor {
  constructor(init: { track: MediaStreamTrack })
  readonly readable: ReadableStream<VideoFrame>
}

declare class MediaStreamTrackGenerator extends MediaStreamTrack {
  constructor(init: { kind: 'video' })
  readonly writable: WritableStream<VideoFrame>
}
