<script setup lang="ts">
// The accounts of the install, for administrators (the router sends anyone else to the broadcasts): list, create,
// edit, disable and enable. An administrator cannot disable themself: the control is disabled with the reason.
import { onMounted, ref } from 'vue'
import { Plus } from '@lucide/vue'
import UserModal from '@/components/UserModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import type { AdminUser } from '@/api/types'
import { userError, useUsers } from '@/stores/users'
import { useSession } from '@/stores/session'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const store = useUsers()
const session = useSession()
const loadError = ref('')
// the create or edit modal: undefined is closed, null creates
const editing = ref<AdminUser | null | undefined>(undefined)

async function load() {
  loadError.value = ''
  try {
    await store.load()
  } catch (e) {
    loadError.value = userError(e)
  }
}
onMounted(load)

async function setDisabled(u: AdminUser, disabled: boolean) {
  if (
    disabled &&
    !(await askConfirm(t('Disable {name}? They cannot sign in and their sessions end at once.', { name: u.name }), {
      ok: t('Disable'),
      danger: true,
    }))
  )
    return
  try {
    await store.update(u.id, { disabled })
    toast.success(disabled ? t('{name} disabled.', { name: u.name }) : t('{name} enabled.', { name: u.name }))
  } catch (e) {
    toast.error(userError(e))
  }
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <h1 class="page-title mb-0 flex-1">{{ t('Users') }}</h1>
      <button type="button" class="btn" @click="editing = null">
        <Plus class="size-3.5" aria-hidden="true" /> {{ t('New user') }}
      </button>
    </div>
    <UiAlert v-if="loadError" kind="error" class="mb-3" :action="t('Try again')" @action="load">
      {{ loadError }}
    </UiAlert>

    <table v-if="store.items.length" class="grid-t">
      <thead>
        <tr>
          <th>{{ t('Name') }}</th>
          <th>{{ t('E-mail') }}</th>
          <th>{{ t('Role') }}</th>
          <th>{{ t('State') }}</th>
          <th><span class="sr-only">{{ t('Actions') }}</span></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in store.items" :key="u.id" :data-user="u.email">
          <td class="w-full">{{ u.name }}</td>
          <td class="whitespace-nowrap">{{ u.email }}</td>
          <td class="whitespace-nowrap">{{ u.admin ? t('Administrator') : t('User') }}</td>
          <td class="whitespace-nowrap">{{ u.disabled ? t('Disabled') : t('Active') }}</td>
          <td class="whitespace-nowrap">
            <!-- "Enable" and "Disable" take the same width, so the buttons line up from row to row -->
            <div class="flex justify-end gap-1">
              <button
                type="button"
                class="btn-ghost"
                :aria-label="t('Edit {name}', { name: u.name })"
                @click="editing = u"
              >
                {{ t('Edit') }}
              </button>
              <button
                v-if="u.disabled"
                type="button"
                class="btn-ghost min-w-[6rem] justify-center"
                :aria-label="t('Enable {name}', { name: u.name })"
                @click="setDisabled(u, false)"
              >
                {{ t('Enable') }}
              </button>
              <button
                v-else
                type="button"
                class="btn-ghost min-w-[6rem] justify-center"
                :disabled="u.id === session.me?.id"
                :title="u.id === session.me?.id ? t('You cannot disable your own account.') : undefined"
                :aria-label="t('Disable {name}', { name: u.name })"
                :aria-describedby="u.id === session.me?.id ? 'users-self-hint' : undefined"
                @click="setDisabled(u, true)"
              >
                {{ t('Disable') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="store.items.length" id="users-self-hint" class="hint mt-2">
      {{ t('You cannot disable your own account.') }}
    </p>

    <UserModal v-if="editing !== undefined" :user="editing" @close="editing = undefined" />
  </div>
</template>
