<script setup lang="ts">
// A muted live picture of one source outside the stage: what a camera or a shared screen shows before the owner
// puts it on.
import { ref, watchEffect } from 'vue'

const props = defineProps<{ track: MediaStreamTrack }>()
const video = ref<HTMLVideoElement | null>(null)

watchEffect(() => {
  if (video.value) video.value.srcObject = new MediaStream([props.track])
})
</script>

<template>
  <video ref="video" class="studio-card-video" autoplay muted playsinline aria-hidden="true" data-preview />
</template>
