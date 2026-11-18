<script setup lang="ts">
// A themed popover beside an icon button (design N5 of add-name-tag-styles), on UI SDK's Popover: the button is
// named by `label` (also its tooltip) and shows the `trigger` slot; the panel is a non-modal dialog titled by `title`
// with the default slot inside, which receives `close`. Escape, a press outside and focus leaving close it, and
// focus returns to the button. `open` is emitted when it opens. Attributes given to the component go to the button.
import { ref } from 'vue'
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '@ui-sdk/vue/popover'
import UiTooltip from '@/components/ui/UiTooltip.vue'

defineOptions({ inheritAttrs: false })
defineProps<{ label: string; title: string }>()
const emit = defineEmits<{ open: [] }>()
defineSlots<{ trigger(): unknown; default(props: { close: () => void }): unknown }>()
const open = ref(false)

function change(next: boolean) {
  open.value = next
  if (next) emit('open')
}
</script>

<template>
  <Popover :open="open" @open-change="change">
    <UiTooltip :text="label">
      <template #default="{ trigger }">
        <PopoverTrigger v-bind="{ ...trigger, ...$attrs }" class="icon-btn" :aria-label="label">
          <slot name="trigger" />
        </PopoverTrigger>
      </template>
    </UiTooltip>
    <PopoverContent class="ui-popover" side="bottom" align="end" :side-offset="6" :collision-padding="8">
      <PopoverTitle class="ui-popover-title">{{ title }}</PopoverTitle>
      <slot :close="() => (open = false)" />
    </PopoverContent>
  </Popover>
</template>
