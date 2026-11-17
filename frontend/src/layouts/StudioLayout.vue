<script setup lang="ts">
// The studio's regions (design/variant-d-square, D13): the header; the stage, which takes the space the others
// leave; one row with the layout picker at the left and the controls at the right; the backstage row; and the side
// panel. The page never scrolls. `host`: under 1280x720 the regions give way to a request for a larger window.
import { useSlots } from 'vue'
import { t } from '@/i18n'

defineProps<{ host?: boolean }>()
const slots = useSlots()
</script>

<template>
  <div class="studio" :class="{ host }">
    <header class="studio-head"><slot name="header" /></header>
    <main class="studio-body" :class="{ 'no-side': !slots.side }">
      <slot name="stage" />
      <div class="studio-row">
        <slot name="picker"><span /></slot>
        <slot name="dock" />
      </div>
      <section class="studio-back" :aria-label="t('Backstage')"><slot name="backstage" /></section>
      <slot name="side" />
    </main>
    <div v-if="host" class="studio-small" role="alert">
      <p class="hint">{{ t('Hosting needs a window of at least 1280 by 720 pixels. Make the window larger.') }}</p>
    </div>
  </div>
</template>
