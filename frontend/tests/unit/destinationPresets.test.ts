// "Platform presets": every preset is an encrypted RTMP address the destination form can fill in.
import { describe, expect, it } from 'vitest'
import { destinationPresets } from '../../src/core/destinationPresets'

describe('destination presets', () => {
  it('are encrypted RTMP addresses with a host and an application path, under unique ids and names', () => {
    for (const p of destinationPresets) expect(p.address, p.id).toMatch(/^rtmps:\/\/[a-z0-9.-]+(:443)?\/[a-z0-9]+\/?$/)
    expect(new Set(destinationPresets.map((p) => p.id)).size).toBe(destinationPresets.length)
    expect(new Set(destinationPresets.map((p) => p.name)).size).toBe(destinationPresets.length)
  })
})
