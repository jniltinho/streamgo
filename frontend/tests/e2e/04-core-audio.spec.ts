// studio-ui "Program audio", the graph of src/core/audioMix.ts in a real Chrome: tones stand for the microphones,
// and each bus is measured (each tone once at its level, no clipping, silence with a live track). The tones are
// local tracks here; that LiveKit's remote tracks need the muted <audio> element is the spike's result (1.1), and
// what is checked here is that the mix attaches one.
import { expect, test, type Page } from '@playwright/test'

async function open(page: Page) {
  await page.goto('/tests/e2e/fixtures/core-lab.html')
  await page.waitForFunction(() => (window as unknown as { labReady?: boolean }).labReady)
}

// with the lab's helpers: L is window.lab
const run = <T>(page: Page, fn: string) =>
  page.evaluate(`(async () => { const L = window.lab; ${fn} })()`) as Promise<T>

test.beforeEach(async ({ page }) => {
  await open(page)
})

test('the context starts suspended and resumes on a click', async ({ page }) => {
  await run(page, `window.mix = new L.AudioMix(true)`)
  const before = await run<boolean>(page, `return window.mix.running`)
  await page.evaluate(() => document.body.addEventListener('click', () => void (window as any).mix.resume()))
  await page.mouse.click(10, 10)
  await expect.poll(() => run<boolean>(page, `return window.mix.running`)).toBe(true)
  // headless Chrome may let the context run without a gesture; the click must work either way
  test.info().annotations.push({ type: 'running before the click', description: String(before) })
})

// The owner (local tone 440 Hz), a guest (660 Hz) and a guest's shared screen (880 Hz) on stage; a backstage guest
// (1100 Hz) speaking. Levels in dB of the spectrum peak, against each tone's own reference.
test('each source once at its level; the owner hears the others, not themselves; backstage is silent', async ({
  page,
}) => {
  await page.mouse.click(10, 10)
  const r = await run<Record<string, number>>(
    page,
    `
    window.played = []
    const play = HTMLMediaElement.prototype.play
    HTMLMediaElement.prototype.play = function () { window.played.push({ muted: this.muted, track: this.srcObject?.getAudioTracks()[0]?.id }); return play.call(this) }
    const mix = new L.AudioMix(true); await mix.resume()
    const own = L.tone(440, 0.1), guest = L.tone(660, 0.1), screen = L.tone(880, 0.1), back = L.tone(1100, 0.1)
    // the guest's camera and screen are both on stage; the guest's microphone listed twice still enters once
    mix.setStage([{ track: own.track, local: true }, { track: guest.track, local: false },
      { track: guest.track, local: false }, { track: screen.track, local: false }])
    const prog = new AnalyserNode(mix.context, { fftSize: 8192, smoothingTimeConstant: 0 })
    mix.context.createMediaStreamSource(mix.programStream).connect(prog)
    const mon = new AnalyserNode(mix.context, { fftSize: 8192, smoothingTimeConstant: 0 })
    mix.monitor.connect(mon)
    await new Promise((r) => setTimeout(r, 1000))
    const out = {}
    for (const [name, t, f] of [['own', own, 440], ['guest', guest, 660], ['screen', screen, 880], ['back', back, 1100]]) {
      out[name + 'Ref'] = L.levelAt(t.ref, f)
      out[name + 'Program'] = L.levelAt(prog, f)
      out[name + 'Monitor'] = L.levelAt(mon, f)
    }
    out.elements = window.played.length
    out.mutedElements = window.played.filter((p) => p.muted).length
    out.guestElement = window.played.filter((p) => p.track === guest.track.id).length
    out.ownElement = window.played.filter((p) => p.track === own.track.id).length
    return out
  `,
  )
  test.info().annotations.push({ type: 'levels (dB)', description: JSON.stringify(r) })
  for (const s of ['own', 'guest', 'screen']) expect(Math.abs(r[`${s}Program`]! - r[`${s}Ref`]!)).toBeLessThan(1)
  for (const s of ['guest', 'screen']) expect(Math.abs(r[`${s}Monitor`]! - r[`${s}Ref`]!)).toBeLessThan(1)
  // own voice not in the monitor, backstage nowhere: far below the reference
  expect(r.ownMonitor!).toBeLessThan(r.ownRef! - 60)
  expect(r.backProgram!).toBeLessThan(r.backRef! - 60)
  expect(r.backMonitor!).toBeLessThan(r.backRef! - 60)
  // one muted, playing element per remote track; none for the owner's own
  expect(r.elements).toBe(2)
  expect(r.mutedElements).toBe(2)
  expect(r.guestElement).toBe(1)
  expect(r.ownElement).toBe(0)
})

test('a reconnected guest is in the program once, with no copy of the earlier track', async ({ page }) => {
  await page.mouse.click(10, 10)
  const r = await run<Record<string, number>>(
    page,
    `
    const mix = new L.AudioMix(true); await mix.resume()
    const before = L.tone(660, 0.1)
    mix.setStage([{ track: before.track, local: false }])
    await new Promise((r) => setTimeout(r, 300))
    const after = L.tone(660, 0.1) // the same voice on a new track after the reconnection
    mix.setStage([{ track: after.track, local: false }])
    const prog = new AnalyserNode(mix.context, { fftSize: 8192, smoothingTimeConstant: 0 })
    mix.context.createMediaStreamSource(mix.programStream).connect(prog)
    await new Promise((r) => setTimeout(r, 1000))
    const once = L.levelAt(prog, 660)
    const ref = L.levelAt(after.ref, 660)
    // the earlier track keeps sounding: were it still in the mix, it would be heard once the new one stops
    after.stop()
    await new Promise((r) => setTimeout(r, 500))
    return { ref, once, earlier: L.levelAt(prog, 660) }
  `,
  )
  test.info().annotations.push({ type: 'levels (dB)', description: JSON.stringify(r) })
  expect(Math.abs(r.once! - r.ref!)).toBeLessThan(1)
  expect(r.earlier!).toBeLessThan(r.ref! - 60)
})

test('six voices at full level do not clip', async ({ page }) => {
  await page.mouse.click(10, 10)
  const max = await run<number>(
    page,
    `
    const mix = new L.AudioMix(true); await mix.resume()
    const tones = [300, 410, 530, 670, 790, 970].map((f) => L.tone(f, 1))
    mix.setStage(tones.map((t) => ({ track: t.track, local: false })))
    const prog = new AnalyserNode(mix.context, { fftSize: 2048 })
    mix.context.createMediaStreamSource(mix.programStream).connect(prog)
    await new Promise((r) => setTimeout(r, 500))
    return L.peak(prog, 3000)
  `,
  )
  test.info().annotations.push({ type: 'program peak', description: String(max) })
  expect(max).toBeGreaterThan(0.3)
  expect(max).toBeLessThan(1)
})

test('an empty stage keeps a live, silent program track; a guest has no program bus', async ({ page }) => {
  await page.mouse.click(10, 10)
  const r = await run<{ tracks: number; state: string; max: number; guestProgram: boolean }>(
    page,
    `
    const mix = new L.AudioMix(true); await mix.resume()
    const t = L.tone(440, 0.5)
    const track = mix.programStream.getAudioTracks()[0]
    mix.setStage([{ track: t.track, local: false }])
    await new Promise((r) => setTimeout(r, 300))
    mix.setStage([])
    const prog = new AnalyserNode(mix.context, { fftSize: 2048 })
    mix.context.createMediaStreamSource(mix.programStream).connect(prog)
    await new Promise((r) => setTimeout(r, 300))
    return { tracks: mix.programStream.getAudioTracks().length, same: mix.programStream.getAudioTracks()[0] === track,
      state: track.readyState, max: await L.peak(prog, 1000), guestProgram: new L.AudioMix(false).programStream !== null }
  `,
  )
  expect(r).toMatchObject({ tracks: 1, same: true, state: 'live', guestProgram: false })
  expect(r.max).toBeLessThan(1e-4)
})
