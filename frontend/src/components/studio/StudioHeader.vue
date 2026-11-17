<script setup lang="ts">
// The header bar: the brand and the broadcast's title; for the owner the output state with its timer, each chosen
// destination with its status, and the go-live and end controls; for a guest whether the broadcast is on air and
// whether they are on stage.
import { computed, onBeforeUnmount, ref } from 'vue'
import { Play, Radio, Square } from '@lucide/vue'
import type { Output } from '@/api/types'
import EndBroadcastModal from '@/components/studio/EndBroadcastModal.vue'
import { destinationDot as statusDot, destinationLabel as statusLabel, liveError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const now = ref(Date.now())
const clock = setInterval(() => (now.value = Date.now()), 1000)
onBeforeUnmount(() => clearInterval(clock))

const output = computed(() => studio.detail?.output)
const onAir = computed(() => (studio.role === 'owner' ? output.value?.state === 'live' : !!studio.stage?.on_air))

const stateLabel = (s: Output['state']) =>
  ({ offline: t('Offline'), starting: t('Starting'), live: t('Live'), interrupted: t('Interrupted') })[s]

const timer = computed(() => {
  const at = output.value?.started_at
  if (!at || output.value?.state === 'offline') return ''
  const s = Math.max(0, Math.floor((now.value - Date.parse(at)) / 1000))
  return [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60].map((n) => String(n).padStart(2, '0')).join(':')
})

const busy = ref(false)
const ending = ref(false)
// a refusal by the server (no destination, its limit, a destination in use) is shown as it answers
const goLiveBlocked = computed(() =>
  studio.hostable ? '' : t('Hosting needs a Chromium-based desktop browser, such as Chrome or Edge.'),
)

async function goLive() {
  busy.value = true
  try {
    await studio.goLive()
  } catch (e) {
    toast.error(liveError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="studio-brand">
    <span class="studio-mark" aria-hidden="true"><Play class="size-4" /></span>
    StreamGo
  </div>
  <h1 class="min-w-0 flex-1 truncate text-sm font-normal" data-page-heading>{{ studio.title }}</h1>

  <template v-if="studio.role === 'owner' && output">
    <span class="studio-chip" :class="{ 'on-air': onAir }" data-output-state>
      <span
        class="studio-dot status-dot"
        :class="onAir ? 'bg-on-air' : output.state === 'offline' ? 'bg-backstage' : 'bg-warn'"
        aria-hidden="true"
      />
      {{ stateLabel(output.state) }}
    </span>
    <span v-if="timer" class="font-mono text-sm" role="timer" :aria-label="t('Time on air')">{{ timer }}</span>
    <ul class="flex gap-2" :aria-label="t('Destinations')">
      <li
        v-for="d in studio.detail?.destinations ?? []"
        :key="d.id"
        class="studio-chip"
        :class="{ 'border-err': d.status === 'retrying' }"
        :title="d.error || undefined"
        :data-destination-chip="d.status"
      >
        <span class="studio-dot status-dot" :class="statusDot(d)" aria-hidden="true" />
        {{ d.name }}
        <span class="sr-only">{{ statusLabel(d) }}{{ d.error ? `: ${d.error}` : '' }}</span>
      </li>
    </ul>
    <button
      v-if="output.state === 'offline'"
      type="button"
      class="btn"
      :disabled="busy || !!goLiveBlocked || studio.phase !== 'joined'"
      :title="goLiveBlocked || undefined"
      :aria-describedby="goLiveBlocked ? 'go-live-blocked' : undefined"
      @click="goLive"
    >
      <Radio class="size-4" aria-hidden="true" /> {{ t('Go live') }}
    </button>
    <button v-else type="button" class="btn-danger" @click="ending = true">
      <Square class="size-4" aria-hidden="true" /> {{ t('End broadcast') }}
    </button>
    <span v-if="goLiveBlocked" id="go-live-blocked" class="sr-only">{{ goLiveBlocked }}</span>
    <EndBroadcastModal v-if="ending" @close="ending = false" />
  </template>

  <template v-else-if="studio.role === 'guest'">
    <span class="studio-chip" :class="{ 'on-air': onAir }" data-on-air>
      <span class="studio-dot status-dot" :class="onAir ? 'bg-on-air' : 'bg-backstage'" aria-hidden="true" />
      {{ onAir ? t('On air') : t('Not on air') }}
    </span>
    <span class="studio-chip" data-guest-place>
      <span
        class="studio-dot status-dot"
        :class="studio.meOnStage ? 'bg-on-stage' : 'bg-backstage'"
        aria-hidden="true"
      />
      {{ studio.meOnStage ? t('On stage') : t('Backstage') }}
    </span>
  </template>
</template>
