<script setup lang="ts">
// The saved banners: show one over the program (at most one at a time), hide it, delete, add.
import { ref } from 'vue'
import { Eye, EyeOff, Plus, Trash2 } from '@lucide/vue'
import type { Banner } from '@/api/types'
import { studioError, useStudio } from '@/stores/studio'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const text = ref('')
const busy = ref(false)
const shown = (b: Banner) => studio.stage?.banner?.id === b.id

async function act(fn: () => Promise<void>) {
  busy.value = true
  try {
    await fn()
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}

async function add() {
  const v = text.value.trim()
  if (!v) return
  await act(async () => {
    await studio.addBanner(v)
    text.value = ''
  })
}

async function remove(b: Banner) {
  if (await askConfirm(t('Delete the banner "{text}"?', { text: b.text }), { ok: t('Delete'), danger: true }))
    await act(() => studio.deleteBanner(b.id))
}
</script>

<template>
  <h2 class="text-sm font-semibold">{{ t('Banners') }}</h2>
  <p class="studio-meta mb-3">{{ t('Lower thirds shown over the program') }}</p>
  <ul class="mb-4 flex flex-col gap-2">
    <li v-for="b in studio.banners" :key="b.id" class="studio-item" :class="{ on: shown(b) }" :data-banner="b.text">
      <p class="mb-2 text-sm break-words">{{ b.text }}</p>
      <div class="flex items-center gap-2">
        <span v-if="shown(b)" class="flex flex-1 items-center gap-1.5 text-xs text-on-stage">
          <span class="studio-dot status-dot bg-on-stage" aria-hidden="true" />{{ t('Showing') }}
        </span>
        <span v-else class="studio-meta flex-1">{{ t('Lower third') }}</span>
        <button
          type="button"
          class="btn-ghost"
          :disabled="busy"
          :aria-label="shown(b) ? t('Hide {text}', { text: b.text }) : t('Show {text}', { text: b.text })"
          @click="act(() => studio.showBanner(shown(b) ? null : b.id))"
        >
          <component :is="shown(b) ? EyeOff : Eye" class="size-4" aria-hidden="true" />
          {{ shown(b) ? t('Hide') : t('Show') }}
        </button>
        <button
          type="button"
          class="icon-btn"
          :disabled="busy"
          :aria-label="t('Delete {text}', { text: b.text })"
          @click="remove(b)"
        >
          <Trash2 class="size-4" aria-hidden="true" />
        </button>
      </div>
    </li>
    <li v-if="!studio.banners.length" class="studio-meta">{{ t('No banners yet.') }}</li>
  </ul>
  <form class="border-t border-line pt-3" @submit.prevent="add">
    <label class="lbl" for="banner-text">{{ t('Add banner') }}</label>
    <div class="flex gap-2">
      <input
        id="banner-text"
        v-model="text"
        class="inp"
        maxlength="120"
        :placeholder="t('Type the banner text')"
        :disabled="studio.banners.length >= 50"
      />
      <button type="submit" class="btn" :disabled="busy || !text.trim() || studio.banners.length >= 50">
        <Plus class="size-4" aria-hidden="true" />{{ t('Add') }}
      </button>
    </div>
  </form>
</template>
