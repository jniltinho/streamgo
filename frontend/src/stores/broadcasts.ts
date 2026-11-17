// The user's broadcasts (GET /broadcasts, newest first) and the changes the broadcasts page makes to them.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Broadcast, BroadcastDetail, BroadcastList, BroadcastTitle } from '@/api/types'

export const useBroadcasts = defineStore('broadcasts', () => {
  const items = ref<Broadcast[]>([])
  const loaded = ref(false)

  async function load(): Promise<void> {
    items.value = (await api<BroadcastList>('GET', '/broadcasts')).broadcasts
    loaded.value = true
  }

  async function create(title: string): Promise<BroadcastDetail> {
    const b = await api<BroadcastDetail>('POST', '/broadcasts', { title } satisfies BroadcastTitle)
    items.value.unshift(b)
    return b
  }

  async function rename(id: string, title: string): Promise<void> {
    const b = await api<BroadcastDetail>('PATCH', `/broadcasts/${id}`, { title } satisfies BroadcastTitle)
    const it = items.value.find((x) => x.id === id)
    if (it) Object.assign(it, { title: b.title, output: b.output })
  }

  async function remove(id: string): Promise<void> {
    await api('DELETE', `/broadcasts/${id}`)
    items.value = items.value.filter((x) => x.id !== id)
  }

  return { items, loaded, load, create, rename, remove }
})
