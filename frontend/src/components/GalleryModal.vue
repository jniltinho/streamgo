<script setup lang="ts">
// The gallery's modal (test page only): a field and the ways to open something over it, another modal, a
// confirmation or a toast, for the layered checks of design U10 of add-studio-interactions.
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'

const props = defineProps<{ level: number }>()
const emit = defineEmits<{ close: [] }>()
const upper = ref(false)
const answer = ref('')

async function ask() {
  answer.value = String(await askConfirm('Rotate the gallery link?', { danger: true, ok: 'Rotate' }))
}

// a confirmation whose modal is unmounted by its parent while it shows: the test closes it with galleryLeave() and
// reads the answer from galleryAnswer
function askOrphan() {
  const w = window as unknown as Record<string, unknown>
  w.galleryLeave = () => emit('close')
  w.galleryAnswer = askConfirm('Rotate the gallery link?', { danger: true, ok: 'Rotate' })
}
</script>

<template>
  <AppModal :title="`Gallery modal ${props.level}`" @close="emit('close')">
    <div class="pnl-body">
      <label class="lbl" :for="`gallery-field-${props.level}`">Name</label>
      <input :id="`gallery-field-${props.level}`" class="inp" />
      <output data-gallery-answer>{{ answer }}</output>
    </div>
    <div class="pnl-foot">
      <button v-if="props.level === 1" type="button" class="btn" data-open="upper" @click="upper = true">
        Another modal
      </button>
      <button type="button" class="btn-ghost" data-open="ask" @click="ask">Ask</button>
      <button v-if="props.level === 2" type="button" class="btn-ghost" data-open="orphan" @click="askOrphan">
        Ask, then go
      </button>
      <button type="button" class="btn-ghost" data-open="toast" @click="toast.info('Shown over the modal.')">
        Toast
      </button>
      <button type="button" class="btn-ghost" @click="emit('close')">Close</button>
    </div>
    <GalleryModal v-if="upper" :level="2" @close="upper = false" />
  </AppModal>
</template>
