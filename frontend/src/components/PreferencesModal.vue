<script setup lang="ts">
// Theme and language in one modal, a tab each: the sign-in page and the signed-in header open it, so neither
// carries those controls itself.
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import { t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const tab = ref<'theme' | 'language'>('theme')
</script>

<template>
  <AppModal :title="t('Theme and language')" wide @close="emit('close')">
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
    </div>
    <div id="pref-panel" class="pnl-body" role="tabpanel" :aria-labelledby="`pref-tab-${tab}`">
      <ThemePicker v-if="tab === 'theme'" />
      <div v-else class="max-w-[20rem]">
        <label class="lbl" for="pref-lang">{{ t('Language') }}</label>
        <LanguageSelect id="pref-lang" />
      </div>
    </div>
  </AppModal>
</template>
