<!-- Portions adapted from MeetHub (MIT): frontend/src/layouts/AppLayout.vue -->
<script setup lang="ts">
// The signed-in shell: a header with the navigation, the user's name, the language switch, the theme picker and
// sign-out; the page below it in a centred column of limited width (web-shell "Pages on large screens": 60rem is
// half of a Full HD window and, with the D13 root size, half of every larger one).
import { Palette } from '@lucide/vue'
import { useRouter } from 'vue-router'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const session = useSession()
const router = useRouter()

async function signOut() {
  try {
    await session.logout()
    await router.push({ name: 'login' })
  } catch {
    toast.error(t('Could not sign out. Check the connection and try again.'))
  }
}
</script>

<template>
  <div class="content h-(--view-h)">
    <header class="content-head">
      <strong class="text-fg-strong">StreamGo</strong>
      <nav class="flex flex-1 gap-1" :aria-label="t('Main')">
        <RouterLink to="/broadcasts" class="act" exact-active-class="font-semibold">{{ t('Broadcasts') }}</RouterLink>
        <RouterLink to="/destinations" class="act" exact-active-class="font-semibold">
          {{ t('Destinations') }}
        </RouterLink>
      </nav>
      <span class="text-fg-muted" data-user-name>{{ session.me?.name }}</span>
      <LanguageSelect id="app-lang" class="w-auto" :aria-label="t('Language')" />
      <details class="relative">
        <summary class="icon-btn" :aria-label="t('Theme')"><Palette class="size-4" aria-hidden="true" /></summary>
        <div class="card absolute right-0 z-50 mt-1 w-[36rem] p-2"><ThemePicker /></div>
      </details>
      <button type="button" class="btn-ghost" @click="signOut">{{ t('Sign out') }}</button>
    </header>
    <main class="content-main">
      <div class="mx-auto w-full max-w-[60rem]" data-page-column>
        <RouterView />
      </div>
    </main>
  </div>
</template>
