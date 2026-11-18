// Shapes of the REST contract (openspec/changes/add-live-studio/design.md, "REST contract"). Later packages add the
// shapes of their own routes here.

/** every error response: { "error": { "code", "message", "field"? } } */
export interface ErrorEnvelope {
  error: { code: string; message: string; field?: string }
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
  /** an administrator manages the accounts on the Users page */
  admin: boolean
}

/** PATCH /me */
export type MePatch = Partial<Pick<Me, 'name' | 'theme' | 'language'>>

/** PUT /me/password (204); a wrong current password is 422 with the field "current" */
export interface PasswordChange {
  current: string
  password: string
}

/** an account in GET /users, POST /users and PATCH /users/:id (administrators only) */
export interface AdminUser {
  id: string
  name: string
  email: string
  admin: boolean
  disabled: boolean
}

/** GET /users */
export interface UserList {
  users: AdminUser[]
}

/** POST /users */
export interface UserCreate {
  email: string
  name: string
  password: string
  admin?: boolean
}

/** PATCH /users/:id: only the given fields change; a new password or disabling ends the user's sessions */
export interface UserPatch {
  name?: string
  admin?: boolean
  disabled?: boolean
  password?: string
}

/** the output settings of GET and PUT /settings (administrators; add-server-settings) */
export interface OutputSettings {
  resolution: string
  max_resolution: string
  /** 0: each resolution's own bitrate */
  video_bitrate_kbps: number
  /** every known resolution, lowest first */
  resolutions: string[]
  /** the fields the server's environment sets: the app cannot change them */
  locked: string[]
}

/** GET and PUT /settings */
export interface ServerSettings {
  output: OutputSettings
}

/** PUT /settings: only the given fields change */
export interface ServerSettingsPatch {
  output: Partial<Pick<OutputSettings, 'resolution' | 'max_resolution' | 'video_bitrate_kbps'>>
}

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

/** POST /auth/login */
export interface LoginRequest {
  email: string
  password: string
}

/** the output of a broadcast (design D5) */
export interface Output {
  state: 'offline' | 'starting' | 'live' | 'interrupted'
  reason: string
  /** RFC 3339 */
  started_at: string | null
  /** RFC 3339 */
  deadline: string | null
}

/** a broadcast in GET /broadcasts */
export interface Broadcast {
  id: string
  title: string
  output: Output
}

export interface Banner {
  id: string
  text: string
}

/** the stage state of design D1 */
export interface Stage {
  v: number
  on_air: boolean
  layout: string
  sources: { p: string; k: string }[]
  banner: Banner | null
  logo: string | null
  accent: string
}

/** a destination as the API returns it: the address and the key are write-only */
export interface Destination {
  id: string
  name: string
  scheme: string
  host: string
  has_key: boolean
}

export interface BroadcastDestination extends Destination {
  status: 'off' | 'connecting' | 'live' | 'retrying'
  error: string
}

/** the server settings the owner's studio builds on, read-only */
export interface StudioSettings {
  max_on_stage: number
  program_width: number
  program_height: number
  /** the transcoder's video bitrate: the WHIP sender's ceiling stays above it */
  video_bitrate_kbps: number
  /** the broadcast's output resolution, e.g. "1080p" */
  resolution: string
  /** the resolutions the owner may choose, lowest first (up to output.max_resolution) */
  resolutions: string[]
}

/** POST /broadcasts (201), GET and PATCH /broadcasts/:id */
export interface BroadcastDetail extends Broadcast {
  invite_url: string
  stage: Stage
  destinations: BroadcastDestination[]
  studio: StudioSettings
}

/** POST and DELETE /broadcasts/:id/live */
export interface LiveAnswer {
  output: Output
}

/** GET /broadcasts, newest first */
export interface BroadcastList {
  broadcasts: Broadcast[]
}

/** POST /broadcasts and PATCH /broadcasts/:id */
export interface BroadcastTitle {
  title: string
}

/** GET /destinations */
export interface DestinationList {
  destinations: Destination[]
}

/** POST /destinations */
export interface DestinationCreate {
  name: string
  address: string
  key?: string
}

/** PATCH /destinations/:id: an absent address or key keeps the stored one */
export type DestinationPatch = Partial<DestinationCreate>

/** POST /broadcasts/:id/studio-token: the LiveKit URL for browsers, an access token and the identity */
export interface Join {
  url: string
  token: string
  identity: string
}

/** POST /join/:token: a Join and the broadcast's title */
export interface GuestJoin extends Join {
  title: string
}

/** GET /join/:token */
export interface Invite {
  title: string
}

/** PATCH /broadcasts/:id/stage: any of these */
export interface StagePatch {
  layout?: string
  sources?: { p: string; k: string }[]
  banner_id?: string | null
  accent?: string
  hide_names?: boolean
  name_style?: string
  hide_screen_names?: boolean
  /** one person's second line; an empty text removes it */
  tagline?: { identity: string; text: string }
}

/** the answer of every route that changes the stage */
export interface StageAnswer {
  stage: Stage
}

/** GET /broadcasts/:id/banners */
export interface BannerList {
  banners: Banner[]
}

/** POST /broadcasts/:id/invite */
export interface InviteLink {
  invite_url: string
}

/** PUT /broadcasts/:id/destinations */
export interface BroadcastDestinationList {
  destinations: BroadcastDestination[]
}
