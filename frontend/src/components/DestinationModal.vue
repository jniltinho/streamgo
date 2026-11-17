<script setup lang="ts">
// Create a destination, or edit the one given. The address and the key are write-only (design D8): editing starts
// with both fields empty, and leaving them empty keeps the stored ones (the PATCH omits them).
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import type { Destination, DestinationPatch } from '@/api/types'
import { destinationPresets } from '@/core/destinationPresets'
import { destinationError, useDestinations } from '@/stores/destinations'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ destination: Destination | null; preset?: string }>()
const emit = defineEmits<{ close: [] }>()
const store = useDestinations()
const name = ref(props.destination?.name ?? '')
const address = ref('')
const key = ref('')
const error = ref('')
const busy = ref(false)

// a popular platform fills its address and, unless the user typed one, its name; "Other" takes them back
const platform = ref('')
const chosen = () => destinationPresets.find((p) => p.id === platform.value)
function applyPlatform() {
  const p = chosen()
  const fromPreset = (v: string, k: 'name' | 'address') => !v.trim() || destinationPresets.some((x) => x[k] === v)
  if (fromPreset(name.value, 'name')) name.value = p?.name ?? ''
  if (fromPreset(address.value, 'address')) address.value = p?.address ?? ''
}
if (!props.destination && props.preset) {
  platform.value = props.preset
  applyPlatform()
}

async function save() {
  busy.value = true
  error.value = ''
  const trimmed = name.value.trim()
  try {
    if (props.destination) {
      const patch: DestinationPatch = { name: trimmed }
      if (address.value) patch.address = address.value
      if (key.value) patch.key = key.value
      await store.update(props.destination.id, patch)
      toast.success(t('Destination saved.'))
    } else {
      await store.create({ name: trimmed, address: address.value, ...(key.value ? { key: key.value } : {}) })
      toast.success(t('Destination {name} created.', { name: trimmed }))
    }
    emit('close')
  } catch (e) {
    error.value = destinationError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="destination ? t('Edit destination') : t('New destination')" @close="emit('close')">
    <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()">
      <div class="pnl-body grid gap-3">
        <UiAlert v-if="error" kind="error">{{ error }}</UiAlert>
        <div v-if="!destination">
          <label class="lbl" for="d-platform">{{ t('Platform') }}</label>
          <select id="d-platform" v-model="platform" class="inp" @change="applyPlatform">
            <option value="">{{ t('Other (type the address)') }}</option>
            <option v-for="p in destinationPresets" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </div>
        <div>
          <label class="lbl" for="d-name">{{ t('Name') }}</label>
          <input id="d-name" v-model="name" class="inp" required maxlength="60" pattern=".*\S.*" />
        </div>
        <div>
          <label class="lbl" for="d-address">{{ t('Address') }}</label>
          <input
            id="d-address"
            v-model="address"
            class="inp"
            :required="!destination"
            maxlength="1024"
            autocomplete="off"
            spellcheck="false"
            placeholder="rtmps://"
            aria-describedby="d-address-hint"
          />
          <p id="d-address-hint" class="hint mt-1">
            {{
              destination
                ? t('An address is set ({address}). Leave the field empty to keep it.', {
                    address: `${destination.scheme}://${destination.host}`,
                  })
                : chosen()
                  ? t(
                      'The usual address of {platform}. Compare it with the one the platform shows you, then paste your stream key.',
                      { platform: chosen()!.name },
                    )
                  : t('The rtmp:// or rtmps:// address the platform gives you.')
            }}
          </p>
        </div>
        <div>
          <label class="lbl" for="d-key">{{ t('Stream key') }}</label>
          <input
            id="d-key"
            v-model="key"
            class="inp"
            type="password"
            maxlength="512"
            autocomplete="off"
            aria-describedby="d-key-hint"
          />
          <p id="d-key-hint" class="hint mt-1">
            {{
              destination?.has_key
                ? t('A key is set. Leave the field empty to keep it.')
                : t('Optional. Never shown again after saving.')
            }}
          </p>
        </div>
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="busy">{{ destination ? t('Save') : t('Create') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </AppModal>
</template>
