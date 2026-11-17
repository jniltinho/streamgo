<script setup lang="ts">
// Settings, Server (add-server-settings): the output's default resolution, its ceiling and the video bitrate, for
// administrators. A field the server's environment sets is disabled and says where it is changed.
import { computed, onMounted, ref } from 'vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { useServerSettings } from '@/stores/settings'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const store = useServerSettings()
const resolution = ref('')
const maxResolution = ref('')
const fixed = ref(false)
const kbps = ref(6000)
const error = ref('')
const busy = ref(false)
const ENV: Record<string, string> = {
  resolution: 'STREAMGO_OUTPUT_RESOLUTION',
  max_resolution: 'STREAMGO_OUTPUT_MAX_RESOLUTION',
  video_bitrate_kbps: 'STREAMGO_OUTPUT_VIDEO_BITRATE_KBPS',
}
const locked = (field: string) => !!store.output?.locked.includes(field)
const all = computed(() => store.output?.resolutions ?? [])
// the default offers the ladder up to the chosen ceiling, and the current default (saving it above is refused)
const defaults = computed(() =>
  all.value.filter((r, i) => i <= all.value.indexOf(maxResolution.value) || r === resolution.value),
)

function fill() {
  const o = store.output
  if (!o) return
  resolution.value = o.resolution
  maxResolution.value = o.max_resolution
  fixed.value = o.video_bitrate_kbps !== 0
  if (fixed.value) kbps.value = o.video_bitrate_kbps
}

onMounted(() => {
  store.load().then(fill, (e) => (error.value = message(e)))
})

function message(e: unknown): string {
  if (!(e instanceof ApiError)) return t('Unexpected error.')
  if (e.code === 'conflict') return t('This setting is set by the server environment.')
  if (e.field === 'resolution') return t('The default resolution cannot be above the highest resolution.')
  if (e.field === 'video_bitrate_kbps') return t('The bitrate is 500 to 40000 kbit/s.')
  return errorMessage(e.code, e.message)
}

async function save() {
  error.value = ''
  busy.value = true
  const output: Record<string, string | number> = {}
  if (!locked('resolution')) output.resolution = resolution.value
  if (!locked('max_resolution')) output.max_resolution = maxResolution.value
  if (!locked('video_bitrate_kbps')) output.video_bitrate_kbps = fixed.value ? kbps.value : 0
  try {
    await store.save({ output })
    fill()
    toast.success(t('Server settings saved.'))
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form
    class="grid max-w-[24rem] gap-3"
    novalidate
    :aria-label="t('Server')"
    @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()"
  >
    <UiAlert v-if="error" kind="error">{{ error }}</UiAlert>
    <template v-if="store.output">
      <div>
        <label class="lbl" for="srv-max">{{ t('Highest resolution') }}</label>
        <select
          id="srv-max"
          v-model="maxResolution"
          class="inp"
          :disabled="locked('max_resolution')"
          aria-describedby="srv-max-hint"
        >
          <option v-for="r in all" :key="r" :value="r">{{ r }}</option>
        </select>
        <p id="srv-max-hint" class="hint mt-1">
          <template v-if="locked('max_resolution')">
            {{ t('Set by the server environment ({name}): change it there.', { name: ENV.max_resolution! }) }}
          </template>
          <template v-else>{{ t('A lower ceiling applies to a live broadcast from its next go-live.') }}</template>
        </p>
      </div>
      <div>
        <label class="lbl" for="srv-default">{{ t('Default resolution of new broadcasts') }}</label>
        <select
          id="srv-default"
          v-model="resolution"
          class="inp"
          :disabled="locked('resolution')"
          :aria-describedby="locked('resolution') ? 'srv-default-hint' : undefined"
        >
          <option v-for="r in defaults" :key="r" :value="r">{{ r }}</option>
        </select>
        <p v-if="locked('resolution')" id="srv-default-hint" class="hint mt-1">
          {{ t('Set by the server environment ({name}): change it there.', { name: ENV.resolution! }) }}
        </p>
      </div>
      <div>
        <label class="lbl" for="srv-bitrate">{{ t('Video bitrate') }}</label>
        <select
          id="srv-bitrate"
          v-model="fixed"
          class="inp"
          :disabled="locked('video_bitrate_kbps')"
          :aria-describedby="locked('video_bitrate_kbps') ? 'srv-bitrate-hint' : undefined"
        >
          <option :value="false">{{ t('By resolution') }}</option>
          <option :value="true">{{ t('Fixed') }}</option>
        </select>
        <p v-if="locked('video_bitrate_kbps')" id="srv-bitrate-hint" class="hint mt-1">
          {{ t('Set by the server environment ({name}): change it there.', { name: ENV.video_bitrate_kbps! }) }}
        </p>
      </div>
      <div v-if="fixed">
        <label class="lbl" for="srv-kbps">{{ t('kbit/s (500 to 40000)') }}</label>
        <input
          id="srv-kbps"
          v-model.number="kbps"
          class="inp"
          type="number"
          required
          min="500"
          max="40000"
          :disabled="locked('video_bitrate_kbps')"
        />
      </div>
      <div><button type="submit" class="btn" :disabled="busy">{{ t('Save') }}</button></div>
    </template>
  </form>
</template>
