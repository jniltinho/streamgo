<script setup lang="ts">
// Create a broadcast, or rename the one given.
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import type { Broadcast } from '@/api/types'
import { useBroadcasts } from '@/stores/broadcasts'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ broadcast: Broadcast | null }>()
const emit = defineEmits<{ close: [] }>()
const store = useBroadcasts()
const title = ref(props.broadcast?.title ?? '')
const error = ref('')
const busy = ref(false)

async function save() {
  busy.value = true
  error.value = ''
  try {
    const name = title.value.trim()
    if (props.broadcast) {
      await store.rename(props.broadcast.id, name)
      toast.success(t('Broadcast renamed.'))
    } else {
      await store.create(name)
      toast.success(t('Broadcast {title} created.', { title: name }))
    }
    emit('close')
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="broadcast ? t('Rename broadcast') : t('New broadcast')" @close="emit('close')">
    <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()">
      <div class="pnl-body">
        <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
        <label class="lbl" for="b-title">{{ t('Title') }}</label>
        <input id="b-title" v-model="title" class="inp" required maxlength="120" pattern=".*\S.*" />
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="busy">{{ broadcast ? t('Save') : t('Create') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </AppModal>
</template>
