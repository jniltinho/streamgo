// The dock's device menus (add-studio-interactions, "Device menus"): what the items say and when the arrow has
// nothing to offer. The menu's keyboard and placement are checked in a browser (07-ui-wrappers, 20-studio).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import ControlsDock from '@/components/studio/ControlsDock.vue'
import { useStudio } from '@/stores/studio'

// jsdom lacks what UI SDK's popups use (the stubs its testing guide lists)
HTMLElement.prototype.showPopover = function () {}
HTMLElement.prototype.hidePopover = function () {}
Element.prototype.getAnimations = () => []
Element.prototype.scrollIntoView = function () {}
Element.prototype.checkVisibility = () => true
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const dev = (kind: MediaDeviceKind, deviceId: string, label = '') =>
  ({ kind, deviceId, label, groupId: '' }) as MediaDeviceInfo

let app: App
let root: HTMLElement
async function mount() {
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(ControlsDock)
  app.mount(root)
  await nextTick()
}
const arrow = (kind: 'audio' | 'video') => root.querySelector<HTMLElement>(`[data-device-menu=${kind}]`)!
const tipOf = (el: HTMLElement) =>
  document
    .getElementById((el.getAttribute('aria-describedby') ?? el.getAttribute('aria-labelledby'))!)
    ?.textContent?.trim()

async function items(kind: 'audio' | 'video') {
  arrow(kind).click()
  await nextTick()
  await new Promise((r) => setTimeout(r))
  const out = [...document.querySelectorAll('[role=menuitemradio]')].map((e) => e.textContent?.trim())
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  return out
}

describe('ControlsDock device menus', () => {
  beforeEach(() => {
    const pinia = createPinia()
    setActivePinia(pinia)
    vi.spyOn(useStudio(), 'listDevices').mockResolvedValue()
  })
  afterEach(() => {
    app.unmount()
    root.remove()
    vi.restoreAllMocks()
  })

  it('lists Default first, then the devices by name', async () => {
    const s = useStudio()
    s.devices = { audio: [dev('audioinput', 'm1', 'Mic One'), dev('audioinput', 'm2', 'Mic Two')], video: [] }
    await mount()
    expect(await items('audio')).toEqual(['Default', 'Mic One', 'Mic Two'])
  })

  it('Labels not given yet: the items are numbered, and the real names come once the browser gives them', async () => {
    const s = useStudio()
    s.devices = { audio: [dev('audioinput', 'm1'), dev('audioinput', 'm2')], video: [] }
    await mount()
    expect(await items('audio')).toEqual(['Default', 'Microphone 1', 'Microphone 2'])
    s.devices = { audio: [dev('audioinput', 'm1', 'USB Mic'), dev('audioinput', 'm2', 'Headset')], video: [] }
    await nextTick()
    expect(await items('audio')).toEqual(['Default', 'USB Mic', 'Headset'])
  })

  it('A device unplugged: the next opening no longer lists it', async () => {
    const s = useStudio()
    s.devices = { audio: [dev('audioinput', 'm1', 'Mic One'), dev('audioinput', 'm2', 'Mic Two')], video: [] }
    await mount()
    s.devices = { audio: [dev('audioinput', 'm1', 'Mic One')], video: [] }
    await nextTick()
    expect(await items('audio')).toEqual(['Default', 'Mic One'])
  })

  it('Nothing to choose: no camera, or permission denied, or not listed yet: focusable, disabled, and why', async () => {
    const s = useStudio()
    await mount()
    const a = arrow('video')
    expect(a.getAttribute('aria-disabled')).toBe('true')
    expect(a.hasAttribute('disabled')).toBe(false)
    expect(a.tabIndex).toBe(0)
    expect(tipOf(a)).toBe('No camera found.')
    s.permission = { audio: 'granted', video: 'denied' }
    await nextTick()
    expect(tipOf(a)).toBe('The browser is not allowed to use the camera.')
    s.permission = { audio: 'granted', video: 'prompt' }
    s.devices = { audio: [], video: [dev('videoinput', '')] }
    await nextTick()
    expect(tipOf(a)).toBe('Turn the camera on once to list the cameras.')
    expect(await items('video')).toEqual([])
  })

  it('the meter says silent while the microphone is off or quiet, heard from 0.05 up', async () => {
    const s = useStudio()
    await mount()
    const meter = root.querySelector('[role=meter]')!
    expect(meter.getAttribute('aria-label')).toBe('Microphone level')
    expect(meter.getAttribute('aria-valuetext')).toBe('Silent')
    s.mic = true
    s.micLevel = 0.04
    await nextTick()
    expect(meter.getAttribute('aria-valuetext')).toBe('Silent')
    s.micLevel = 0.05
    await nextTick()
    expect(meter.getAttribute('aria-valuetext')).toBe('Heard')
    expect(meter.getAttribute('aria-valuenow')).toBe('5')
  })

  it('Device settings has a name tooltip and no title', async () => {
    await mount()
    const b = root.querySelector<HTMLElement>('[data-control=settings]')!
    expect(b.hasAttribute('title')).toBe(false)
    expect(b.getAttribute('aria-label')).toBe('Device settings')
    expect(tipOf(b)).toBe('Device settings')
  })

  it('Buttons with text: Mic, Camera and Leave studio carry no name tooltip', async () => {
    await mount()
    for (const sel of ['[data-control=mic]', '[data-control=camera]', '[data-control=share]'])
      expect(root.querySelector(sel)!.hasAttribute('aria-labelledby')).toBe(false)
  })
})
