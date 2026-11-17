<script setup lang="ts">
// The stage region: the canvas the compositor draws from the stage state. For the owner it is the program itself
// (what is published); for a guest, the compositor's preview.
import { onBeforeUnmount, onMounted, ref } from 'vue'
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
      class="studio-program stage-tile"
      role="img"
      :aria-label="studio.role === 'owner' ? t('Program') : t('Stage')"
      data-program
    />
  </div>
</template>
