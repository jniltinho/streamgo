<!-- Portions adapted from MeetHub (MIT): frontend/src/components/chat/QuickThemes.vue -->
<script setup lang="ts">
// The theme picker (ui-themes spec): every theme of the catalog with a preview; a choice applies at once, is kept in
// this browser and, when signed in, saved in the user's preferences so it follows them to other browsers.
import { ref } from 'vue'
import { useSession } from '@/stores/session'
import { themeCatalog } from '@/themes/catalog'
import { applyTheme, currentTheme } from '@/themes/apply'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'
import { ApiError } from '@/api/client'

const session = useSession()
const active = ref(currentTheme())

async function choose(id: string) {
  const entry = themeCatalog.find((x) => x.id === id)
  applyTheme(id, entry?.scheme)
  active.value = id
  if (!session.loggedIn) return
  try {
    await session.updateMe({ theme: id })
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <div class="theme-grid" role="radiogroup" :aria-label="t('Theme')">
    <button
      v-for="th in themeCatalog"
      :key="th.id"
      type="button"
      role="radio"
      class="theme-tile"
      :class="{ on: active === th.id }"
      :aria-checked="active === th.id"
      :data-theme-id="th.id"
      @click="choose(th.id)"
    >
      <span class="theme-preview" aria-hidden="true">
        <span :style="{ background: th.swatch[3] }" />
        <span :style="{ background: th.swatch[1] }" />
        <span :style="{ background: th.swatch[0] }"><i :style="{ background: th.swatch[2] }" /></span>
      </span>
      <span class="theme-name">{{ th.label }}</span>
    </button>
  </div>
</template>
