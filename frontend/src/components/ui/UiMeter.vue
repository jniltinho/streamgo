<script setup lang="ts">
// A themed level meter (design U4 of add-studio-interactions), on UI SDK's Meter: role="meter" named by `label`,
// with `text` as the value a screen reader hears. `value` runs from 0 to 1.
import { Meter, MeterIndicator, MeterTrack } from '@ui-sdk/vue/meter'

defineProps<{ value: number; label: string; text: string }>()
</script>

<template>
  <Meter class="ui-meter" :value="value * 100" :aria-label="label" :get-value-label="() => text">
    <MeterTrack class="ui-meter-track">
      <MeterIndicator class="ui-meter-fill" />
    </MeterTrack>
  </Meter>
</template>
