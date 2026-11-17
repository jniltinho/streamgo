// Portions adapted from MeetHub (MIT): frontend/src/stores/session.ts
// The signed-in user (GET /me) and their preferences: the theme and the language follow the user to every browser.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import type { Me, MePatch } from '@/api/types'
import { isLanguage, setLanguage } from '@/i18n'
import { themeCatalog } from '@/themes/catalog'
import { applyTheme, forgetTheme } from '@/themes/apply'

export const useSession = defineStore('session', () => {
  const me = ref<Me | null>(null)
  const loggedIn = computed(() => me.value !== null)

  function accept(m: Me): Me {
    me.value = m
    const theme = themeCatalog.find((t) => t.id === m.theme)
    if (theme) applyTheme(theme.id, theme.scheme)
    if (isLanguage(m.language)) setLanguage(m.language)
    return m
  }

  // loadMe restores an existing session (page reload); false when not signed in.
  async function loadMe(): Promise<boolean> {
    try {
      accept(await api<Me>('GET', '/me'))
      return true
    } catch {
      return false
    }
  }

  async function login(email: string, password: string): Promise<Me> {
    await api('POST', '/auth/login', { email, password })
    return accept(await api<Me>('GET', '/me'))
  }

  async function updateMe(patch: MePatch): Promise<Me> {
    return accept(await api<Me>('PATCH', '/me', patch))
  }

  // clear forgets the session locally (after sign-out or when the server ended it).
  function clear(): void {
    me.value = null
    forgetTheme()
  }

  // logout clears the browser only once the server revoked the session (or says it already ended); on any other
  // failure the session may still be live, so the error is raised and nothing is forgotten locally.
  async function logout(): Promise<void> {
    try {
      await api('POST', '/auth/logout')
    } catch (e) {
      if (!(e instanceof ApiError && e.status === 401)) throw e
    }
    clear()
  }

  return { me, loggedIn, loadMe, login, updateMe, logout, clear }
})
