<script setup lang="ts">
// The output quality of the broadcast (add-output-quality) and the destinations it uses, chosen from the user's
// destinations; each chosen one with its status and last error as the API gives them.
import { computed, onMounted, ref } from 'vue'
import { useDestinations } from '@/stores/destinations'
import { destinationDot as statusDot, destinationLabel as statusLabel, liveError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'
import { aboveFullHD, uploadMbps } from '@/core/quality'

const studio = useStudio()
const mine = useDestinations()
const busy = ref(false)
const chosen = computed(() => new Map((studio.detail?.destinations ?? []).map((d) => [d.id, d])))
const quality = computed(() => studio.detail?.studio)
const qualityBusy = ref(false)
const NAMES: Record<string, string> = { '720p': 'HD', '1080p': 'Full HD', '1440p': 'QHD' }

async function chooseQuality(select: HTMLSelectElement) {
  const before = quality.value?.resolution ?? ''
  qualityBusy.value = true
  try {
    await studio.setQuality(select.value)
    toast.success(t('Output quality set to {resolution}.', { resolution: select.value }))
  } catch (e) {
    select.value = before // refused (live in another tab, above the ceiling): the choice stays as it was
    toast.error(liveError(e))
  } finally {
    qualityBusy.value = false
  }
}

onMounted(() => {
  if (!mine.loaded) mine.load().catch((e) => toast.error(liveError(e)))
})

async function toggle(id: string, box: HTMLInputElement) {
  const on = box.checked
  const ids = [...chosen.value.keys()].filter((x) => x !== id)
  if (on) ids.push(id)
  busy.value = true
  try {
    await studio.setDestinations(ids)
  } catch (e) {
    box.checked = !on // refused (the last destination of a live broadcast, one in use): the choice stays as it was
    toast.error(liveError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="quality" class="form-row mb-4" data-quality>
    <label class="lbl" for="output-quality">{{ t('Output quality') }}</label>
    <select
      id="output-quality"
      class="inp"
      :value="quality.resolution"
      :disabled="studio.onAir || qualityBusy"
      aria-describedby="output-quality-hint"
      @change="chooseQuality($event.target as HTMLSelectElement)"
    >
      <option v-for="r in quality.resolutions" :key="r" :value="r">{{ r }} ({{ NAMES[r] ?? r }})</option>
    </select>
    <p id="output-quality-hint" class="hint mt-1">
      <template v-if="studio.onAir">{{ t('The quality can change only while the broadcast is offline.') }}</template>
      <template v-else>
        {{
          t('{width}x{height} at 30 fps; uses up to {mbps} Mbit/s of your upload.', {
            width: quality.program_width,
            height: quality.program_height,
            mbps: uploadMbps(quality),
          })
        }}
        <template v-if="aboveFullHD(quality.resolution)">
          {{ t('Some platforms accept at most 1080p.') }}
        </template>
      </template>
    </p>
  </div>
  <h2 class="text-sm font-semibold">{{ t('Destinations') }}</h2>
  <p class="studio-meta mb-3">{{ t('Where this broadcast goes when it is live') }}</p>
  <ul class="flex flex-col gap-2">
    <li v-for="d in mine.items" :key="d.id" class="studio-item" :class="{ on: chosen.has(d.id) }">
      <label class="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          :checked="chosen.has(d.id)"
          :disabled="busy"
          :data-destination="d.name"
          @change="toggle(d.id, $event.target as HTMLInputElement)"
        />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-sm">{{ d.name }}</span>
          <span class="studio-meta block truncate">{{ d.scheme }}://{{ d.host }}</span>
        </span>
      </label>
      <template v-if="chosen.get(d.id)">
        <p class="mt-2 flex items-center gap-1.5 text-xs" data-destination-status>
          <span class="studio-dot status-dot" :class="statusDot(chosen.get(d.id)!)" aria-hidden="true" />
          {{ statusLabel(chosen.get(d.id)!) }}
        </p>
        <p v-if="chosen.get(d.id)!.error" class="field-error">{{ chosen.get(d.id)!.error }}</p>
      </template>
    </li>
    <li v-if="mine.loaded && !mine.items.length" class="studio-meta">
      {{ t('You have no destinations yet.') }}
    </li>
  </ul>
  <RouterLink to="/destinations" class="act mt-3 text-xs">{{ t('Manage destinations') }}</RouterLink>
</template>
