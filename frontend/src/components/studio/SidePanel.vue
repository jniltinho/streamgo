<script setup lang="ts">
// The side panel: the Banners, Brand and Destinations tabs (an ARIA tab list: arrow keys move between tabs).
import { ref } from 'vue'
import { Palette, Radio, RectangleHorizontal } from '@lucide/vue'
import BannersTab from '@/components/studio/BannersTab.vue'
import BrandTab from '@/components/studio/BrandTab.vue'
import DestinationsTab from '@/components/studio/DestinationsTab.vue'
import { t } from '@/i18n'

const tabs = [
  { id: 'banners', icon: RectangleHorizontal, label: () => t('Banners'), view: BannersTab },
  { id: 'brand', icon: Palette, label: () => t('Brand'), view: BrandTab },
  { id: 'destinations', icon: Radio, label: () => t('Destinations'), view: DestinationsTab },
] as const
const current = ref(0)
const list = ref<HTMLElement | null>(null)

function key(e: KeyboardEvent) {
  const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key]
  if (!step) return
  e.preventDefault()
  current.value = (current.value + step + tabs.length) % tabs.length
  list.value?.querySelectorAll<HTMLElement>('[role=tab]')[current.value]?.focus()
}
</script>

<template>
  <aside class="card studio-side" :aria-label="t('Studio settings')">
    <div ref="list" class="cardtabs fit" role="tablist" @keydown="key">
      <button
        v-for="(tab, i) in tabs"
        :id="`side-tab-${tab.id}`"
        :key="tab.id"
        type="button"
        class="cardtab flex items-center gap-1.5"
        :class="{ on: current === i }"
        role="tab"
        :aria-selected="current === i"
        :aria-controls="`side-panel-${tab.id}`"
        :tabindex="current === i ? 0 : -1"
        @click="current = i"
      >
        <component :is="tab.icon" class="size-4" aria-hidden="true" />{{ tab.label() }}
      </button>
    </div>
    <div
      :id="`side-panel-${tabs[current]!.id}`"
      class="studio-side-body"
      role="tabpanel"
      :aria-labelledby="`side-tab-${tabs[current]!.id}`"
      tabindex="0"
    >
      <component :is="tabs[current]!.view" />
    </div>
  </aside>
</template>
