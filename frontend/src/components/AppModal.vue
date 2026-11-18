<!-- Portions adapted from MeetHub (MIT): frontend/src/components/chat/ChatModal.vue -->
<script setup lang="ts">
// A themed modal on UI SDK's Dialog (design U10 of add-studio-interactions): mounted open by its parent's v-if, it
// traps focus, makes the page inert, closes on Escape and its close control (never on a click outside), starts on
// the first control of its body and gives the focus back to its opener.
import { onUnmounted } from 'vue'
import { X } from '@lucide/vue'
import { Dialog, DialogClose, DialogContent, DialogTitle } from '@ui-sdk/vue/dialog'
import { t } from '@/i18n'

defineProps<{ title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const opener = document.activeElement as HTMLElement | null
// open while mounted: Escape and the close control ask the parent, which unmounts it; the focus goes back here,
// once the page is no longer inert
onUnmounted(() => opener?.focus?.())
</script>

<template>
  <Dialog :open="true" :close-on-outside-press="false" @open-change="(open: boolean) => open || emit('close')">
    <DialogContent
      class="pnl max-h-[90vh] overflow-auto"
      :class="wide ? 'w-[min(60rem,92vw)]' : 'w-[min(28rem,92vw)]'"
      initial-focus="input, select, textarea, button:not(.modal-x)"
      final-focus="none"
    >
      <div class="pnl-head flex items-center">
        <DialogTitle>
          <template #render="{ props }">
            <span v-bind="props" class="pnl-title">{{ title }}</span>
          </template>
        </DialogTitle>
        <DialogClose class="icon-btn modal-x" :aria-label="t('Close')">
          <X class="size-4" aria-hidden="true" />
        </DialogClose>
      </div>
      <slot />
    </DialogContent>
  </Dialog>
</template>
