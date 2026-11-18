<script setup lang="ts">
// A person's second line (design N2 and N5 of add-name-tag-styles), for the owner on a backstage card: a popover with
// one field, Save and Remove. Saving it empty removes it too. The Two lines style shows it under the name.
import { computed, ref, useId } from 'vue'
import { IdCard } from '@lucide/vue'
import UiPopover from '@/components/ui/UiPopover.vue'
import { type Person, studioError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ person: Person }>()
const studio = useStudio()
const saved = computed(() => studio.stage?.taglines[props.person.identity] ?? '')
const text = ref('')
const busy = ref(false)
const field = useId()

async function save(value: string, close: () => void) {
  busy.value = true
  try {
    await studio.setTagline(props.person.identity, value)
    close()
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UiPopover
    :label="t('Second line: {name}', { name: person.name })"
    :title="t('Second line of {name}', { name: person.name })"
    data-tagline
    @open="text = saved"
  >
    <template #trigger><IdCard class="size-4" aria-hidden="true" /></template>
    <template #default="{ close }">
      <form class="grid gap-2" @submit.prevent="save(text, close)">
        <label class="lbl" :for="field">{{ t('Role or company') }}</label>
        <input
          :id="field"
          v-model="text"
          class="inp"
          maxlength="60"
          autocomplete="off"
          :aria-describedby="`${field}-hint`"
        />
        <p :id="`${field}-hint`" class="studio-meta">{{ t('Shown under the name with the Two lines style.') }}</p>
        <div class="flex justify-end gap-2">
          <button v-if="saved" type="button" class="btn-ghost" :disabled="busy" @click="save('', close)">
            {{ t('Remove') }}
          </button>
          <button type="submit" class="btn" :disabled="busy">{{ t('Save') }}</button>
        </div>
      </form>
    </template>
  </UiPopover>
</template>
