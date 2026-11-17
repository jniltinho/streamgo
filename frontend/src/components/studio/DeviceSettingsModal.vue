<script setup lang="ts">
// The device settings: which microphone and camera the studio uses.
import { onMounted } from 'vue'
import AppModal from '@/components/AppModal.vue'
import DevicePickers from '@/components/studio/DevicePickers.vue'
import { useStudio } from '@/stores/studio'
import { t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const studio = useStudio()
onMounted(() => void studio.listDevices())
</script>

<template>
  <AppModal :title="t('Device settings')" @close="emit('close')">
    <div class="pnl-body"><DevicePickers /></div>
    <div class="pnl-foot">
      <button type="button" class="btn" @click="emit('close')">{{ t('Done') }}</button>
    </div>
  </AppModal>
</template>
