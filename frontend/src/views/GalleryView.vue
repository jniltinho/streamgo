<!-- Portions adapted from MeetHub (MIT): frontend/src/views/GalleryView.vue -->
<script setup lang="ts">
// Development/test-only page (absent from production builds): buttons, fields, cards, tabs, every toast, alert and
// dialog kind, the studio marks and the theme picker, for the visual, contrast and radius checks of each theme.
import UiAlert from '@/components/ui/UiAlert.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import { askConfirm, confirmTyped, showAlert } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'

function allToasts() {
  toast.success('Saved successfully.')
  toast.info('A new version is available.')
  toast.warning('Your session ends in 5 minutes.')
  toast.error('The server could not be reached.')
}
</script>

<template>
  <main class="content-main" data-gallery>
    <h1 class="page-title">Feedback gallery</h1>
    <div class="mb-3 flex flex-wrap gap-2">
      <button type="button" class="btn" data-open="toasts" @click="allToasts">Toasts</button>
      <button type="button" class="btn-ghost" data-open="alert" @click="showAlert('Maintenance tonight at 22:00.')">
        Alert dialog
      </button>
      <button
        type="button"
        class="btn-ghost"
        data-open="confirm"
        @click="askConfirm('Delete the broadcast Weekly Product Live?', { danger: true, ok: 'Delete' })"
      >
        Confirm dialog
      </button>
      <button
        type="button"
        class="btn-danger"
        data-open="typed"
        @click="confirmTyped('This cannot be undone.', 'ana@example.com')"
      >
        Typed confirm
      </button>
    </div>
    <div class="grid max-w-xl gap-2">
      <UiAlert kind="info" title="Information">The studio opens in the next version.</UiAlert>
      <UiAlert kind="success" title="Success">Destination saved.</UiAlert>
      <UiAlert kind="warning" title="Attention" action="Review">Two destinations have no key.</UiAlert>
      <UiAlert kind="error" title="Error" dismissible>Invalid e-mail or password.</UiAlert>
    </div>
    <form
      class="mt-4 grid max-w-xs gap-1"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity()"
    >
      <label class="lbl" for="g-email">E-mail</label>
      <input id="g-email" class="inp" type="email" required />
      <label class="lbl mt-2" for="g-layout">Layout</label>
      <select id="g-layout" class="inp">
        <option>Solo</option>
        <option>Side by side</option>
      </select>
      <label class="mt-2 flex items-center gap-2"><input type="checkbox" checked /> Record</label>
      <button type="submit" class="btn mt-2" data-open="validate">Validate</button>
    </form>

    <!-- cards and tabs -->
    <div class="mt-4 grid max-w-xl gap-3" data-gallery-cards>
      <section class="card">
        <h2 class="card-h">Banners</h2>
        <div class="cardtabs">
          <button type="button" class="cardtab on">Banners</button>
          <button type="button" class="cardtab">Brand</button>
          <button type="button" class="cardtab">Destinations</button>
        </div>
        <div class="p-3">
          <div class="subtabs">
            <button type="button" class="subtab on">Lower third</button>
            <button type="button" class="subtab">Ticker</button>
          </div>
          <p class="hint">Lower thirds shown over the program.</p>
          <a class="act" href="#gallery">Show</a>
          <span class="tag bg-strong-fill text-on-strong-fill">Showing</span>
        </div>
      </section>
    </div>

    <!-- the studio's roles: the live mark, the stage backdrop, on-stage and backstage marks -->
    <div class="mt-4 grid max-w-xl gap-2" data-gallery-studio>
      <div class="card flex items-center gap-3 p-2">
        <span class="border border-on-air px-2 font-semibold text-fg"
          ><span class="status-dot mr-1 inline-block size-2 bg-on-air" aria-hidden="true" />LIVE</span
        >
        <span class="text-on-stage"
          ><span class="status-dot mr-1 inline-block size-2 bg-on-stage" aria-hidden="true" />On stage</span
        >
        <span class="text-backstage"
          ><span class="status-dot mr-1 inline-block size-2 bg-backstage" aria-hidden="true" />Backstage</span
        >
      </div>
      <div class="stage grid h-24 place-items-center bg-stage-frame text-on-page">The stage backdrop</div>
    </div>

    <div class="mt-4 max-w-3xl" data-gallery-picker>
      <ThemePicker />
    </div>
  </main>
</template>
