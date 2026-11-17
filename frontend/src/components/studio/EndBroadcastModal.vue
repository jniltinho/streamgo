<script setup lang="ts">
// "End needs confirmation": nothing stops until the owner confirms; then the output ends and the program stops.
import { ref } from 'vue'
import { Square } from '@lucide/vue'
import AppModal from '@/components/AppModal.vue'
import { liveError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const studio = useStudio()
const busy = ref(false)

async function end() {
  busy.value = true
  try {
    await studio.endBroadcast()
    toast.success(t('The broadcast ended.'))
    emit('close')
  } catch (e) {
    toast.error(liveError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="t('End broadcast')" @close="emit('close')">
    <div class="pnl-body">
      <p>{{ t('End the broadcast now? Every destination stops receiving it.') }}</p>
    </div>
    <div class="pnl-foot">
      <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Keep broadcasting') }}</button>
      <button type="button" class="btn-danger" :disabled="busy" data-confirm-end @click="end">
        <Square class="size-4" aria-hidden="true" /> {{ t('End broadcast') }}
      </button>
    </div>
  </AppModal>
</template>
