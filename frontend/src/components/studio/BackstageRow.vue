<script setup lang="ts">
// The backstage: every participant and shared screen in the studio with whether each is on stage. The owner adds
// and takes sources off the stage and removes guests; a guest sees the names only.
import { computed, ref } from 'vue'
import { Mic, MicOff, MonitorUp, UserMinus, UserPlus, Video, VideoOff } from '@lucide/vue'
import InviteModal from '@/components/studio/InviteModal.vue'
import SourcePreview from '@/components/studio/SourcePreview.vue'
import { initials } from '@/core/compositor'
import type { StageSource } from '@/core/stageState'
import { type Person, studioError, useStudio } from '@/stores/studio'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const owner = computed(() => studio.role === 'owner')
const inviteOpen = ref(false)
const busy = ref(false)

interface Card {
  key: string
  person: Person
  source: StageSource
  track: MediaStreamTrack | null
}
// the host first, then everyone in the order they joined; each shared screen right after its owner
const cards = computed<Card[]>(() =>
  [...studio.people]
    .sort((a, b) => Number(b.host) - Number(a.host))
    .flatMap((p) => [
      { key: p.identity, person: p, source: { p: p.identity, k: 'camera' as const } },
      ...(p.screen
        ? [{ key: `${p.identity}:screen`, person: p, source: { p: p.identity, k: 'screen' as const } }]
        : []),
    ])
    .map((c) => ({ ...c, track: studio.videoTrack(c.source) })),
)

const label = (c: Card) => (c.source.k === 'screen' ? t("{name}'s screen", { name: c.person.name }) : c.person.name)
const role = (c: Card) =>
  c.source.k === 'screen' ? t('Screen share') : c.person.host ? t('Host') : c.person.local ? t('You') : t('Guest')

async function act(fn: () => Promise<void>) {
  busy.value = true
  try {
    await fn()
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}

async function removeGuest(p: Person) {
  const ok = await askConfirm(
    t('Remove {name} from the studio? They can come back only with a valid invite link.', { name: p.name }),
    {
      title: t('Remove guest'),
      ok: t('Remove'),
      danger: true,
    },
  )
  if (ok) await act(() => studio.removeGuest(p.identity))
}
</script>

<template>
  <div class="mb-2 flex items-baseline gap-3">
    <h2 class="text-sm font-semibold">{{ t('Backstage') }}</h2>
    <span class="studio-meta">{{ t('Everyone in the studio') }}</span>
  </div>
  <ul class="studio-cards" :aria-label="t('People in the studio')">
    <li
      v-if="studio.phase === 'connecting'"
      class="card studio-card justify-center text-sm"
      role="status"
      data-connecting
    >
      {{ t('Connecting to the studio…') }}
    </li>
    <li
      v-for="c in cards"
      :key="c.key"
      class="card studio-card"
      :data-card="label(c)"
      :data-on-stage="studio.isOnStage(c.source)"
    >
      <div class="flex min-w-0 items-center gap-2.5">
        <SourcePreview v-if="c.track" :track="c.track" />
        <span v-else-if="c.source.k === 'screen'" class="studio-avatar avatar" aria-hidden="true">
          <MonitorUp class="size-4" />
        </span>
        <span v-else class="studio-avatar avatar" aria-hidden="true">{{ initials(c.person.name) }}</span>
        <div class="min-w-0">
          <div class="truncate text-sm" :title="label(c)">{{ label(c) }}</div>
          <div class="studio-meta">{{ role(c) }}</div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <template v-if="studio.isOnStage(c.source)">
          <span class="studio-state flex items-center gap-1.5 text-xs whitespace-nowrap text-on-stage">
            <span class="studio-dot status-dot bg-on-stage" aria-hidden="true" />
            <span class="studio-state-text">{{ t('On stage') }}</span>
          </span>
          <button
            v-if="owner"
            type="button"
            class="btn-ghost whitespace-nowrap"
            :disabled="busy"
            :aria-label="t('Take off the stage: {name}', { name: label(c) })"
            @click="act(() => studio.takeOffStage(c.source))"
          >
            {{ t('Take off') }}
          </button>
        </template>
        <template v-else>
          <button
            v-if="owner"
            type="button"
            class="btn whitespace-nowrap"
            :disabled="busy || studio.stageFull"
            :title="
              studio.stageFull ? t('The stage is full: {n} sources at most.', { n: studio.maxOnStage }) : undefined
            "
            :aria-label="t('Add to stage: {name}', { name: label(c) })"
            :aria-describedby="studio.stageFull ? 'stage-full' : undefined"
            @click="act(() => studio.addToStage(c.source))"
          >
            {{ t('Add to stage') }}
          </button>
          <span v-else class="flex items-center gap-1.5 text-xs whitespace-nowrap text-backstage">
            <span class="studio-dot status-dot bg-backstage" aria-hidden="true" />{{ t('Backstage') }}
          </span>
        </template>
        <span class="ml-auto flex items-center gap-1.5 text-fg-muted">
          <template v-if="c.source.k === 'camera'">
            <component
              :is="c.person.mic ? Mic : MicOff"
              class="size-4"
              role="img"
              :aria-label="c.person.mic ? t('Microphone on') : t('Microphone off')"
            />
            <component
              :is="c.person.cam ? Video : VideoOff"
              class="size-4"
              role="img"
              :aria-label="c.person.cam ? t('Camera on') : t('Camera off')"
            />
          </template>
          <button
            v-if="owner && !c.person.host && c.source.k === 'camera'"
            type="button"
            class="icon-btn"
            :aria-label="t('Remove {name} from the studio', { name: c.person.name })"
            :title="t('Remove from the studio')"
            @click="removeGuest(c.person)"
          >
            <UserMinus class="size-4" aria-hidden="true" />
          </button>
        </span>
      </div>
    </li>
    <li v-if="owner" class="flex">
      <button type="button" class="card studio-card add" @click="inviteOpen = true">
        <UserPlus class="size-5" aria-hidden="true" />{{ t('Invite guest') }}
      </button>
    </li>
  </ul>
  <p v-if="owner && studio.stageFull" id="stage-full" class="sr-only">
    {{ t('The stage is full: {n} sources at most.', { n: studio.maxOnStage }) }}
  </p>
  <InviteModal v-if="inviteOpen" @close="inviteOpen = false" />
</template>
