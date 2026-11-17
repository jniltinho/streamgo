<!-- Portions adapted from MeetHub (MIT): frontend/src/layouts/AppLayout.vue -->
<script setup lang="ts">
// The signed-in shell: a header with the navigation, the user's name (it opens the profile), the settings and
// sign-out as icons; the page below it in a centred column of limited width (web-shell "Pages on large screens": 60rem is
// half of a Full HD window and, with the D13 root size, half of every larger one).
import { ref } from 'vue'
import { CircleUser, LogOut, Settings } from '@lucide/vue'
import { useRouter } from 'vue-router'
import PreferencesModal from '@/components/PreferencesModal.vue'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const session = useSession()
const router = useRouter()
// the settings modal and the tab it opens on: undefined is closed
const prefs = ref<'theme' | 'profile' | undefined>(undefined)

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
      <button
        type="button"
        class="btn-ghost"
        :aria-label="t('Your profile: {name}', { name: session.me?.name ?? '' })"
        :title="t('Your profile')"
        @click="prefs = 'profile'"
      >
        <CircleUser class="size-4" aria-hidden="true" />
        <span data-user-name>{{ session.me?.name }}</span>
      </button>
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Settings')"
        :title="t('Settings')"
        @click="prefs = 'theme'"
      >
        <Settings class="size-4" aria-hidden="true" />
      </button>
      <button type="button" class="icon-btn" :aria-label="t('Sign out')" :title="t('Sign out')" @click="signOut">
        <LogOut class="size-4" aria-hidden="true" />
      </button>
    </header>
    <main class="content-main">
      <div class="mx-auto w-full max-w-[60rem]" data-page-column>
        <RouterView />
      </div>
    </main>
    <PreferencesModal v-if="prefs" :initial="prefs" @close="prefs = undefined" />
  </div>
</template>
