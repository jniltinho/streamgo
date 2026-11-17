import { describe, expect, it } from 'vitest'
import { aboveFullHD, screenCapture, sizeChanged, uploadMbps } from '@/core/quality'

const qhd = { program_width: 2560, program_height: 1440, video_bitrate_kbps: 12000 }

describe('output quality', () => {
  it('captures a shared screen at up to the program size, above 1080p too', () => {
    expect(screenCapture(qhd)).toEqual({ width: 2560, height: 1440, frameRate: 30 })
    expect(screenCapture({ ...qhd, program_width: 1280, program_height: 720 })).toEqual({
      width: 1280,
      height: 720,
      frameRate: 30,
    })
  })
  it('rebuilds the canvas only when the program size changed', () => {
    expect(sizeChanged({ width: 1920, height: 1080 }, qhd)).toBe(true)
    expect(sizeChanged({ width: 2560, height: 1440 }, qhd)).toBe(false)
  })
  it('hints above Full HD only', () => {
    expect(['720p', '1080p', '1440p'].map(aboveFullHD)).toEqual([false, false, true])
  })
  it('says the upload: twice the bitrate', () => {
    expect(uploadMbps(qhd)).toBe(24)
    expect(uploadMbps({ ...qhd, video_bitrate_kbps: 6000 })).toBe(12)
  })
})
