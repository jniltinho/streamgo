<script setup lang="ts">
// The microphone and camera pickers, before joining and in the device settings.
import { useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()

async function choose(kind: 'audio' | 'video', e: Event) {
  try {
    await studio.chooseDevice(kind, (e.target as HTMLSelectElement).value)
  } catch {
    toast.error(t('That device could not be used.'))
  }
}
</script>

<template>
  <div class="form-row">
    <label class="lbl" for="dev-mic">{{ t('Microphone') }}</label>
    <select id="dev-mic" class="inp" :value="studio.micId" @change="choose('audio', $event)">
      <option value="">{{ t('Default') }}</option>
      <option v-for="d in studio.devices.audio" :key="d.deviceId" :value="d.deviceId">
        {{ d.label || t('Microphone') }}
      </option>
    </select>
  </div>
  <div class="form-row">
    <label class="lbl" for="dev-cam">{{ t('Camera') }}</label>
    <select id="dev-cam" class="inp" :value="studio.camId" @change="choose('video', $event)">
      <option value="">{{ t('Default') }}</option>
      <option v-for="d in studio.devices.video" :key="d.deviceId" :value="d.deviceId">
        {{ d.label || t('Camera') }}
      </option>
    </select>
  </div>
</template>
