<script setup lang="ts">
// A themed on/off switch (design N5 of add-name-tag-styles), on UI SDK's Switch: role="switch" named by `label`, its
// state is `checked` and turning it emits `change`. With `disabled` (the reason) it cannot be changed and the reason
// is shown under it and describes it. Attributes given to the component go to the switch.
import { useId } from 'vue'
import { Switch, SwitchThumb } from '@ui-sdk/vue/switch'

defineOptions({ inheritAttrs: false })
defineProps<{ checked: boolean; label: string; disabled?: string }>()
const emit = defineEmits<{ change: [checked: boolean] }>()
const reason = useId()
</script>

<template>
  <label class="ui-switch-row">
    <span class="ui-switch-label">{{ label }}</span>
    <Switch
      v-bind="$attrs"
      class="ui-switch"
      :checked="checked"
      :disabled="!!disabled"
      :aria-describedby="disabled ? reason : undefined"
      @checked-change="(v: boolean) => emit('change', v)"
    >
      <SwitchThumb class="ui-switch-thumb" />
    </Switch>
  </label>
  <p v-if="disabled" :id="reason" class="ui-switch-reason">{{ disabled }}</p>
</template>
