<script setup lang="ts">
// The guest's way in (studio-ui "Guest join page and guest view"): the broadcast's title, a name, a preview of the
// chosen camera and microphone with device pickers, then the guest's view of the studio: the stage, whether it is
// on air, whether they are on stage, the others' names and their own controls. A reload rejoins with the same
// name, and the guest cookie keeps the same identity (studio "Reload keeps the identity").
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Mic, MicOff, Video, VideoOff } from '@lucide/vue'
import StudioLayout from '@/layouts/StudioLayout.vue'
import BackstageRow from '@/components/studio/BackstageRow.vue'
import ControlsDock from '@/components/studio/ControlsDock.vue'
import DevicePickers from '@/components/studio/DevicePickers.vue'
import StudioHeader from '@/components/studio/StudioHeader.vue'
import StudioStage from '@/components/studio/StudioStage.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiMeter from '@/components/ui/UiMeter.vue'
import { api, ApiError } from '@/api/client'
import type { Invite } from '@/api/types'
import { type DeviceState, HEARD_LEVEL, isFull, studioError, useStudio } from '@/stores/studio'
import { t } from '@/i18n'

const studio = useStudio()
const route = useRoute()
const token = String(route.params.token)
const KEY = `streamgo.join.${token}`

/** where the page is: loading, an invalid link, the form, the studio, or one of its endings */
const page = ref<'loading' | 'invalid' | 'form' | 'studio' | 'full' | 'left'>('loading')
const title = ref('')
const name = ref('')
const error = ref('')
const busy = ref(false)
const deviceState = ref<DeviceState>('checking')
const video = ref<HTMLVideoElement | null>(null)

const previewTrack = computed(() =>
  studio.cam ? studio.preview.find((x) => x.kind === 'video')?.mediaStreamTrack : undefined,
)
watch([previewTrack, video], ([track, el]) => {
  if (el) el.srcObject = track ? new MediaStream([track]) : null
})

const remembered = () => {
  try {
    return sessionStorage.getItem(KEY) ?? ''
  } catch {
    return ''
  }
}
const remember = (v: string | null) => {
  try {
    if (v === null) sessionStorage.removeItem(KEY)
    else sessionStorage.setItem(KEY, v)
  } catch {
    /* storage unavailable: a reload asks for the name again */
  }
}

onMounted(async () => {
  try {
    title.value = (await api<Invite>('GET', `/join/${encodeURIComponent(token)}`)).title
  } catch (e) {
    page.value = e instanceof ApiError && e.code === 'not_found' ? 'invalid' : 'form'
    if (page.value === 'form') error.value = studioError(e)
    return
  }
  page.value = 'form'
  const saved = remembered()
  name.value = saved
  deviceState.value = await studio.startPreview()
  if (saved) await join() // a reload: back into the studio with the same name
})
onBeforeUnmount(() => studio.close())

async function join() {
  const n = name.value.trim()
  if (!n) return
  busy.value = true
  error.value = ''
  try {
    await studio.joinGuest(token, n, { mic: studio.mic, cam: studio.cam })
    remember(n)
    page.value = 'studio'
  } catch (e) {
    if (e instanceof ApiError && e.code === 'not_found') page.value = 'invalid'
    else if (
      isFull(e) ||
      (e instanceof ApiError && e.status === 429 && e.code === 'rate_limited' && /guests/.test(e.message))
    )
      page.value = 'full'
    else
      error.value =
        e instanceof ApiError
          ? studioError(e)
          : t('Could not connect to the studio. Check the connection and try again.')
    if (page.value !== 'form') remember(null)
    else deviceState.value = await studio.startPreview()
  } finally {
    busy.value = false
  }
}

async function leave() {
  remember(null)
  await studio.leave()
}

// the studio ended: removed by the owner, left on purpose, or lost
watch(
  () => studio.ended,
  (why) => {
    if (!why || page.value !== 'studio') return
    if (why === 'removed' || why === 'left') remember(null)
    page.value = 'left'
  },
)

async function again() {
  page.value = 'form'
  await nextTick()
  deviceState.value = await studio.startPreview()
}
</script>

<template>
  <StudioLayout v-if="page === 'studio'">
    <template #header><StudioHeader /></template>
    <template #stage><StudioStage /></template>
    <template #dock><ControlsDock @leave="leave" /></template>
    <template #backstage><BackstageRow /></template>
  </StudioLayout>

  <main v-else class="auth-page h-(--view-h) overflow-auto">
    <div class="card w-[min(32rem,100%)] p-6">
      <template v-if="page === 'loading'">
        <p class="hint" role="status">{{ t('Opening the invite…') }}</p>
      </template>

      <template v-else-if="page === 'invalid'">
        <h1 class="page-title">{{ t('This invite link is not valid') }}</h1>
        <p class="hint">
          {{ t('The link may be mistyped, or the host created a new one. Ask the host for the current link.') }}
        </p>
      </template>

      <template v-else-if="page === 'full'">
        <h1 class="page-title">{{ t('The studio is full') }}</h1>
        <p class="hint mb-4">{{ t('The studio has as many people as it can hold. Try again when someone leaves.') }}</p>
        <button type="button" class="btn" @click="again">{{ t('Try again') }}</button>
      </template>

      <template v-else-if="page === 'left'">
        <template v-if="studio.ended === 'removed'">
          <h1 class="page-title">{{ t('You were removed from the studio') }}</h1>
          <p class="hint">
            {{ t('The host removed you from this broadcast. You can come back only with a new invitation.') }}
          </p>
        </template>
        <template v-else-if="studio.ended === 'elsewhere'">
          <h1 class="page-title">{{ t('You joined from another tab') }}</h1>
          <p class="hint">{{ t('The studio is open in another tab or window. This one stopped.') }}</p>
        </template>
        <template v-else>
          <h1 class="page-title">
            {{ studio.ended === 'left' ? t('You left the studio') : t('The connection to the studio was lost') }}
          </h1>
          <button type="button" class="btn mt-2" @click="again">{{ t('Join again') }}</button>
        </template>
      </template>

      <form v-else @submit.prevent="join">
        <p class="studio-meta">{{ t('Join the studio') }}</p>
        <h1 class="page-title" data-join-title>{{ title }}</h1>
        <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
        <UiAlert
          v-if="deviceState === 'denied'"
          kind="warning"
          class="mb-3"
          :title="t('Camera and microphone blocked')"
        >
          {{
            t('Allow them with the camera icon in the address bar and reload the page. You can also join without them.')
          }}
        </UiAlert>
        <UiAlert v-else-if="deviceState === 'none'" kind="info" class="mb-3">
          {{ t('No camera or microphone was found. You can join without them.') }}
        </UiAlert>

        <div class="relative mb-3">
          <video
            ref="video"
            class="studio-preview stage-tile"
            autoplay
            muted
            playsinline
            :aria-label="t('Your camera preview')"
            data-preview
          />
          <span v-if="!previewTrack" class="absolute inset-0 grid place-items-center text-on-page">
            {{ t('Camera off') }}
          </span>
        </div>
        <div class="mb-3 flex items-center gap-2">
          <button
            type="button"
            class="btn-ghost"
            :aria-pressed="studio.mic"
            :disabled="deviceState !== 'ok'"
            @click="studio.setMic(!studio.mic)"
          >
            <component :is="studio.mic ? Mic : MicOff" class="size-4" aria-hidden="true" /> {{ t('Mic') }}
          </button>
          <button
            type="button"
            class="btn-ghost"
            :aria-pressed="studio.cam"
            :disabled="deviceState !== 'ok'"
            @click="studio.setCam(!studio.cam)"
          >
            <component :is="studio.cam ? Video : VideoOff" class="size-4" aria-hidden="true" /> {{ t('Camera') }}
          </button>
          <UiMeter
            class="flex-1"
            :value="studio.mic ? studio.micLevel : 0"
            :label="t('Microphone level')"
            :text="studio.mic && studio.micLevel >= HEARD_LEVEL ? t('Heard') : t('Silent')"
            data-mic-meter
          />
        </div>
        <DevicePickers v-if="deviceState === 'ok'" />

        <label class="lbl" for="join-name">{{ t('Your name') }}</label>
        <input
          id="join-name"
          v-model="name"
          class="inp mb-3"
          required
          maxlength="40"
          autocomplete="name"
          pattern=".*\S.*"
        />
        <button type="submit" class="btn" :disabled="busy || !name.trim() || deviceState === 'checking'">
          {{ t('Enter studio') }}
        </button>
      </form>
    </div>
  </main>
</template>
