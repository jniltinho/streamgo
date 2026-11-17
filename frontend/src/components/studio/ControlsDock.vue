<script setup lang="ts">
// The controls dock: one's own microphone, camera and screen share; for the owner the invite link; the device
// settings; leaving.
import { ref } from 'vue'
import { LogOut, Mic, MicOff, MonitorUp, Settings, UserPlus, Video, VideoOff } from '@lucide/vue'
import DeviceSettingsModal from '@/components/studio/DeviceSettingsModal.vue'
import InviteModal from '@/components/studio/InviteModal.vue'
import { studioError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const emit = defineEmits<{ leave: [] }>()
const settingsOpen = ref(false)
const inviteOpen = ref(false)

async function run(fn: () => Promise<void>, failed: string) {
  try {
    await fn()
  } catch (e) {
    if (e instanceof Error && e.name === 'NotAllowedError') return // the browser's picker was cancelled
    toast.error(failed || studioError(e))
  }
}
const toggleMic = () => run(() => studio.setMic(!studio.mic), t('The microphone could not be turned on.'))
const toggleCam = () => run(() => studio.setCam(!studio.cam), t('The camera could not be turned on.'))
const toggleShare = () => run(() => studio.setSharing(!studio.sharing), t('The screen could not be shared.'))
</script>

<template>
  <div class="card studio-box" role="toolbar" :aria-label="t('Your controls')">
    <button type="button" class="btn-ghost" :aria-pressed="studio.mic" data-control="mic" @click="toggleMic">
      <component :is="studio.mic ? Mic : MicOff" class="size-4" aria-hidden="true" /> {{ t('Mic') }}
    </button>
    <button type="button" class="btn-ghost" :aria-pressed="studio.cam" data-control="camera" @click="toggleCam">
      <component :is="studio.cam ? Video : VideoOff" class="size-4" aria-hidden="true" /> {{ t('Camera') }}
    </button>
    <button type="button" class="btn-ghost" :aria-pressed="studio.sharing" data-control="share" @click="toggleShare">
      <MonitorUp class="size-4" aria-hidden="true" /> {{ studio.sharing ? t('Stop sharing') : t('Share screen') }}
    </button>
    <button v-if="studio.role === 'owner'" type="button" class="btn-ghost" @click="inviteOpen = true">
      <UserPlus class="size-4" aria-hidden="true" /> {{ t('Invite') }}
    </button>
    <button
      type="button"
      class="btn-ghost"
      :aria-label="t('Device settings')"
      :title="t('Device settings')"
      @click="settingsOpen = true"
    >
      <Settings class="size-4" aria-hidden="true" />
    </button>
    <span class="mx-1 h-6 border-l border-line" aria-hidden="true" />
    <button type="button" class="btn-ghost text-err" @click="emit('leave')">
      <LogOut class="size-4" aria-hidden="true" /> {{ t('Leave studio') }}
    </button>
    <DeviceSettingsModal v-if="settingsOpen" @close="settingsOpen = false" />
    <InviteModal v-if="inviteOpen" @close="inviteOpen = false" />
  </div>
</template>
