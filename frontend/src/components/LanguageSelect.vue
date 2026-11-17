<!-- Portions adapted from MeetHub (MIT): frontend/src/components/AuthFrame.vue -->
<script setup lang="ts">
// The language switch: applies at once (no reload), is kept in this browser and, when signed in, saved in the user's
// preferences (PATCH /me) so it follows them to other browsers.
import { ApiError } from '@/api/client'
import { toast } from '@/feedback/toasts'
import { errorMessage, isLanguage, languages, locale, setLanguage, t } from '@/i18n'
import { useSession } from '@/stores/session'

defineProps<{ id: string }>()
const session = useSession()

async function choose(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  if (!isLanguage(v)) return
  setLanguage(v)
  if (!session.loggedIn) return
  try {
    await session.updateMe({ language: v })
  } catch (err) {
    toast.error(err instanceof ApiError ? errorMessage(err.code, err.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <select :id="id" class="inp" :value="locale" @change="choose">
    <option v-for="l in languages" :key="l.id" :value="l.id" :lang="l.id">{{ l.label }}</option>
  </select>
</template>
