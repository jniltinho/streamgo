<!-- Portions adapted from MeetHub (MIT): frontend/src/views/GalleryView.vue -->
<script setup lang="ts">
// Development/test-only page (absent from production builds): buttons, fields, cards, tabs, every toast, alert and
// dialog kind, the studio marks and the theme picker, for the visual, contrast and radius checks of each theme.
import { ref } from 'vue'
import { Mic, MicAudioLines, MicOff, Settings } from '@lucide/vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiMenu from '@/components/ui/UiMenu.vue'
import UiMeter from '@/components/ui/UiMeter.vue'
import UiPopover from '@/components/ui/UiPopover.vue'
import UiRadioGroup from '@/components/ui/UiRadioGroup.vue'
import UiSwitch from '@/components/ui/UiSwitch.vue'
import UiTooltip from '@/components/ui/UiTooltip.vue'
import ThemePicker from '@/components/ThemePicker.vue'
import { askConfirm, confirmTyped, showAlert } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'

// the UI SDK wrappers: a short menu, a long one (twelve long names), one with nothing to choose
const cameras = [
  { value: '', label: 'Default' },
  { value: 'front', label: 'Front' },
  { value: 'back', label: 'Back' },
]
const camera = ref('')
const mics = [
  { value: '', label: 'Default' },
  ...Array.from({ length: 12 }, (_, i) => ({
    value: `mic-${i + 1}`,
    label: `USB Audio Interface ${i + 1} with a very long product name (Analog Stereo Input, Channel ${i + 1})`,
  })),
]
const mic = ref('')
// the state motion (design U6): a dock icon that changes with its pressed state, the speaking mark, the live mark
const pressed = ref(true)
// the switches, the radio cards and the popover of add-name-tag-styles
const names = ref(true)
const screens = ref(true)
const tagStyles = [
  { value: 'bar', label: 'Bar' },
  { value: 'solid', label: 'Solid' },
  { value: 'strip', label: 'Strip' },
]
const tagStyle = ref('bar')
const tagline = ref('')
const draft = ref('')

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
    <!-- on a card, as every form of the app is: a field's error text is made for a surface, not for the page -->
    <form
      class="card mt-4 grid max-w-xs gap-1 p-3"
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

    <!-- the UI SDK wrappers (design U2 of add-studio-interactions) -->
    <div class="card mt-4 flex max-w-xl flex-wrap items-center gap-2 p-2" data-gallery-ui>
      <UiTooltip text="Device settings">
        <template #default="{ trigger }">
          <button v-bind="trigger" type="button" class="btn-ghost" aria-label="Device settings" data-gallery-tip>
            <Settings class="size-4" aria-hidden="true" />
          </button>
        </template>
      </UiTooltip>
      <span>Camera</span>
      <UiMenu
        label="Choose a camera"
        :items="cameras"
        :value="camera"
        data-gallery-menu="camera"
        @select="camera = $event"
      />
      <span>Microphone</span>
      <UiMenu label="Choose a microphone" :items="mics" :value="mic" data-gallery-menu="mic" @select="mic = $event" />
      <span>None</span>
      <UiMenu label="Choose a speaker" :items="[]" value="" disabled="No speaker found." data-gallery-menu="none" />
      <UiMeter :value="0.5" label="Microphone level" text="Heard" data-gallery-meter />
      <output data-gallery-chosen>{{ camera }}|{{ mic }}</output>
    </div>
    <!-- the wrappers of add-name-tag-styles (design N5): two switches (the second depends on the first), a radio
         group of cards and a popover with a small form -->
    <div class="card mt-4 grid max-w-xl gap-3 p-2" data-gallery-tags>
      <UiSwitch :checked="names" label="Show names" data-gallery-switch="names" @change="names = $event" />
      <div>
        <UiSwitch
          :checked="screens"
          label="Show names on screens"
          :disabled="names ? undefined : 'The names are off.'"
          data-gallery-switch="screens"
          @change="screens = $event"
        />
      </div>
      <UiRadioGroup label="Style" :items="tagStyles" :value="tagStyle" data-gallery-radio @select="tagStyle = $event">
        <template #item="{ value }"><span class="studio-preview block" :data-gallery-picture="value" /></template>
      </UiRadioGroup>
      <div class="flex items-center gap-2">
        <span>Second line</span>
        <UiPopover label="Edit the second line" title="Second line" data-gallery-popover>
          <template #trigger><Settings class="size-4" aria-hidden="true" /></template>
          <template #default="{ close }">
            <form class="grid gap-2" @submit.prevent="((tagline = draft), close())">
              <label class="lbl" for="gallery-tagline">Role or company</label>
              <input id="gallery-tagline" v-model="draft" class="inp" aria-describedby="gallery-tagline-hint" />
              <p id="gallery-tagline-hint" class="studio-meta">Shown under the name.</p>
              <div class="flex justify-end gap-2">
                <button type="button" class="btn-ghost" @click="close()">Cancel</button>
                <button type="submit" class="btn">Save</button>
              </div>
            </form>
          </template>
        </UiPopover>
      </div>
      <output data-gallery-tags-state>{{ names }}|{{ screens }}|{{ tagStyle }}|{{ tagline }}</output>
    </div>
    <div class="card mt-4 flex max-w-xl items-center gap-3 p-2" data-gallery-motion>
      <button type="button" class="btn-ghost" :aria-pressed="pressed" data-gallery-toggle @click="pressed = !pressed">
        <component :is="pressed ? Mic : MicOff" :key="String(pressed)" class="dock-icon size-4" aria-hidden="true" />
        Mic
      </button>
      <MicAudioLines class="speaking-mark size-4 text-ok" role="img" aria-label="Speaking" data-gallery-speaking />
      <span class="studio-chip on-air" data-gallery-live
        ><span class="studio-dot status-dot bg-on-air" aria-hidden="true" />Live</span
      >
    </div>

    <div class="mt-4 max-w-3xl" data-gallery-picker>
      <ThemePicker />
    </div>
  </main>
</template>
