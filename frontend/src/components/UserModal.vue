<script setup lang="ts">
// Create an account, or edit the one given: name, role and a new password (empty keeps the current one). An
// administrator cannot remove their own role: the checkbox is disabled with the reason.
import { computed, ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import type { AdminUser, UserPatch } from '@/api/types'
import { userError, useUsers } from '@/stores/users'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ user: AdminUser | null }>()
const emit = defineEmits<{ close: [] }>()
const store = useUsers()
const session = useSession()
const self = computed(() => props.user?.id === session.me?.id)
const name = ref(props.user?.name ?? '')
const email = ref('')
const password = ref('')
const admin = ref(props.user?.admin ?? false)
const error = ref('')
const busy = ref(false)

async function save() {
  busy.value = true
  error.value = ''
  const trimmed = name.value.trim()
  try {
    if (props.user) {
      const patch: UserPatch = { name: trimmed }
      if (admin.value !== props.user.admin) patch.admin = admin.value
      if (password.value) patch.password = password.value
      await store.update(props.user.id, patch)
      toast.success(t('User saved.'))
    } else {
      await store.create({ email: email.value.trim(), name: trimmed, password: password.value, admin: admin.value })
      toast.success(t('User {name} created.', { name: trimmed }))
    }
    emit('close')
  } catch (e) {
    error.value = userError(e, !props.user)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="user ? t('Edit user') : t('New user')" @close="emit('close')">
    <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && save()">
      <div class="pnl-body grid gap-3">
        <UiAlert v-if="error" kind="error">{{ error }}</UiAlert>
        <div>
          <label class="lbl" for="u-name">{{ t('Name') }}</label>
          <input id="u-name" v-model="name" class="inp" required maxlength="60" pattern=".*\S.*" />
        </div>
        <div v-if="!user">
          <label class="lbl" for="u-email">{{ t('E-mail') }}</label>
          <input id="u-email" v-model="email" class="inp" type="email" required maxlength="320" autocomplete="off" />
        </div>
        <div>
          <label class="lbl" for="u-password">{{ user ? t('New password') : t('Password') }}</label>
          <input
            id="u-password"
            v-model="password"
            class="inp"
            type="password"
            :required="!user"
            minlength="10"
            maxlength="256"
            autocomplete="new-password"
            aria-describedby="u-password-hint"
          />
          <p id="u-password-hint" class="hint mt-1">
            {{
              user
                ? t('Leave it empty to keep the current password. A new one ends the user’s sessions.')
                : t('At least 10 characters.')
            }}
          </p>
        </div>
        <div>
          <label class="flex items-center gap-2">
            <input v-model="admin" type="checkbox" :disabled="self" aria-describedby="u-admin-hint" />
            {{ t('Administrator') }}
          </label>
          <p id="u-admin-hint" class="hint mt-1">
            {{
              self
                ? t('You cannot remove your own administrator role.')
                : t('An administrator manages the users of this install.')
            }}
          </p>
        </div>
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="busy">{{ user ? t('Save') : t('Create') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </AppModal>
</template>
