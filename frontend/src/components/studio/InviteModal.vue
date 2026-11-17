<script setup lang="ts">
// The invite link: copy it, or rotate it (the old link stops working).
import { ref } from 'vue'
import { Copy, RefreshCw } from '@lucide/vue'
import AppModal from '@/components/AppModal.vue'
import { studioError, useStudio } from '@/stores/studio'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const studio = useStudio()
const busy = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(studio.detail?.invite_url ?? '')
    toast.success(t('Invite link copied.'))
  } catch {
    toast.error(t('The link could not be copied. Select it and copy it yourself.'))
  }
}

async function rotate() {
  const ok = await askConfirm(t('Create a new invite link? The current link stops working.'), {
    ok: t('New link'),
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    await studio.rotateInvite()
    toast.success(t('A new invite link was created.'))
  } catch (e) {
    toast.error(studioError(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppModal :title="t('Invite guests')" @close="emit('close')">
    <div class="pnl-body">
      <label class="lbl" for="invite-url">{{ t('Invite link') }}</label>
      <input
        id="invite-url"
        class="inp mb-2"
        readonly
        :value="studio.detail?.invite_url"
        data-invite-url
        @focus="($event.target as HTMLInputElement).select()"
      />
      <p class="hint">{{ t('Anyone with this link can join the studio backstage, without an account.') }}</p>
    </div>
    <div class="pnl-foot">
      <button type="button" class="btn" @click="copy">
        <Copy class="size-4" aria-hidden="true" /> {{ t('Copy link') }}
      </button>
      <button type="button" class="btn-ghost" :disabled="busy" @click="rotate">
        <RefreshCw class="size-4" aria-hidden="true" /> {{ t('New link') }}
      </button>
      <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Close') }}</button>
    </div>
  </AppModal>
</template>
