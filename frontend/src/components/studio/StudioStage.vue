<script setup lang="ts">
// The stage region: the canvas the compositor draws from the stage state. For the owner it is the program itself
// (what is published); for a guest, the compositor's preview.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import LiveNotice from '@/components/studio/LiveNotice.vue'
import { useStudio } from '@/stores/studio'
import { t } from '@/i18n'

const studio = useStudio()
const canvas = ref<HTMLCanvasElement | null>(null)

onMounted(() => studio.attachCanvas(canvas.value!))
onBeforeUnmount(() => studio.detachCanvas())
</script>

<template>
  <div class="studio-stage stage">
    <canvas
      ref="canvas"
      class="studio-program stage-tile [grid-area:1/1]"
      role="img"
      :aria-label="studio.role === 'owner' ? t('Program') : t('Stage')"
      data-program
    />
    <LiveNotice v-if="studio.role === 'owner'" />
    <p
      v-if="studio.onStage.length === 0"
      class="card z-10 self-end px-3 py-2 text-sm [grid-area:1/1]"
      role="note"
      data-empty-stage
    >
      {{
        studio.role === 'owner'
          ? t('The stage is empty. Use "Add to stage" in the backstage to put a camera or a screen on it.')
          : t('Nothing is on stage yet.')
      }}
    </p>
  </div>
</template>
