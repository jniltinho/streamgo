<script setup lang="ts">
// The owner's studio (studio-ui "Studio screen for the owner"): the regions of the approved design over one
// LiveKit connection. Another tab opening the same studio takes over: this one stops and says so, without
// reconnecting.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StudioLayout from '@/layouts/StudioLayout.vue'
import BackstageRow from '@/components/studio/BackstageRow.vue'
import ControlsDock from '@/components/studio/ControlsDock.vue'
import LayoutPicker from '@/components/studio/LayoutPicker.vue'
import SidePanel from '@/components/studio/SidePanel.vue'
import StudioHeader from '@/components/studio/StudioHeader.vue'
import StudioStage from '@/components/studio/StudioStage.vue'
import { ApiError } from '@/api/client'
import { studioError, useStudio } from '@/stores/studio'
import { t } from '@/i18n'

const studio = useStudio()
const route = useRoute()
const router = useRouter()
const failed = ref('')

async function open() {
  failed.value = ''
  try {
    await studio.openOwner(String(route.params.id))
  } catch (e) {
    failed.value =
      e instanceof ApiError && e.code === 'not_found'
        ? t('This broadcast does not exist.')
        : e instanceof ApiError
          ? studioError(e)
          : t('Could not connect to the studio. Check the connection and try again.')
  }
}

async function leave() {
  await studio.leave()
  await router.push('/broadcasts')
}

const reload = () => location.reload()

// "Publishing while hidden": while a broadcast is on and this tab is hidden, its title in the tab strip warns the
// owner that this tab sends it
const title = document.title
function warnHidden() {
  document.title =
    document.visibilityState === 'hidden' && studio.onAir ? t('● On air from this tab: keep it open') : title
}

watch(() => studio.onAir, warnHidden)
onMounted(() => {
  void open()
  document.addEventListener('visibilitychange', warnHidden)
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', warnHidden)
  document.title = title
  studio.close()
})
</script>

<template>
  <div v-if="studio.ended === 'elsewhere' || studio.ended === 'lost' || failed" class="studio-message">
    <div class="card max-w-md p-6 text-center" role="alert">
      <h1 class="page-title">
        {{ studio.ended === 'elsewhere' ? t('The studio was opened elsewhere') : t('The studio is not connected') }}
      </h1>
      <p class="hint mb-4">
        {{
          studio.ended === 'elsewhere'
            ? t('This broadcast is now hosted in another tab or window. This one stopped and will not reconnect.')
            : failed || t('The connection to the studio was lost.')
        }}
      </p>
      <div class="flex justify-center gap-2">
        <button type="button" class="btn" @click="studio.ended === 'elsewhere' ? reload() : open()">
          {{ studio.ended === 'elsewhere' ? t('Host here instead') : t('Try again') }}
        </button>
        <RouterLink to="/broadcasts" class="btn-ghost">{{ t('Back to broadcasts') }}</RouterLink>
      </div>
    </div>
  </div>
  <StudioLayout v-else host>
    <template #header><StudioHeader /></template>
    <template #stage><StudioStage /></template>
    <template #picker><LayoutPicker /></template>
    <template #dock><ControlsDock @leave="leave" /></template>
    <template #backstage><BackstageRow /></template>
    <template #side><SidePanel /></template>
  </StudioLayout>
</template>
