<script setup lang="ts">
// Theme and language in one modal, a tab each: the sign-in page and the signed-in header open it, so neither
// carries those controls itself. Signed in it is "Settings", with a third tab for the name shown on the user's
// name tag.
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ initial?: 'theme' | 'language' | 'profile' }>()
const emit = defineEmits<{ close: [] }>()
const session = useSession()
const tab = ref<'theme' | 'language' | 'profile'>(props.initial ?? 'theme')
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
    </div>
    <div id="pref-panel" class="pnl-body" role="tabpanel" :aria-labelledby="`pref-tab-${tab}`">
      <ThemePicker v-if="tab === 'theme'" />
      <div v-else-if="tab === 'language'" class="max-w-[20rem]">
        <label class="lbl" for="pref-lang">{{ t('Language') }}</label>
        <LanguageSelect id="pref-lang" />
      </div>
      <form
        v-else
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
    </div>
  </AppModal>
</template>
