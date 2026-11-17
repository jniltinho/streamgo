// The user's destinations (GET /destinations). The address and the key are write-only: the API returns the scheme,
// the host and whether a key is set, and a PATCH without them keeps the stored ones.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { errorMessage, t } from '@/i18n'
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

// destinationError is the message for a failed destination request: a 409 means a broadcast is using it.
export function destinationError(e: unknown): string {
  if (e instanceof ApiError && e.code === 'conflict') return t('This destination is in use by a broadcast.')
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}
