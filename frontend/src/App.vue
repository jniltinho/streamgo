<!-- Portions adapted from MeetHub (MIT): frontend/src/App.vue -->
<script setup lang="ts">
// The toast stack and the dialog host are built on UI SDK: loaded right after the entry, not in it, so the first
// page paints without waiting for the library (design U8 and U10 of add-studio-interactions).
import { lazy } from '@/components/lazy'
import { dialogsUnavailable } from '@/feedback/dialogs'

const ToastStack = lazy(() => import('@/components/ui/ToastStack.vue'))
const DialogHost = lazy(() => import('@/components/ui/DialogHost.vue'), dialogsUnavailable)
</script>

<template>
  <RouterView />
  <ToastStack />
  <DialogHost />
</template>
