<script setup lang="ts">
// The user's destinations: list, create, edit and delete. The address and the key are write-only (design D8): the
// list shows the name, scheme, host and whether a key is set.
import { onMounted, ref } from 'vue'
import { Plus, Send } from '@lucide/vue'
import DestinationModal from '@/components/DestinationModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import type { Destination } from '@/api/types'
import { destinationPresets } from '@/core/destinationPresets'
import { destinationError, useDestinations } from '@/stores/destinations'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const store = useDestinations()
const loadError = ref('')
// the create or edit modal: undefined is closed, null creates
const editing = ref<Destination | null | undefined>(undefined)
// the platform a new destination starts from, when one of the shortcuts opened the modal
const preset = ref('')
function create(platform = '') {
  preset.value = platform
  editing.value = null
}
const message = destinationError

async function load() {
  loadError.value = ''
  try {
    await store.load()
  } catch (e) {
    loadError.value = message(e)
  }
}
onMounted(load)

async function remove(d: Destination) {
  if (!(await askConfirm(t('Delete the destination {name}?', { name: d.name }), { ok: t('Delete'), danger: true })))
    return
  try {
    await store.remove(d.id)
    toast.success(t('Destination deleted.'))
  } catch (e) {
    toast.error(message(e))
  }
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <h1 class="page-title mb-0 flex-1">{{ t('Destinations') }}</h1>
      <button type="button" class="btn" @click="create()">
        <Plus class="size-3.5" aria-hidden="true" /> {{ t('New destination') }}
      </button>
    </div>
    <UiAlert v-if="loadError" kind="error" class="mb-3" :action="t('Try again')" @action="load">
      {{ loadError }}
    </UiAlert>

    <div v-if="store.loaded && !store.items.length" class="card empty-state">
      <Send class="size-8" aria-hidden="true" />
      <p>{{ t('No destinations yet. Add the RTMP address of a platform to stream to it.') }}</p>
      <div class="flex flex-wrap justify-center gap-2" role="group" :aria-label="t('Popular platforms')">
        <button v-for="p in destinationPresets" :key="p.id" type="button" class="btn-ghost" @click="create(p.id)">
          <Plus class="size-3.5" aria-hidden="true" /> {{ p.name }}
        </button>
      </div>
    </div>
    <table v-else-if="store.items.length" class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Name') }}</th>
          <th>{{ t('Address') }}</th>
          <th>{{ t('Stream key') }}</th>
          <th><span class="sr-only">{{ t('Actions') }}</span></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="d in store.items" :key="d.id" :data-destination="d.id">
          <td class="w-full">{{ d.name }}</td>
          <td class="whitespace-nowrap">{{ d.scheme }}://{{ d.host }}</td>
          <td class="whitespace-nowrap">{{ d.has_key ? t('Set') : t('Not set') }}</td>
          <td class="whitespace-nowrap">
            <div class="flex justify-end gap-1">
              <button
                type="button"
                class="btn-ghost"
                :aria-label="t('Edit {name}', { name: d.name })"
                @click="editing = d"
              >
                {{ t('Edit') }}
              </button>
              <button
                type="button"
                class="btn-ghost"
                :aria-label="t('Delete {name}', { name: d.name })"
                @click="remove(d)"
              >
                {{ t('Delete') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <DestinationModal
      v-if="editing !== undefined"
      :destination="editing"
      :preset="preset"
      @close="editing = undefined"
    />
  </div>
</template>
