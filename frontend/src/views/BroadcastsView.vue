<script setup lang="ts">
// The user's broadcasts, newest first, with their state: create, rename, delete, and open one in the studio.
import { onMounted, ref } from 'vue'
import { Plus, Radio } from '@lucide/vue'
import AppModal from '@/components/AppModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import type { Broadcast, Output } from '@/api/types'
import { useBroadcasts } from '@/stores/broadcasts'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const store = useBroadcasts()
const loadError = ref('')
// the create or rename dialog: id null creates
const editing = ref<{ id: string | null; title: string } | null>(null)
const formError = ref('')
const busy = ref(false)

const message = (e: unknown) => (e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))

const stateLabel = (s: Output['state']) =>
  ({ offline: t('Offline'), starting: t('Starting'), live: t('Live'), interrupted: t('Interrupted') })[s]
const stateDot = (s: Output['state']) =>
  ({ offline: 'bg-fg-muted', starting: 'bg-warn', live: 'bg-on-air', interrupted: 'bg-err' })[s]

async function load() {
  loadError.value = ''
  try {
    await store.load()
  } catch (e) {
    loadError.value = message(e)
  }
}
onMounted(load)

function open(b: Broadcast | null) {
  formError.value = ''
  editing.value = { id: b?.id ?? null, title: b?.title ?? '' }
}

async function save() {
  const ed = editing.value
  if (!ed) return
  busy.value = true
  formError.value = ''
  try {
    const title = ed.title.trim()
    if (ed.id) {
      await store.rename(ed.id, title)
      toast.success(t('Broadcast renamed.'))
    } else {
      await store.create(title)
      toast.success(t('Broadcast {title} created.', { title }))
    }
    editing.value = null
  } catch (e) {
    formError.value = message(e)
  } finally {
    busy.value = false
  }
}

async function remove(b: Broadcast) {
  if (!(await askConfirm(t('Delete the broadcast {title}?', { title: b.title }), { ok: t('Delete'), danger: true })))
    return
  try {
    await store.remove(b.id)
    toast.success(t('Broadcast deleted.'))
  } catch (e) {
    toast.error(
      e instanceof ApiError && e.code === 'conflict' ? t('End the broadcast before deleting it.') : message(e),
    )
  }
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <h1 class="page-title mb-0 flex-1">{{ t('Broadcasts') }}</h1>
      <button type="button" class="btn" @click="open(null)">
        <Plus class="size-3.5" aria-hidden="true" /> {{ t('New broadcast') }}
      </button>
    </div>
    <UiAlert v-if="loadError" kind="error" class="mb-3" :action="t('Try again')" @action="load">
      {{ loadError }}
    </UiAlert>

    <div v-if="store.loaded && !store.items.length" class="card empty-state">
      <Radio class="size-8" aria-hidden="true" />
      <p>{{ t('No broadcasts yet. Create one to open the studio.') }}</p>
    </div>
    <table v-else-if="store.items.length" class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Title') }}</th>
          <th>{{ t('State') }}</th>
          <th><span class="sr-only">{{ t('Actions') }}</span></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="b in store.items" :key="b.id" :data-broadcast="b.id">
          <td class="w-full">{{ b.title }}</td>
          <td class="whitespace-nowrap">
            <span class="status-dot mr-1.5 inline-block size-2" :class="stateDot(b.output.state)" aria-hidden="true" />
            {{ stateLabel(b.output.state) }}
          </td>
          <td class="whitespace-nowrap">
            <div class="flex justify-end gap-1">
              <RouterLink :to="`/studio/${b.id}`" class="btn">{{ t('Open studio') }}</RouterLink>
              <button
                type="button"
                class="btn-ghost"
                :aria-label="t('Rename {title}', { title: b.title })"
                @click="open(b)"
              >
                {{ t('Rename') }}
              </button>
              <button
                type="button"
                class="btn-ghost"
                :aria-label="t('Delete {title}', { title: b.title })"
                @click="remove(b)"
              >
                {{ t('Delete') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <AppModal v-if="editing" :title="editing.id ? t('Rename broadcast') : t('New broadcast')" @close="editing = null">
      <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()">
        <div class="pnl-body">
          <UiAlert v-if="formError" kind="error" class="mb-3">{{ formError }}</UiAlert>
          <label class="lbl" for="b-title">{{ t('Title') }}</label>
          <input id="b-title" v-model="editing.title" class="inp" required maxlength="120" pattern=".*\S.*" />
        </div>
        <div class="pnl-foot">
          <button type="submit" class="btn" :disabled="busy">{{ editing.id ? t('Save') : t('Create') }}</button>
          <button type="button" class="btn-ghost" @click="editing = null">{{ t('Cancel') }}</button>
        </div>
      </form>
    </AppModal>
  </div>
</template>
