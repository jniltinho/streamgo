// Portions adapted from MeetHub (MIT): frontend/src/themes/apply.ts
// Applying themes at runtime, with the precedence of the ui-themes spec (the same as public/theme-boot.js):
// the signed-in user's preference > the theme remembered in this browser > streamgo-dark, or streamgo-light when the
// browser prefers a light scheme.
export const THEME_STORAGE_KEY = 'streamgo.theme'
export const DEFAULT_THEME = 'streamgo-dark'

export function browserDefault(): string {
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'streamgo-light' : DEFAULT_THEME
}

export function currentTheme(): string {
  return document.documentElement.getAttribute('data-theme') ?? browserDefault()
}

// applyTheme switches the theme now; remember=true keeps it in this browser for the next page load.
export function applyTheme(id: string, scheme?: 'light' | 'dark', remember = true): void {
  const root = document.documentElement
  root.setAttribute('data-theme', id)
  if (scheme) root.style.colorScheme = scheme
  if (!remember) return
  try {
    localStorage.setItem(THEME_STORAGE_KEY, id)
  } catch {
    /* storage unavailable */
  }
}

// forgetTheme runs on sign-out: a shared browser goes back to the default.
export function forgetTheme(): void {
  try {
    localStorage.removeItem(THEME_STORAGE_KEY)
  } catch {
    /* storage unavailable */
  }
  document.documentElement.removeAttribute('data-user-theme')
  document.documentElement.style.colorScheme = ''
  applyTheme(browserDefault(), undefined, false)
}
