<script setup lang="ts">
// The brand of the program: its accent colour, the name tags (shown or not, on shared screens or not, their style
// chosen from previews drawn by the program's own function) and the logo (PNG, JPEG or WebP up to 2 MB).
import { computed, ref, watchPostEffect } from 'vue'
import { ImageUp, Trash2 } from '@lucide/vue'
import UiRadioGroup from '@/components/ui/UiRadioGroup.vue'
import UiSwitch from '@/components/ui/UiSwitch.vue'
import { ACCENTS, drawNameTagPreview } from '@/core/compositor'
import { NAME_STYLES, type NameStyle } from '@/core/stageState'
import { studioError, useStudio } from '@/stores/studio'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const studio = useStudio()
const busy = ref(false)
const file = ref<HTMLInputElement | null>(null)
const accent = () => studio.stage?.accent ?? ''

const styleNames: Record<NameStyle, () => string> = {
  bar: () => t('Bar'),
  solid: () => t('Solid'),
  strip: () => t('Strip'),
  text: () => t('Text only'),
  'two-line': () => t('Two lines'),
}
const styles = computed(() => NAME_STYLES.map((value) => ({ value, label: styleNames[value]() })))
const previews = new Map<string, HTMLCanvasElement>()
watchPostEffect(() => {
  const host = studio.people.find((p) => p.host)
  const name = host?.name || t('Your name')
  const tagline = (host && studio.stage?.taglines[host.identity]) || t('Second line')
  for (const [style, c] of previews) drawNameTagPreview(c, name, tagline, accent(), style as NameStyle)
})

async function act(fn: () => Promise<void>) {
  busy.value = true
  try {
    await fn()
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}

async function upload() {
  const f = file.value?.files?.[0]
  if (!f) return
  if (f.size > 2 * 1024 * 1024) toast.error(t('The logo must be at most 2 MB.'))
  else await act(() => studio.uploadLogo(f))
  file.value!.value = ''
}
</script>

<template>
  <h2 class="text-sm font-semibold">{{ t('Brand') }}</h2>
  <p class="studio-meta mb-3">{{ t('Colour and logo of the program') }}</p>
  <h3 id="accent-label" class="lbl">{{ t('Accent colour') }}</h3>
  <div class="mb-4 flex flex-wrap gap-2" role="radiogroup" aria-labelledby="accent-label">
    <button
      v-for="c in ACCENTS"
      :key="c"
      type="button"
      class="studio-swatch"
      role="radio"
      :aria-checked="accent() === c"
      :aria-label="c"
      :style="{ background: c }"
      :disabled="busy"
      @click="act(() => studio.setAccent(c))"
    />
    <label class="studio-swatch relative grid place-items-center overflow-hidden" :title="t('Other colour')">
      <span class="sr-only">{{ t('Other colour') }}</span>
      <input
        type="color"
        class="absolute inset-0 size-full cursor-pointer opacity-0"
        :value="accent()"
        :disabled="busy"
        @change="act(() => studio.setAccent(($event.target as HTMLInputElement).value))"
      />
      <span aria-hidden="true">+</span>
    </label>
  </div>

  <h3 class="lbl">{{ t('Name tags') }}</h3>
  <div class="studio-item mb-4 grid gap-3">
    <UiSwitch
      :checked="!studio.stage?.hide_names"
      :label="t(`Show each person's name on the program`)"
      data-name-tags
      @change="(on) => act(() => studio.setNameTags(on))"
    />
    <div>
      <UiSwitch
        :checked="!studio.stage?.hide_screen_names"
        :label="t('Show names on shared screens')"
        :disabled="studio.stage?.hide_names ? t('The names are off on the program.') : undefined"
        data-screen-names
        @change="(on) => act(() => studio.setScreenNames(on))"
      />
    </div>
    <UiRadioGroup
      :label="t('Style of the name tags')"
      :items="styles"
      :value="studio.stage?.name_style ?? 'bar'"
      data-name-style
      @select="(s) => act(() => studio.setNameStyle(s as NameStyle))"
    >
      <template #item="{ value }">
        <canvas
          :ref="(el) => el && previews.set(value, el as HTMLCanvasElement)"
          width="128"
          height="72"
          class="aspect-video w-full"
          :data-preview="value"
        />
      </template>
    </UiRadioGroup>
  </div>

  <h3 class="lbl">{{ t('Logo') }}</h3>
  <div class="studio-item flex items-center gap-3">
    <img v-if="studio.stage?.logo" :src="studio.stage.logo" :alt="t('Logo')" class="size-10 object-contain" data-logo />
    <span v-else class="studio-meta flex-1">{{ t('No logo. It is shown in the top right corner.') }}</span>
    <span v-if="studio.stage?.logo" class="studio-meta flex-1">{{ t('Top right corner') }}</span>
    <label class="btn-ghost" :class="{ 'pointer-events-none opacity-50': busy }">
      <ImageUp class="size-4" aria-hidden="true" />{{ studio.stage?.logo ? t('Replace') : t('Upload') }}
      <input
        ref="file"
        type="file"
        class="sr-only"
        accept="image/png,image/jpeg,image/webp"
        :disabled="busy"
        data-logo-input
        @change="upload"
      />
    </label>
    <button
      v-if="studio.stage?.logo"
      type="button"
      class="icon-btn"
      :disabled="busy"
      :aria-label="t('Remove the logo')"
      @click="act(() => studio.deleteLogo())"
    >
      <Trash2 class="size-4" aria-hidden="true" />
    </button>
  </div>
</template>
