<script setup lang="ts">
// A themed group of choices shown as cards (design N5 of add-name-tag-styles), on UI SDK's Radio: role="radiogroup"
// named by `label`, one tab stop, arrow keys move and choose. `value` is the chosen item and choosing one emits
// `select`. Each card holds the `item` slot (a picture, hidden from screen readers) above the item's label, which
// names it. Attributes given to the component go to the group.
import { Radio, RadioGroup } from '@ui-sdk/vue/radio'

defineOptions({ inheritAttrs: false })
defineProps<{ label: string; items: { value: string; label: string }[]; value: string }>()
const emit = defineEmits<{ select: [value: string] }>()
defineSlots<{ item(props: { value: string }): unknown }>()
</script>

<template>
  <RadioGroup
    v-bind="$attrs"
    class="ui-radio-group"
    :value="value"
    :aria-label="label"
    @value-change="(v: string | null) => v !== null && emit('select', v)"
  >
    <Radio v-for="i in items" :key="i.value" :value="i.value" class="ui-radio" :data-value="i.value">
      <span class="ui-radio-picture" aria-hidden="true"><slot name="item" :value="i.value" /></span>
      <span class="ui-radio-label">{{ i.label }}</span>
    </Radio>
  </RadioGroup>
</template>
