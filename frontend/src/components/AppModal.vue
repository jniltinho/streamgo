<!-- Portions adapted from MeetHub (MIT): frontend/src/components/chat/ChatModal.vue -->
<script setup lang="ts">
// A themed modal (<dialog>.showModal(): focus trapped by the browser, Escape closes, focus returns to the opener).
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { X } from '@lucide/vue'
import { t } from '@/i18n'

defineProps<{ title: string }>()
const emit = defineEmits<{ close: [] }>()
const dlg = ref<HTMLDialogElement | null>(null)
const opener = document.activeElement as HTMLElement | null

onMounted(async () => {
  await nextTick()
  if (dlg.value && !dlg.value.open && typeof dlg.value.showModal === 'function') dlg.value.showModal()
  dlg.value?.querySelector<HTMLElement>('input, select, textarea, button:not(.modal-x)')?.focus()
})
onBeforeUnmount(() => {
  dlg.value?.close() // while a modal is open the rest of the page is inert and cannot take the focus
  opener?.focus?.()
})
</script>

<template>
  <dialog ref="dlg" class="pnl w-[min(28rem,92vw)]" aria-labelledby="app-modal-title" @cancel.prevent="emit('close')">
    <div class="pnl-head flex items-center">
      <span id="app-modal-title" class="flex-1">{{ title }}</span>
      <button type="button" class="icon-btn modal-x" :aria-label="t('Close')" @click="emit('close')">
        <X class="size-4" aria-hidden="true" />
      </button>
    </div>
    <slot />
  </dialog>
</template>
