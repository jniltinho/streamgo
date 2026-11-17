// Portions adapted from MeetHub (MIT): frontend/src/i18n/index.ts
// Translations: English source strings in code, one catalog per language with the same keys (en.ts is the list of
// every string; the parity test keeps pt-BR equal to it). A missing entry falls back to the English text.
// Placeholders are written {name} and filled from the params object. "error.<code>" keys translate API error codes.
import { ref } from 'vue'
import en from './en'
import ptBR from './pt-BR'

export type Language = 'en' | 'pt-BR'
export const languages: { id: Language; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'pt-BR', label: 'Português (Brasil)' },
]

export type Catalog = Record<string, string>
export const catalogs: Record<Language, Catalog> = { en, 'pt-BR': ptBR }
const STORAGE_KEY = 'streamgo.language'

export const locale = ref<Language>(initialLanguage())

// the user's choice remembered in this browser, else the browser's language, else English; the signed-in user's
// saved preference replaces it once GET /me answers (stores/session.ts)
function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLanguage(saved)) return saved
  } catch {
    /* storage unavailable */
  }
  const browser = typeof navigator === 'undefined' ? [] : navigator.languages
  return browser.some((l) => l.toLowerCase().startsWith('pt')) ? 'pt-BR' : 'en'
}

export function isLanguage(v: unknown): v is Language {
  return v === 'en' || v === 'pt-BR'
}

// setLanguage applies a language now (no reload) and remembers it in this browser.
export function setLanguage(lang: Language): void {
  locale.value = lang
  document.documentElement.lang = lang
  try {
    localStorage.setItem(STORAGE_KEY, lang)
  } catch {
    /* storage unavailable */
  }
}

export function t(text: string, params?: Record<string, string | number>): string {
  let out = catalogs[locale.value][text] ?? text
  if (params) {
    for (const [k, v] of Object.entries(params)) out = out.replaceAll(`{${k}}`, String(v))
  }
  return out
}

// errorMessage translates an API error code, falling back to the server's message.
export function errorMessage(code: string, fallback: string): string {
  return catalogs[locale.value][`error.${code}`] ?? fallback
}
