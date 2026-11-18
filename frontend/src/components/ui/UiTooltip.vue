<script setup lang="ts">
// A themed tooltip that names an icon-only control (design U5 of add-studio-interactions), on UI SDK's Tooltip. The
// control stays the caller's element: the default slot receives `trigger`, the attributes and listeners to bind on
// it. Shown 500 ms after the pointer rests on it and at once on keyboard focus; Escape, blur and leaving hide it.
// The text names the control, or with `describes` adds a description to the name the control has (a reason).
import { Tooltip, TooltipContent, TooltipTrigger } from '@ui-sdk/vue/tooltip'

defineProps<{ text: string; describes?: boolean; side?: 'top' | 'bottom' | 'left' | 'right' }>()
defineSlots<{ default(props: { trigger: Record<string, unknown> }): unknown }>()
</script>

<template>
  <Tooltip :delay="500" :type="describes ? 'description' : 'label'">
    <TooltipTrigger>
      <template #render="{ props }">
        <slot :trigger="props" />
      </template>
    </TooltipTrigger>
    <TooltipContent class="ui-tooltip" :side="side ?? 'top'" :side-offset="6" :collision-padding="8">
      {{ text }}
    </TooltipContent>
  </Tooltip>
</template>
