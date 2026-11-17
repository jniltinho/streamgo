// The server settings an administrator changes in Settings, Server (GET and PUT /settings, add-server-settings).
// The server checks the role: anyone else gets 403.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { OutputSettings, ServerSettings, ServerSettingsPatch } from '@/api/types'

export const useServerSettings = defineStore('serverSettings', () => {
  const output = ref<OutputSettings | null>(null)

  async function load(): Promise<void> {
    output.value = (await api<ServerSettings>('GET', '/settings')).output
  }

  async function save(patch: ServerSettingsPatch): Promise<void> {
    output.value = (await api<ServerSettings>('PUT', '/settings', patch)).output
  }

  return { output, load, save }
})
