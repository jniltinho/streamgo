<!-- Portions adapted from MeetHub (MIT): frontend/src/layouts/AppLayout.vue -->
<script setup lang="ts">
// The signed-in shell: a header with the navigation, the theme picker and sign-out, and the page below it.
import { Palette } from '@lucide/vue'
import { useRouter } from 'vue-router'
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
        <RouterLink to="/broadcasts" class="act">{{ t('Broadcasts') }}</RouterLink>
        <RouterLink to="/destinations" class="act">{{ t('Destinations') }}</RouterLink>
      </nav>
      <details class="relative">
        <summary class="icon-btn" :aria-label="t('Theme')"><Palette class="size-4" aria-hidden="true" /></summary>
        <div class="card absolute right-0 z-50 mt-1 w-[36rem] p-2"><ThemePicker /></div>
      </details>
      <button type="button" class="btn-ghost" @click="signOut">{{ t('Sign out') }}</button>
    </header>
    <RouterView />
  </div>
</template>
