// The output quality of a broadcast as the studio uses it (add-output-quality): the program's size and bitrate come
// from the broadcast detail; these helpers turn them into what the canvas, the screen capture and the hint need.

export interface ProgramQuality {
  program_width: number
  program_height: number
  video_bitrate_kbps: number
}

/** the size a shared screen is captured at: up to the program's (LiveKit's default stops at 1080p) */
export const screenCapture = (q: ProgramQuality) => ({
  width: q.program_width,
  height: q.program_height,
  frameRate: 30,
})

/** the program canvas must be rebuilt: its size is not the program's */
export const sizeChanged = (canvas: { width: number; height: number }, q: ProgramQuality) =>
  canvas.width !== q.program_width || canvas.height !== q.program_height

/** a resolution above Full HD, which some platforms do not accept */
export const aboveFullHD = (resolution: string) => Number.parseInt(resolution, 10) > 1080

/** the owner's upload the program may use, in Mbit/s: the WHIP sender's ceiling is twice the transcoder's bitrate */
export const uploadMbps = (q: ProgramQuality) => Math.round((q.video_bitrate_kbps * 2) / 100) / 10
