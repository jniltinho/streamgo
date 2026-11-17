<script setup lang="ts">
// The studio's regions (design/variant-d-square, D13): the header; the stage, which takes the space the others
// leave; one row with the layout picker at the left and the controls at the right; the backstage row; and the side
// panel. In a window of 19:10 or wider the backstage and the side panel share the right-hand column (.studio-right)
// instead. The page never scrolls. `host`: under 1280x720 the regions give way to a request for a larger window.
import { onBeforeUnmount, onMounted, ref, useSlots } from 'vue'
import { t } from '@/i18n'

defineProps<{ host?: boolean }>()
const slots = useSlots()

// --stage-w: the width the stage needs to show a 16:9 program at its current height. The wide-window arrangement
// gives the stage's column exactly that, so no empty band is left beside the program.
const body = ref<HTMLElement | null>(null)
let observer: ResizeObserver | undefined
onMounted(() => {
  const stage = body.value?.querySelector<HTMLElement>('.studio-stage')
  if (!stage) return
  observer = new ResizeObserver(() => {
    const s = getComputedStyle(stage)
    const frameY = parseFloat(s.paddingTop) + parseFloat(s.paddingBottom)
    const frameX = stage.offsetWidth - stage.clientWidth + parseFloat(s.paddingLeft) + parseFloat(s.paddingRight)
    body.value?.style.setProperty('--stage-w', `${Math.ceil(((stage.clientHeight - frameY) * 16) / 9 + frameX)}px`)
  })
  observer.observe(stage)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div class="studio" :class="{ host }">
    <header class="studio-head"><slot name="header" /></header>
    <main ref="body" class="studio-body" :class="{ 'no-side': !slots.side }">
      <slot name="stage" />
      <div class="studio-row">
        <slot name="picker"><span /></slot>
        <slot name="dock" />
      </div>
      <div class="studio-right">
        <section class="studio-back" :aria-label="t('Backstage')"><slot name="backstage" /></section>
        <slot name="side" />
      </div>
    </main>
    <div v-if="host" class="studio-small" role="alert">
      <p class="hint">{{ t('Hosting needs a window of at least 1280 by 720 pixels. Make the window larger.') }}</p>
    </div>
  </div>
</template>
