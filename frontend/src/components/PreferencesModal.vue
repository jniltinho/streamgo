<script setup lang="ts">
// Theme and language in one modal, a tab each: the sign-in page and the signed-in header open it, so neither
// carries those controls itself. Signed in it is "Settings", with a third tab for the name shown on the user's
// name tag and the user's own password, and for administrators a Server tab (ServerSettingsTab.vue).
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ServerSettingsTab from '@/components/ServerSettingsTab.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ initial?: 'theme' | 'language' | 'profile' }>()
const emit = defineEmits<{ close: [] }>()
const session = useSession()
const tab = ref<'theme' | 'language' | 'profile' | 'server'>(props.initial ?? 'theme')
const name = ref(session.me?.name ?? '')
const nameError = ref('')
const busy = ref(false)

async function saveName() {
  busy.value = true
  nameError.value = ''
  try {
    name.value = (await session.updateMe({ name: name.value.trim() })).name
    toast.success(t('Name saved.'))
  } catch (e) {
    nameError.value = e instanceof ApiError ? e.message : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}

// "Change password": a different repetition and a wrong current password show at their field
const current = ref('')
const password = ref('')
const repeat = ref('')
const pwError = ref('')
const currentEl = ref<HTMLInputElement>()
const repeatEl = ref<HTMLInputElement>()

async function changePassword(form: HTMLFormElement) {
  pwError.value = ''
  repeatEl.value!.setCustomValidity(
    repeat.value && repeat.value !== password.value ? t('The passwords do not match.') : '',
  )
  if (!form.reportValidity()) return
  busy.value = true
  try {
    await session.changePassword(current.value, password.value)
    current.value = password.value = repeat.value = ''
    toast.success(t('Password changed. Your other sessions ended.'))
  } catch (e) {
    if (e instanceof ApiError && e.field === 'current') {
      currentEl.value!.setCustomValidity(t('This is not your current password.'))
      currentEl.value!.reportValidity()
    } else pwError.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="session.me ? t('Settings') : t('Theme and language')" wide @close="emit('close')">
    <div class="cardtabs" role="tablist">
      <button
        id="pref-tab-theme"
        type="button"
        role="tab"
        class="cardtab"
        :class="{ on: tab === 'theme' }"
        :aria-selected="tab === 'theme'"
        aria-controls="pref-panel"
        @click="tab = 'theme'"
      >
        {{ t('Theme') }}
      </button>
      <button
        id="pref-tab-language"
        type="button"
        role="tab"
        class="cardtab"
        :class="{ on: tab === 'language' }"
        :aria-selected="tab === 'language'"
        aria-controls="pref-panel"
        @click="tab = 'language'"
      >
        {{ t('Language') }}
      </button>
      <button
        v-if="session.me"
        id="pref-tab-profile"
        type="button"
        role="tab"
        class="cardtab"
        :class="{ on: tab === 'profile' }"
        :aria-selected="tab === 'profile'"
        aria-controls="pref-panel"
        @click="tab = 'profile'"
      >
        {{ t('Profile') }}
      </button>
      <button
        v-if="session.me?.admin"
        id="pref-tab-server"
        type="button"
        role="tab"
        class="cardtab"
        :class="{ on: tab === 'server' }"
        :aria-selected="tab === 'server'"
        aria-controls="pref-panel"
        @click="tab = 'server'"
      >
        {{ t('Server') }}
      </button>
    </div>
    <div id="pref-panel" class="pnl-body" role="tabpanel" :aria-labelledby="`pref-tab-${tab}`">
      <ThemePicker v-if="tab === 'theme'" />
      <div v-else-if="tab === 'language'" class="max-w-[20rem]">
        <label class="lbl" for="pref-lang">{{ t('Language') }}</label>
        <LanguageSelect id="pref-lang" />
      </div>
      <ServerSettingsTab v-else-if="tab === 'server'" />
      <div v-else class="grid items-start gap-6 md:grid-cols-2">
        <form
          class="grid max-w-[20rem] gap-3"
          novalidate
          @submit.prevent="($event.target as HTMLFormElement).reportValidity() && saveName()"
        >
          <UiAlert v-if="nameError" kind="error">{{ nameError }}</UiAlert>
          <div>
            <label class="lbl" for="pref-name">{{ t('Your name') }}</label>
            <input
              id="pref-name"
              v-model="name"
              class="inp"
              required
              maxlength="60"
              pattern=".*\S.*"
              aria-describedby="pref-name-hint"
            />
            <p id="pref-name-hint" class="hint mt-1">
              {{ t('Shown on your name tag in the studio. A studio that is open shows it once you open it again.') }}
            </p>
          </div>
          <div><button type="submit" class="btn" :disabled="busy">{{ t('Save') }}</button></div>
        </form>
        <form
          class="grid max-w-[20rem] gap-3"
          novalidate
          :aria-label="t('Change password')"
          @submit.prevent="changePassword($event.target as HTMLFormElement)"
        >
          <h3 class="text-sm font-semibold">{{ t('Change password') }}</h3>
          <UiAlert v-if="pwError" kind="error">{{ pwError }}</UiAlert>
          <div>
            <label class="lbl" for="pref-current">{{ t('Current password') }}</label>
            <input
              id="pref-current"
              ref="currentEl"
              v-model="current"
              class="inp"
              type="password"
              required
              autocomplete="current-password"
              @input="currentEl!.setCustomValidity('')"
            />
          </div>
          <div>
            <label class="lbl" for="pref-password">{{ t('New password') }}</label>
            <input
              id="pref-password"
              v-model="password"
              class="inp"
              type="password"
              required
              minlength="10"
              maxlength="256"
              autocomplete="new-password"
            />
          </div>
          <div>
            <label class="lbl" for="pref-repeat">{{ t('Repeat the new password') }}</label>
            <input
              id="pref-repeat"
              ref="repeatEl"
              v-model="repeat"
              class="inp"
              type="password"
              required
              autocomplete="new-password"
              @input="repeatEl!.setCustomValidity('')"
            />
          </div>
          <div>
            <button type="submit" class="btn" :disabled="busy">{{ t('Change password') }}</button>
          </div>
        </form>
      </div>
    </div>
  </AppModal>
</template>
