<!-- Portions adapted from MeetHub (MIT): frontend/src/views/LoginView.vue -->
<script setup lang="ts">
// Sign-in: e-mail and password, back to the page the visitor came from (?next=, kept inside the app by safeNext).
// The language and the theme can be chosen before signing in, in the preferences modal.
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogIn, Palette } from '@lucide/vue'
import PreferencesModal from '@/components/PreferencesModal.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { safeNext } from '@/router'
import { useSession } from '@/stores/session'
import { errorMessage, t } from '@/i18n'

const session = useSession()
const route = useRoute()
const router = useRouter()
const email = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
const prefsOpen = ref(false)

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await session.login(email.value, password.value)
    await router.replace(safeNext(route.query.next))
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-page h-(--view-h) overflow-auto">
    <main class="auth-box">
      <div class="auth-brand">StreamGo</div>
      <h1 class="auth-title">{{ t('Sign in') }}</h1>
      <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
      <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && submit()">
        <div class="form-row">
          <label class="lbl" for="email">{{ t('E-mail') }}</label>
          <input id="email" v-model="email" class="inp" type="email" autocomplete="username" required />
        </div>
        <div class="form-row">
          <label class="lbl" for="password">{{ t('Password') }}</label>
          <input
            id="password"
            v-model="password"
            class="inp"
            type="password"
            autocomplete="current-password"
            required
          />
        </div>
        <button type="submit" class="btn w-full justify-center" :disabled="busy">
          <LogIn class="size-3.5" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Sign in') }}
        </button>
      </form>
      <button type="button" class="act mt-4 px-0" @click="prefsOpen = true">
        <Palette class="size-3.5" aria-hidden="true" /> {{ t('Theme and language') }}
      </button>
    </main>
    <PreferencesModal v-if="prefsOpen" @close="prefsOpen = false" />
  </div>
</template>
