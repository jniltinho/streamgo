<!-- Portions adapted from MeetHub (MIT): frontend/src/components/ui/ToastStack.vue -->
<script setup lang="ts">
// The toasts on UI SDK's Toast (design U10 of add-studio-interactions). feedback/toasts.ts stays the one authority
// (at most five, 4 s or 8 s, paused under the pointer or the focus): its list is mirrored into a toaster of this
// component that never times out or queues anything itself. The library brings the region in the top layer, above
// an open modal, and the announcements: its live regions, errors assertively and the rest politely.
import { onUnmounted, watch } from 'vue'
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from '@lucide/vue'
import { createToaster, Toast, ToastClose, ToastDescription, ToastRegion, ToastTitle } from '@ui-sdk/vue/toast'
import {
  closeToast,
  MAX_TOASTS,
  pauseToast,
  resumeToast,
  toastState,
  type Toast as Item,
  type ToastKind,
} from '@/feedback/toasts'
import { t } from '@/i18n'

const toaster = createToaster()
const shown = new Set<number>()
watch(
  () => [...toastState.items],
  (items) => {
    const now = new Set(items.map((it) => it.id))
    for (const id of shown) {
      if (now.has(id)) continue
      shown.delete(id)
      toaster.dismiss(String(id))
    }
    for (const it of items) {
      if (shown.has(it.id)) continue
      shown.add(it.id)
      toaster({
        id: String(it.id),
        type: it.kind,
        title: it.title,
        description: it.message,
        duration: Infinity,
        priority: it.kind === 'error' ? 'assertive' : 'polite',
        data: it, // the reactive item: its slide-in state, kept while the closed toast leaves
        onDismiss: () => closeToast(it.id), // its close control, Escape or a swipe
      })
    }
  },
  { immediate: true },
)
onUnmounted(() => toaster.dismiss())

const css: Record<ToastKind, string> = { success: '', info: 'info', warning: 'warn', error: 'err' }
const icons = { success: CircleCheck, info: Info, warning: TriangleAlert, error: CircleAlert }
const item = (lib: { data?: unknown }) => lib.data as Item
</script>

<template>
  <!-- room for the toasts that animate out: the limit is MAX_TOASTS, in feedback/toasts.ts -->
  <ToastRegion
    id="toasts"
    :toaster="toaster"
    side="top"
    align="end"
    :max-visible="MAX_TOASTS * 2"
    :aria-label="t('Notifications')"
  >
    <template #default="{ toast: lib }">
      <Toast
        class="toast"
        :class="[css[item(lib).kind], { show: item(lib).show }]"
        @mouseenter="pauseToast(item(lib).id)"
        @mouseleave="resumeToast(item(lib).id)"
        @focusin="pauseToast(item(lib).id)"
        @focusout="resumeToast(item(lib).id)"
      >
        <div class="toastHead flex items-center gap-1.5">
          <component :is="icons[item(lib).kind]" class="size-3.5" aria-hidden="true" />
          <ToastTitle class="flex-1" />
          <ToastClose class="toast-close" :aria-label="t('Close')">
            <X class="size-[0.8125rem]" aria-hidden="true" />
          </ToastClose>
        </div>
        <ToastDescription class="toastBody" />
      </Toast>
    </template>
  </ToastRegion>
</template>
