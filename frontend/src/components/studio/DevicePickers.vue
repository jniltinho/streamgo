<script setup lang="ts">
// The microphone and camera pickers, before joining and in the device settings. A switch that does not take leaves
// the select on the device really in use (design U3 of add-studio-interactions).
import { deviceSwitchError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()

async function choose(kind: 'audio' | 'video', e: Event) {
  const select = e.target as HTMLSelectElement
  const r = await studio.chooseDevice(kind, select.value)
  if (r === 'ok') return
  select.value = kind === 'audio' ? studio.micId : studio.camId
  toast.error(deviceSwitchError(kind, r))
}
</script>

<template>
  <div class="form-row">
    <label class="lbl" for="dev-mic">{{ t('Microphone') }}</label>
    <select id="dev-mic" class="inp" :value="studio.micId" @change="choose('audio', $event)">
      <option value="">{{ t('Default') }}</option>
      <option v-for="(d, i) in studio.devices.audio.filter((x) => x.deviceId)" :key="d.deviceId" :value="d.deviceId">
        {{ d.label || t('Microphone {n}', { n: i + 1 }) }}
      </option>
    </select>
  </div>
  <div class="form-row">
    <label class="lbl" for="dev-cam">{{ t('Camera') }}</label>
    <select id="dev-cam" class="inp" :value="studio.camId" @change="choose('video', $event)">
      <option value="">{{ t('Default') }}</option>
      <option v-for="(d, i) in studio.devices.video.filter((x) => x.deviceId)" :key="d.deviceId" :value="d.deviceId">
        {{ d.label || t('Camera {n}', { n: i + 1 }) }}
      </option>
    </select>
  </div>
</template>
