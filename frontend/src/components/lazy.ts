// Components loaded after the entry (design U8 and U10 of add-studio-interactions): UI SDK stays out of the static
// closure of the entry and of the sign-in page, which paint without waiting for it.
import { defineAsyncComponent, defineComponent, h, onErrorCaptured, type Component } from 'vue'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

type Loader = () => Promise<Component | { default: Component }>

export const RETRIES = 3
export const RETRY_MS = 500
const nothing = defineComponent({ render: () => null })
const loadErrors = new WeakSet<Error>()

async function withRetries(loader: Loader) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await loader()
    } catch (e) {
      if (attempt > RETRIES) throw e instanceof Error ? e : new Error(String(e))
      await new Promise((r) => setTimeout(r, RETRY_MS * attempt))
    }
  }
}

// lazy loads a component's chunk, trying again a few times (a dropped connection, a deploy that replaced the
// files); when it still fails, onFail runs and the error is logged once, by Vue, with nothing rendered. A later
// mount tries again.
export function lazy(loader: Loader, onFail?: () => void) {
  return defineAsyncComponent({
    loader: () =>
      withRetries(loader).catch((err: Error) => {
        loadErrors.add(err)
        onFail?.()
        throw err
      }),
    errorComponent: nothing,
  })
}

// lazyModal is a modal (closed by its `close` event) fetched when first opened. When its chunk cannot be loaded,
// an error toast says so and the modal closes, so the control that opened it works again once the network is back.
export function lazyModal(loader: Loader) {
  const inner = lazy(loader)
  return defineComponent({
    inheritAttrs: false,
    setup(_, { attrs }) {
      onErrorCaptured((err) => {
        if (!(err instanceof Error) || !loadErrors.has(err)) return
        toast.error(t('Could not load this window. Check the connection and try again.'))
        ;(attrs.onClose as (() => void) | undefined)?.()
        return false
      })
      return () => h(inner, attrs)
    },
  })
}
