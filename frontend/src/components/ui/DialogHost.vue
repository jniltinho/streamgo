<!-- Portions adapted from MeetHub (MIT): frontend/src/components/ui/DialogHost.vue -->
<script setup lang="ts">
// Renders the current queued dialog (design U10 of add-studio-interactions): a confirmation on UI SDK's Alert Dialog,
// an alert on its Dialog. Modal, focus kept inside, Escape is a cancel, a click outside does nothing, first focus on
// the safest action (Cancel, although OK comes first), focus returned to the opener once the queue is empty.
import { computed, ref, watch } from 'vue'
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from '@ui-sdk/vue/alert-dialog'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@ui-sdk/vue/dialog'
import { dialogState, settleDialog, typedMatches, type DialogRequest } from '@/feedback/dialogs'
import { t } from '@/i18n'

const alertParts = { Root: Dialog, Content: DialogContent, Title: DialogTitle, Desc: DialogDescription }
const confirmParts = {
  Root: AlertDialog,
  Content: AlertDialogContent,
  Title: AlertDialogTitle,
  Desc: AlertDialogDescription,
}

const safe = ref<HTMLButtonElement | null>(null)
const typedInput = ref<HTMLInputElement | null>(null)
const cur = computed(() => dialogState.current)
const parts = computed(() => (cur.value?.kind === 'alert' ? alertParts : confirmParts))
const okEnabled = computed(() => !!cur.value && (cur.value.kind !== 'typed' || typedMatches(cur.value)))
// each request mounts its own dialog, also when two in a row have the same text
const keys = new WeakMap<DialogRequest, number>()
let seq = 0
const keyOf = (r: DialogRequest) => keys.get(r) ?? (keys.set(r, ++seq), seq)
let opener: HTMLElement | null = null

watch(
  () => dialogState.current,
  (req, prev) => {
    if (req && !prev) opener = document.activeElement as HTMLElement | null
    if (req) return
    // the opener may have gone with a modal its parent closed meanwhile: then the modal still open takes the focus
    if (opener?.isConnected) opener.focus?.()
    else {
      const modals = document.querySelectorAll<HTMLElement>('[role=dialog], [role=alertdialog]')
      const top = modals[modals.length - 1]
      ;(top?.querySelector<HTMLElement>('input, select, textarea, button:not(.modal-x)') ?? top)?.focus()
    }
    opener = null
  },
  { flush: 'post' }, // after the dialog left the page, which is no longer inert
)

function answer(ok: boolean) {
  if (ok && !okEnabled.value) return
  settleDialog(ok)
}
</script>

<template>
  <component
    :is="parts.Root"
    v-if="cur"
    :key="keyOf(dialogState.current!)"
    :open="true"
    v-bind="cur.kind === 'alert' ? { closeOnOutsidePress: false } : {}"
    @open-change="(open: boolean) => open || answer(false)"
  >
    <component
      :is="parts.Content"
      class="pnl w-fit min-w-[min(22.5rem,92vw)] max-w-[min(40rem,92vw)]"
      :initial-focus="() => (cur?.kind === 'typed' ? typedInput : safe)"
      final-focus="none"
    >
      <form @submit.prevent="answer(true)">
        <div class="pnl-head flex items-center">
          <component :is="parts.Title">
            <template #render="{ props }">
              <span v-bind="props" class="pnl-title">{{ cur.title }}</span>
            </template>
          </component>
        </div>
        <div class="pnl-body">
          <component
            :is="parts.Desc"
            class="whitespace-pre-line"
            :class="{ 'mb-2 font-semibold text-err': cur.kind === 'typed' }"
          >
            {{ cur.message }}
          </component>
          <template v-if="cur.kind === 'typed'">
            <label class="mb-1 block" for="dlg-typed">
              {{ t('Type') }} <strong>{{ cur.expected }}</strong> {{ t('to confirm:') }}
            </label>
            <input id="dlg-typed" ref="typedInput" v-model="cur.value" class="inp w-full" autocomplete="off" />
          </template>
        </div>
        <div class="pnl-foot">
          <button
            type="submit"
            :class="cur.danger ? 'btn-danger' : 'btn'"
            :disabled="!okEnabled"
            :ref="cur.kind === 'alert' ? 'safe' : undefined"
          >
            {{ cur.ok }}
          </button>
          <button v-if="cur.kind !== 'alert'" ref="safe" type="button" class="btn-ghost" @click="answer(false)">
            {{ cur.cancel }}
          </button>
        </div>
      </form>
    </component>
  </component>
</template>
