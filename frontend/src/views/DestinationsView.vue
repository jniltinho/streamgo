<script setup lang="ts">
// The user's destinations: list, create, edit and delete. The address and the key are write-only (design D8): the
// list shows the name, scheme, host and whether a key is set; editing starts with both fields empty, and leaving them
// empty keeps the stored ones (the PATCH omits them).
import { onMounted, ref } from 'vue'
import { Plus, Send } from '@lucide/vue'
import AppModal from '@/components/AppModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import type { Destination, DestinationPatch } from '@/api/types'
import { useDestinations } from '@/stores/destinations'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const store = useDestinations()
const loadError = ref('')
// the create or edit dialog: dest null creates
const editing = ref<{ dest: Destination | null; name: string; address: string; key: string } | null>(null)
const formError = ref('')
const busy = ref(false)

const message = (e: unknown) =>
  e instanceof ApiError && e.code === 'conflict'
    ? t('This destination is in use by a broadcast.')
    : e instanceof ApiError
      ? errorMessage(e.code, e.message)
      : t('Unexpected error.')

async function load() {
  loadError.value = ''
  try {
    await store.load()
  } catch (e) {
    loadError.value = message(e)
  }
}
onMounted(load)

function open(d: Destination | null) {
  formError.value = ''
  editing.value = { dest: d, name: d?.name ?? '', address: '', key: '' }
}

async function save() {
  const ed = editing.value
  if (!ed) return
  busy.value = true
  formError.value = ''
  const name = ed.name.trim()
  try {
    if (ed.dest) {
      const patch: DestinationPatch = { name }
      if (ed.address) patch.address = ed.address
      if (ed.key) patch.key = ed.key
      await store.update(ed.dest.id, patch)
      toast.success(t('Destination saved.'))
    } else {
      await store.create({ name, address: ed.address, ...(ed.key ? { key: ed.key } : {}) })
      toast.success(t('Destination {name} created.', { name }))
    }
    editing.value = null
  } catch (e) {
    formError.value = message(e)
  } finally {
    busy.value = false
  }
}

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
      <button type="button" class="btn" @click="open(null)">
        <Plus class="size-3.5" aria-hidden="true" /> {{ t('New destination') }}
      </button>
    </div>
    <UiAlert v-if="loadError" kind="error" class="mb-3" :action="t('Try again')" @action="load">
      {{ loadError }}
    </UiAlert>

    <div v-if="store.loaded && !store.items.length" class="card empty-state">
      <Send class="size-8" aria-hidden="true" />
      <p>{{ t('No destinations yet. Add the RTMP address of a platform to stream to it.') }}</p>
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
              <button type="button" class="btn-ghost" :aria-label="t('Edit {name}', { name: d.name })" @click="open(d)">
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

    <AppModal
      v-if="editing"
      :title="editing.dest ? t('Edit destination') : t('New destination')"
      @close="editing = null"
    >
      <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()">
        <div class="pnl-body grid gap-3">
          <UiAlert v-if="formError" kind="error">{{ formError }}</UiAlert>
          <div>
            <label class="lbl" for="d-name">{{ t('Name') }}</label>
            <input id="d-name" v-model="editing.name" class="inp" required maxlength="60" pattern=".*\S.*" />
          </div>
          <div>
            <label class="lbl" for="d-address">{{ t('Address') }}</label>
            <input
              id="d-address"
              v-model="editing.address"
              class="inp"
              :required="!editing.dest"
              maxlength="1024"
              autocomplete="off"
              spellcheck="false"
              placeholder="rtmps://"
              aria-describedby="d-address-hint"
            />
            <p id="d-address-hint" class="hint mt-1">
              {{
                editing.dest
                  ? t('An address is set ({address}). Leave the field empty to keep it.', {
                      address: `${editing.dest.scheme}://${editing.dest.host}`,
                    })
                  : t('The rtmp:// or rtmps:// address the platform gives you.')
              }}
            </p>
          </div>
          <div>
            <label class="lbl" for="d-key">{{ t('Stream key') }}</label>
            <input
              id="d-key"
              v-model="editing.key"
              class="inp"
              type="password"
              maxlength="512"
              autocomplete="off"
              aria-describedby="d-key-hint"
            />
            <p id="d-key-hint" class="hint mt-1">
              {{
                editing.dest?.has_key
                  ? t('A key is set. Leave the field empty to keep it.')
                  : t('Optional. Never shown again after saving.')
              }}
            </p>
          </div>
        </div>
        <div class="pnl-foot">
          <button type="submit" class="btn" :disabled="busy">{{ editing.dest ? t('Save') : t('Create') }}</button>
          <button type="button" class="btn-ghost" @click="editing = null">{{ t('Cancel') }}</button>
        </div>
      </form>
    </AppModal>
  </div>
</template>
