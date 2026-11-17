// Portions adapted from MeetHub (MIT): frontend/src/main.ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { installNativeReplacements } from './feedback/native'
import { locale } from './i18n'

document.documentElement.lang = locale.value
installNativeReplacements()
createApp(App).use(createPinia()).use(router).mount('#app')
