<script setup lang="ts">
// The controls dock: one's own microphone (with its level meter) and camera, each with a menu of the devices of
// that kind, the screen share; for the owner the invite link; the device settings; leaving.
import { ref } from 'vue'
import { LogOut, Mic, MicOff, MonitorUp, Settings, UserPlus, Video, VideoOff } from '@lucide/vue'
import DeviceSettingsModal from '@/components/studio/DeviceSettingsModal.vue'
import InviteModal from '@/components/studio/InviteModal.vue'
import UiMenu from '@/components/ui/UiMenu.vue'
import UiMeter from '@/components/ui/UiMeter.vue'
import UiTooltip from '@/components/ui/UiTooltip.vue'
import { deviceSwitchError, HEARD_LEVEL, studioError, useStudio } from '@/stores/studio'
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

// the devices of a kind as menu items, Default first; a browser not yet allowed gives no names: they are numbered
function items(kind: 'audio' | 'video') {
  const listed = studio.devices[kind].filter((d) => d.deviceId)
  return [
    { value: '', label: t('Default') },
    ...listed.map((d, i) => ({
      value: d.deviceId,
      label: d.label || t(kind === 'audio' ? 'Microphone {n}' : 'Camera {n}', { n: i + 1 }),
    })),
  ]
}

// why there is nothing to choose: permission denied, no device, or devices the browser does not show yet
function unavailable(kind: 'audio' | 'video'): string {
  if (studio.devices[kind].some((d) => d.deviceId)) return ''
  const mic = kind === 'audio'
  if (studio.permission[kind] === 'denied')
    return mic ? t('The browser is not allowed to use the microphone.') : t('The browser is not allowed to use the camera.')
  if (!studio.devices[kind].length) return mic ? t('No microphone found.') : t('No camera found.')
  return mic ? t('Turn the microphone on once to list the microphones.') : t('Turn the camera on once to list the cameras.')
}

async function choose(kind: 'audio' | 'video', id: string) {
  const r = await studio.chooseDevice(kind, id)
  if (r !== 'ok') toast.error(deviceSwitchError(kind, r))
}
</script>

<template>
  <div class="card studio-box" role="toolbar" :aria-label="t('Your controls')">
    <span class="dock-group">
      <button type="button" class="btn-ghost" :aria-pressed="studio.mic" data-control="mic" @click="toggleMic">
        <component :is="studio.mic ? Mic : MicOff" :key="String(studio.mic)" class="size-4 dock-icon" aria-hidden="true" />
        {{ t('Mic') }}
      </button>
      <UiMenu
        :label="t('Choose a microphone')"
        :items="items('audio')"
        :value="studio.micId"
        :disabled="unavailable('audio')"
        data-device-menu="audio"
        @open="studio.listDevices()"
        @select="choose('audio', $event)"
      />
      <UiMeter
        :value="studio.mic ? studio.micLevel : 0"
        :label="t('Microphone level')"
        :text="studio.mic && studio.micLevel >= HEARD_LEVEL ? t('Heard') : t('Silent')"
        data-mic-meter
      />
    </span>
    <span class="dock-group">
      <button type="button" class="btn-ghost" :aria-pressed="studio.cam" data-control="camera" @click="toggleCam">
        <component :is="studio.cam ? Video : VideoOff" :key="String(studio.cam)" class="size-4 dock-icon" aria-hidden="true" />
        {{ t('Camera') }}
      </button>
      <UiMenu
        :label="t('Choose a camera')"
        :items="items('video')"
        :value="studio.camId"
        :disabled="unavailable('video')"
        data-device-menu="video"
        @open="studio.listDevices()"
        @select="choose('video', $event)"
      />
    </span>
    <button type="button" class="btn-ghost" :aria-pressed="studio.sharing" data-control="share" @click="toggleShare">
      <MonitorUp class="size-4" aria-hidden="true" /> {{ studio.sharing ? t('Stop sharing') : t('Share screen') }}
    </button>
    <button v-if="studio.role === 'owner'" type="button" class="btn-ghost" @click="inviteOpen = true">
      <UserPlus class="size-4" aria-hidden="true" /> {{ t('Invite') }}
    </button>
    <UiTooltip :text="t('Device settings')">
      <template #default="{ trigger }">
        <button
          v-bind="trigger"
          type="button"
          class="btn-ghost"
          :aria-label="t('Device settings')"
          data-control="settings"
          @click="settingsOpen = true"
        >
          <Settings class="size-4" aria-hidden="true" />
        </button>
      </template>
    </UiTooltip>
    <span class="mx-1 h-6 border-l border-line" aria-hidden="true" />
    <button type="button" class="btn-ghost text-err" @click="emit('leave')">
      <LogOut class="size-4" aria-hidden="true" /> {{ t('Leave studio') }}
    </button>
    <DeviceSettingsModal v-if="settingsOpen" @close="settingsOpen = false" />
    <InviteModal v-if="inviteOpen" @close="inviteOpen = false" />
  </div>
</template>
