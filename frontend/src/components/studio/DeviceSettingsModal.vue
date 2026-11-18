<script setup lang="ts">
// The device settings: which microphone and camera the studio uses, and the sound cues of this browser (design U7
// of add-studio-interactions: kept in this browser, not in the account).
import { onMounted } from 'vue'
import AppModal from '@/components/AppModal.vue'
import DevicePickers from '@/components/studio/DevicePickers.vue'
import { useStudio } from '@/stores/studio'
import { t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const studio = useStudio()
onMounted(() => void studio.listDevices())

const set = (key: 'sounds' | 'whileOnAir', e: Event) =>
  studio.setCuePrefs({ ...studio.cuePrefs, [key]: (e.target as HTMLInputElement).checked })
</script>

<template>
  <AppModal :title="t('Device settings')" @close="emit('close')">
    <div class="pnl-body">
      <DevicePickers />
      <fieldset class="mt-3" data-sounds>
        <legend class="lbl">{{ t('Sounds') }} · {{ t('On this browser') }}</legend>
        <label class="flex items-center gap-2">
          <input type="checkbox" :checked="studio.cuePrefs.sounds" data-sounds-on @change="set('sounds', $event)" />
          {{ t('Play a sound when someone joins or leaves, and when the program goes on or off air') }}
        </label>
        <label class="mt-1 flex items-center gap-2">
          <input
            type="checkbox"
            :checked="studio.cuePrefs.whileOnAir"
            :disabled="!studio.cuePrefs.sounds"
            aria-describedby="sounds-on-air-hint"
            data-sounds-on-air
            @change="set('whileOnAir', $event)"
          />
          {{ t('Sounds while on air') }}
        </label>
        <p id="sounds-on-air-hint" class="hint">
          {{ t('A sound may be heard in the program through your speakers or a shared screen. Use headphones.') }}
        </p>
      </fieldset>
    </div>
    <div class="pnl-foot">
      <button type="button" class="btn" @click="emit('close')">{{ t('Done') }}</button>
    </div>
  </AppModal>
</template>
