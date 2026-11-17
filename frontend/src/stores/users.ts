// The accounts an administrator manages (GET /users). The server checks the role: anyone else gets 403.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { errorMessage, t } from '@/i18n'
import type { AdminUser, UserCreate, UserList, UserPatch } from '@/api/types'

export const useUsers = defineStore('users', () => {
  const items = ref<AdminUser[]>([])
  const loaded = ref(false)

  async function load(): Promise<void> {
    items.value = (await api<UserList>('GET', '/users')).users
    loaded.value = true
  }

  async function create(body: UserCreate): Promise<AdminUser> {
    const u = await api<AdminUser>('POST', '/users', body)
    items.value.push(u)
    return u
  }

  async function update(id: string, patch: UserPatch): Promise<AdminUser> {
    const u = await api<AdminUser>('PATCH', `/users/${id}`, patch)
    items.value = items.value.map((x) => (x.id === id ? u : x))
    return u
  }

  return { items, loaded, load, create, update }
})

// userError is the message for a failed request: a 409 on creation is a taken e-mail, on a change the last
// administrator (the page never offers an administrator to disable or demote themself).
export function userError(e: unknown, creating = false): string {
  if (e instanceof ApiError && e.code === 'conflict')
    return creating ? t('A user with this e-mail already exists.') : t('An install needs an enabled administrator.')
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}
