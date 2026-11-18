// The toast stack and the dialog host loaded after the entry (design U10 of add-studio-interactions): a toast or a
// dialog asked before its host is mounted, and a host whose chunk never loads. The chunk is a deferred promise.
// The tests run in order: the last one leaves the dialogs unavailable for this file's module state.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App, type Component } from 'vue'
import { lazy, lazyModal, RETRIES } from '@/components/lazy'
import { askConfirm, dialogsUnavailable, dialogState, showAlert } from '@/feedback/dialogs'
import { closeToast, DURATION, toast, toastState } from '@/feedback/toasts'

// jsdom has no Web Animations: UI SDK's presence waits for none
Element.prototype.getAnimations ??= () => []
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

type Chunk = { default: Component }
function deferred() {
  let resolve!: (m: Chunk) => void
  let reject!: (e: Error) => void
  const promise = new Promise<Chunk>((res, rej) => ((resolve = res), (reject = rej)))
  return { promise, resolve, reject }
}
const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve()
  await nextTick()
}

let app: App | null = null
function mount(root: Component): HTMLElement {
  const el = document.createElement('div')
  document.body.append(el)
  app = createApp(root)
  app.mount(el)
  return el
}
afterEach(() => {
  app?.unmount()
  app = null
  document.body.innerHTML = ''
})

describe('a toast pushed before the stack is mounted', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    for (const it of toastState.items.slice()) closeToast(it.id)
    vi.useRealTimers()
  })

  it('waits, then is shown and stays its full time', async () => {
    const chunk = deferred()
    const Stack = lazy(() => chunk.promise)
    mount(() => h(Stack))
    const id = toast.success('Saved.')
    await vi.advanceTimersByTimeAsync(DURATION.success * 3) // the chunk is slow: nothing can show the toast
    expect(toastState.items.map((t) => t.id)).toEqual([id])
    expect(toastState.items[0]!.show).toBe(false)

    chunk.resolve(await import('@/components/ui/ToastStack.vue'))
    await flush()
    await vi.advanceTimersByTimeAsync(50)
    expect(toastState.items[0]!.show).toBe(true)
    await vi.advanceTimersByTimeAsync(DURATION.success - 100)
    expect(toastState.items[0]?.show).toBe(true) // its whole time, counted from the stack's arrival
    await vi.advanceTimersByTimeAsync(600)
    expect(toastState.items).toHaveLength(0)
  })
})

describe('a dialog asked before the host is mounted', () => {
  it('is shown when the host mounts, focus on Cancel, and gives the focus back to its opener', async () => {
    const chunk = deferred()
    const Host = lazy(() => chunk.promise)
    mount(() => [h('button', { id: 'opener' }, 'Delete'), h(Host)])
    const opener = document.getElementById('opener') as HTMLButtonElement
    opener.focus()
    const answer = askConfirm('Delete it?')
    opener.blur() // the focus moved meanwhile: it still goes back to what had it when it was asked
    await flush()
    expect(document.querySelector('[role=alertdialog]')).toBeNull()

    chunk.resolve(await import('@/components/ui/DialogHost.vue'))
    await vi.waitFor(() => expect(document.querySelector('[role=alertdialog]')).not.toBeNull())
    // the first focus on Cancel needs a real browser: tests/e2e/09-lazy-feedback.spec.ts
    const cancel = [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Cancel')!
    cancel.click()
    await expect(answer).resolves.toBe(false)
    await vi.waitFor(() => expect(document.activeElement).toBe(opener))
  })
})

describe('a chunk that does not load', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('is tried again, then every pending and later dialog settles as cancelled, logged once', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const loads: ReturnType<typeof deferred>[] = []
    const Host = lazy(() => {
      loads.push(deferred())
      return loads.at(-1)!.promise
    }, dialogsUnavailable)
    mount(() => h(Host))
    const confirm = askConfirm('Delete it?')
    const alert = showAlert('Saved.')
    for (let i = 0; i <= RETRIES; i++) {
      loads.at(-1)!.reject(new Error('Failed to fetch dynamically imported module'))
      await vi.advanceTimersByTimeAsync(5000)
    }
    expect(loads).toHaveLength(RETRIES + 1)
    await expect(confirm).resolves.toBe(false)
    await expect(alert).resolves.toBeUndefined()
    await expect(askConfirm('Later?')).resolves.toBe(false)
    expect(dialogState.current).toBeNull()
    expect(error).toHaveBeenCalledTimes(1) // the failure is logged once
    error.mockRestore()
    warn.mockRestore()
  })

  it('a modal that does not load shows an error toast and closes, and opens once the network is back', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    let fail = true
    let loads = 0
    const Modal = lazyModal(() => {
      loads++
      return fail
        ? Promise.reject(new Error('offline'))
        : Promise.resolve(defineComponent({ render: () => h('p', { id: 'modal' }, 'open') }))
    })
    const Root = defineComponent({
      data: () => ({ open: true }),
      render() {
        return this.open ? h(Modal, { onClose: () => (this.open = false) }) : h('button', { id: 'btn' }, 'Open')
      },
    })
    const el = mount(Root)
    await vi.advanceTimersByTimeAsync(5000)
    expect(loads).toBe(RETRIES + 1)
    expect(el.querySelector('#btn')).not.toBeNull() // closed: the control that opened it is back
    expect(toastState.items.at(-1)?.kind).toBe('error')

    fail = false
    ;(app!._instance!.proxy as unknown as { open: boolean }).open = true
    await vi.advanceTimersByTimeAsync(10)
    await flush()
    expect(el.querySelector('#modal')).not.toBeNull()
    expect(loads).toBe(RETRIES + 2)
    vi.restoreAllMocks()
  })
})
