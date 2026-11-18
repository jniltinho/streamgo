<script setup lang="ts">
// A themed menu of choices behind an arrow button (design U3 of add-studio-interactions), on UI SDK's Menu: the
// items are radio items, `value` is the checked one, and choosing one emits `select` and closes the menu (focus
// returns to the arrow). With `disabled` (the reason) the arrow stays focusable, opens nothing and its tooltip says
// why. Attributes given to the component go to the arrow.
import { Check, ChevronUp } from '@lucide/vue'
import { Menu, MenuContent, MenuItemIndicator, MenuRadioGroup, MenuRadioItem, MenuTrigger } from '@ui-sdk/vue/menu'
import UiTooltip from '@/components/ui/UiTooltip.vue'

defineOptions({ inheritAttrs: false })
defineProps<{ label: string; items: { value: string; label: string }[]; value: string; disabled?: string }>()
const emit = defineEmits<{ select: [value: string]; open: [] }>()
</script>

<template>
  <Menu @open-change="(open: boolean) => open && emit('open')">
    <UiTooltip :text="disabled || label" :describes="!!disabled">
      <template #default="{ trigger }">
        <MenuTrigger
          v-bind="{ ...trigger, ...$attrs }"
          class="btn-ghost ui-menu-trigger"
          :aria-label="label"
          :disabled="!!disabled"
          focusable-when-disabled
        >
          <ChevronUp class="size-4" aria-hidden="true" />
        </MenuTrigger>
      </template>
    </UiTooltip>
    <MenuContent class="ui-menu" side="top" align="start" :side-offset="4" :collision-padding="8" :aria-label="label">
      <MenuRadioGroup :value="value" @value-change="(v: string) => emit('select', v)">
        <MenuRadioItem v-for="i in items" :key="i.value" :value="i.value" close-on-select class="ui-menu-item">
          <MenuItemIndicator class="ui-menu-check" keep-mounted
            ><Check class="size-3.5" aria-hidden="true"
          /></MenuItemIndicator>
          <span class="ui-menu-text">{{ i.label }}</span>
        </MenuRadioItem>
      </MenuRadioGroup>
    </MenuContent>
  </Menu>
</template>
