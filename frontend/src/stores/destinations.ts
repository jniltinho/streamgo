// The user's destinations (GET /destinations). The address and the key are write-only: the API returns the scheme,
// the host and whether a key is set, and a PATCH without them keeps the stored ones.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'
import type { Destination, DestinationCreate, DestinationList, DestinationPatch } from '@/api/types'

export const useDestinations = defineStore('destinations', () => {
  const items = ref<Destination[]>([])
  const loaded = ref(false)

  async function load(): Promise<void> {
    items.value = (await api<DestinationList>('GET', '/destinations')).destinations
    loaded.value = true
  }

  async function create(body: DestinationCreate): Promise<Destination> {
    const d = await api<Destination>('POST', '/destinations', body)
    items.value.push(d)
    return d
  }

  async function update(id: string, patch: DestinationPatch): Promise<Destination> {
    const d = await api<Destination>('PATCH', `/destinations/${id}`, patch)
    items.value = items.value.map((x) => (x.id === id ? d : x))
    return d
  }

  async function remove(id: string): Promise<void> {
    await api('DELETE', `/destinations/${id}`)
    items.value = items.value.filter((x) => x.id !== id)
  }

  return { items, loaded, load, create, update, remove }
})
