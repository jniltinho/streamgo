<script setup lang="ts">
// The destinations this broadcast uses, chosen from the user's destinations; each chosen one with its status and
// last error as the API gives them.
import { computed, onMounted, ref } from 'vue'
import { useDestinations } from '@/stores/destinations'
import { destinationDot as statusDot, destinationLabel as statusLabel, studioError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const mine = useDestinations()
const busy = ref(false)
const chosen = computed(() => new Map((studio.detail?.destinations ?? []).map((d) => [d.id, d])))

onMounted(() => {
  if (!mine.loaded) mine.load().catch((e) => toast.error(studioError(e)))
})

async function toggle(id: string, on: boolean) {
  const ids = [...chosen.value.keys()].filter((x) => x !== id)
  if (on) ids.push(id)
  busy.value = true
  try {
    await studio.setDestinations(ids)
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
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
          @change="toggle(d.id, ($event.target as HTMLInputElement).checked)"
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
