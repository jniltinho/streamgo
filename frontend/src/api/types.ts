// Shapes of the REST contract (openspec/changes/add-live-studio/design.md, "REST contract"). Later packages add the
// shapes of their own routes here.

/** every error response: { "error": { "code", "message" } } */
export interface ErrorEnvelope {
  error: { code: string; message: string }
}

/** GET /me, and the answer of POST /auth/login and PATCH /me */
export interface Me {
  id: string
  name: string
  email: string
  /** a theme id of the registry; '' when the user never chose one */
  theme: string
  /** 'en' or 'pt-BR'; '' when the user never chose one */
  language: string
}

/** PATCH /me */
export type MePatch = Partial<Pick<Me, 'theme' | 'language'>>

/** one entry of GET /themes */
export interface ThemeInfo {
  id: string
  label: string
  scheme: 'light' | 'dark'
  default?: boolean
}

/** GET /themes */
export interface ThemeList {
  themes: ThemeInfo[]
}
