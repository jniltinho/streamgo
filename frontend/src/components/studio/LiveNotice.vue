<script setup lang="ts">
// What the owner must know or do about the live output, over the top of the stage: a browser that cannot host, a
// broadcast that is on while this studio does not send it ("Resume broadcast"), a screen to share again.
import { ref } from 'vue'
import { Play, TriangleAlert } from '@lucide/vue'
import { liveError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const busy = ref(false)

async function resume() {
  busy.value = true
  try {
    await studio.resumeBroadcast()
  } catch (e) {
    toast.error(liveError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="z-10 flex w-[min(40rem,100%)] flex-col gap-2 self-start [grid-area:1/1]">
    <p v-if="!studio.hostable" class="alert alert-warning" role="note" data-cannot-host>
      <TriangleAlert class="alert-icon size-4" aria-hidden="true" />
      {{
        t(
          'Hosting needs a Chromium-based desktop browser, such as Chrome or Edge. You can prepare the stage here, but not go live.',
        )
      }}
    </p>
    <div v-if="studio.needsResume" class="alert alert-warning items-center" role="alert" data-resume>
      <TriangleAlert class="alert-icon size-4" aria-hidden="true" />
      <span class="flex-1">
        <span class="alert-title block">{{ t('The broadcast is on, but this studio is not sending it.') }}</span>
        {{ studio.publishError || t('After a reload the browser needs one click before the studio can send sound.') }}
      </span>
      <button type="button" class="btn whitespace-nowrap" :disabled="busy" @click="resume">
        <Play class="size-4" aria-hidden="true" /> {{ t('Resume broadcast') }}
      </button>
    </div>
    <p v-if="studio.reshare" class="alert alert-warning" role="note" data-reshare>
      <TriangleAlert class="alert-icon size-4" aria-hidden="true" />
      {{ t('Your screen is on stage but no longer shared: a reload stops it. Share it again with “Share screen”.') }}
    </p>
  </div>
</template>
