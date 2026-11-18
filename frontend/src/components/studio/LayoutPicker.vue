<script setup lang="ts">
// The six layouts as a radio group; each thumbnail is drawn by the layout function itself.
import { ref } from 'vue'
import UiTooltip from '@/components/ui/UiTooltip.vue'
import { layoutRects } from '@/core/layout'
import { LAYOUTS, type Layout } from '@/core/stageState'
import { studioError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const group = ref<HTMLElement | null>(null)

const name = (l: Layout) =>
  ({
    solo: t('Solo'),
    grid: t('Grid'),
    spotlight: t('Spotlight'),
    sidebar: t('Sidebar'),
    screen: t('Screen'),
    pip: t('Picture in picture'),
  })[l]
const sample = [{ k: 'camera' }, { k: 'camera' }, { k: 'camera' }, { k: 'camera' }] as const
const thumbs = Object.fromEntries(
  LAYOUTS.map((l) => [l, layoutRects(l, l === 'solo' ? sample.slice(0, 1) : [...sample], 160, 90).map((p) => p.rect)]),
) as Record<Layout, { x: number; y: number; w: number; h: number }[]>

const current = () => studio.stage?.layout ?? 'grid'

async function pick(l: Layout) {
  if (l === current()) return
  try {
    await studio.setLayout(l)
  } catch (e) {
    toast.error(studioError(e))
  }
}

// arrow keys move and select, as in any radio group
function key(e: KeyboardEvent, i: number) {
  const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key]
  if (!step) return
  e.preventDefault()
  const next = LAYOUTS[(i + step + LAYOUTS.length) % LAYOUTS.length]!
  group.value?.querySelector<HTMLElement>(`[data-layout=${next}]`)?.focus()
  void pick(next)
}
</script>

<template>
  <div class="card studio-box">
    <span id="layout-label" class="px-2 text-xs">{{ t('Layout') }}</span>
    <div ref="group" class="flex gap-1" role="radiogroup" aria-labelledby="layout-label">
      <UiTooltip v-for="(l, i) in LAYOUTS" :key="l" :text="name(l)">
        <template #default="{ trigger }">
          <button
            v-bind="trigger"
            type="button"
            class="studio-layout"
            role="radio"
            :aria-checked="current() === l"
            :tabindex="current() === l ? 0 : -1"
            :aria-label="name(l)"
            :data-layout="l"
            @click="pick(l)"
            @keydown="key($event, i)"
          >
            <span class="studio-thumb" aria-hidden="true">
              <span
                v-for="(r, j) in thumbs[l]"
                :key="j"
                :style="{
                  left: `${(r.x / 160) * 100}%`,
                  top: `${(r.y / 90) * 100}%`,
                  width: `${(r.w / 160) * 100}%`,
                  height: `${(r.h / 90) * 100}%`,
                }"
              />
            </span>
          </button>
        </template>
      </UiTooltip>
    </div>
  </div>
</template>
