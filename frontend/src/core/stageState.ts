// The stage state (design D1): the only shape clients parse, from the room metadata and from the API's answers.
// Unknown fields are ignored; a state with a lower `v` than the last one seen is dropped.

export const LAYOUTS = ['solo', 'grid', 'spotlight', 'sidebar', 'screen', 'pip'] as const
export type Layout = (typeof LAYOUTS)[number]
export type SourceKind = 'camera' | 'screen'
/** the styles of the name tags (design N1); the first is the default */
export const NAME_STYLES = ['bar', 'solid', 'strip', 'text', 'two-line'] as const
export type NameStyle = (typeof NAME_STYLES)[number]
/** a second line holds at most this many characters (Unicode code points, trimmed), as the server counts them */
export const MAX_TAGLINE = 60
export const taglineFits = (text: string) => [...text.trim()].length <= MAX_TAGLINE

export interface StageSource {
  /** participant identity: host-<user id> or guest-<guest id> */
  p: string
  k: SourceKind
}

export interface StageState {
  v: number
  on_air: boolean
  layout: Layout
  /** ordered: the first is the featured one */
  sources: StageSource[]
  banner: { id: string; text: string } | null
  logo: string | null
  accent: string
  /** the name tags are taken off the tiles; false when the server does not say */
  hide_names: boolean
  /** "bar" when the server does not say or names an unknown style */
  name_style: NameStyle
  /** the name tags are taken off the tiles of shared screens */
  hide_screen_names: boolean
  /** a participant's identity → the second line the owner wrote for them */
  taglines: Record<string, string>
}

const isObj = (x: unknown): x is Record<string, unknown> => typeof x === 'object' && x !== null && !Array.isArray(x)

// parseStage reads a stage state from the room metadata (a JSON string) or an API answer (an object). It returns
// null when the input is not a valid stage state.
export function parseStage(raw: unknown): StageState | null {
  if (typeof raw === 'string') {
    try {
      raw = JSON.parse(raw)
    } catch {
      return null
    }
  }
  if (!isObj(raw)) return null
  const { v, on_air, layout, sources, banner, logo, accent } = raw
  if (typeof v !== 'number' || !Number.isInteger(v) || v < 0) return null
  if (typeof on_air !== 'boolean' || typeof accent !== 'string') return null
  if (typeof layout !== 'string' || !(LAYOUTS as readonly string[]).includes(layout)) return null
  if (logo !== null && typeof logo !== 'string') return null
  if (!Array.isArray(sources)) return null
  const parsed: StageSource[] = []
  for (const s of sources) {
    if (!isObj(s) || typeof s.p !== 'string' || (s.k !== 'camera' && s.k !== 'screen')) return null
    parsed.push({ p: s.p, k: s.k })
  }
  let b: StageState['banner'] = null
  if (banner !== null) {
    if (!isObj(banner) || typeof banner.id !== 'string' || typeof banner.text !== 'string') return null
    b = { id: banner.id, text: banner.text }
  }
  return {
    v,
    on_air,
    layout: layout as Layout,
    sources: parsed,
    banner: b,
    logo,
    accent,
    hide_names: raw.hide_names === true,
    name_style: (NAME_STYLES as readonly unknown[]).includes(raw.name_style) ? (raw.name_style as NameStyle) : 'bar',
    hide_screen_names: raw.hide_screen_names === true,
    taglines: isObj(raw.taglines)
      ? Object.fromEntries(Object.entries(raw.taglines).filter((e): e is [string, string] => typeof e[1] === 'string'))
      : {},
  }
}

// StageFeed keeps the newest stage state seen from any channel (room metadata, API answers).
export class StageFeed {
  current: StageState | null = null

  /** accept parses raw and keeps it unless it is invalid or older than the current one; it returns whether it did */
  accept(raw: unknown): boolean {
    const next = parseStage(raw)
    if (!next || (this.current && next.v < this.current.v)) return false
    this.current = next
    return true
  }
}
