/**
 * REST API Client for the Go backend
 * Replaces mst-gql / GraphQL
 */

const DEFAULT_BASE = "http://localhost:8080"

function getBaseUrl(): string {
  const env = process.env.EXPO_PUBLIC_API_URL
  if (env) {
    // Strip /graphql if present (old env format)
    return env.replace("/graphql", "")
  }
  return DEFAULT_BASE
}

/** Server origin (https://…). Exported so callers outside this module can
 *  build URLs against the same backend. */
export function apiBaseUrl(): string {
  return getBaseUrl()
}

const API_BASE = `${getBaseUrl()}/api/v2/mobile`

let _token: string | null = null
let _onUnauthorized: (() => void) | null = null

export function setApiToken(token: string | null) {
  _token = token
}

export function setOnUnauthorized(handler: () => void) {
  _onUnauthorized = handler
}

async function request<T = any>(method: string, path: string, body?: any): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "application/json",
  }
  if (_token) {
    headers["Authorization"] = `Bearer ${_token}`
  }

  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401) {
    _onUnauthorized?.()
    throw new Error("Unauthorized")
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || `Request failed: ${res.status}`)
  }

  // Handle 204 No Content
  if (res.status === 204) return {} as T

  return res.json()
}

const get = <T = any>(path: string) => request<T>("GET", path)
const post = <T = any>(path: string, body?: any) => request<T>("POST", path, body)
const put = <T = any>(path: string, body?: any) => request<T>("PUT", path, body)
const del = <T = any>(path: string) => request<T>("DELETE", path)

// --- Types ---

export interface User {
  id: number
  user_id: string
  name: string | null
  email: string
  subscription_valid: boolean
  subscription_product_id: string | null
  subscription_expiry: string | null
  will_renew: boolean | null
  subscription_platform: string | null
  notifications_count?: number
  unread_notifications_count?: number
  // False for accounts created by Apple/Facebook sign-in that never set a
  // password: they "set" one rather than "change" it. Only /me reports it.
  has_password?: boolean
}

export interface Message {
  am_i_responding?: boolean
  system_name?: string | null
  system_uuid?: string | null
  system_suburb?: string | null
  id: number
  system_id: number
  capcode_id: number | null
  address: string
  message: string
  lat: string | null
  lng: string | null
  created_at: string
  capcode_alias: string | null
  capcode_icon: string | null
  capcode_colour: string | null
  agency_name: string | null
  responders?: Responder[]
}

export interface Responder {
  id: number
  user_id: number
  name: string
  email: string
  hash?: string
}

export interface Capcode {
  id: number
  address: string
  alias: string
  icon: string
  colour: string
  agency_id: number | null
  agency_name: string | null
}

export interface Alertzone {
  id: number
  name: string
  lat: number
  lng: number
  radius: number
}

export interface MajorIncident {
  id: number
  title: string
  category: string
  geometry: any
  data: any
  created_at: string
  updated_at: string
}

export interface HazardReduction {
  id: number
  title: string
  leadAgency: string
  supportingAgencies: string
  lga: string
  size: number
  tenure: string
  geometry: any
  start_at: string
  end_at: string
  created_at: string
}

export interface Photo {
  id: number
  photo_id: string
  url: string
  thumb: string
  created_at: string
  user_id: number
}

export interface PaginatedResponse<T> {
  data: T[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

/**
 * Keyset-paginated response. The feed prepends live messages over SSE, so an
 * offset shifts under the reader and duplicates or skips rows; the next page is
 * addressed by the last row you already hold instead. Derive the cursor from
 * the final item's created_at + id.
 */
export interface CursorResponse<T> {
  data: T[]
  per_page: number
  has_more: boolean
}

export interface AuthResponse {
  token: string
  user: User
}

// --- Auth ---

export const auth = {
  login: (email: string, password: string) =>
    post<AuthResponse>("/auth/login", { email, password }),

  register: (name: string, email: string, password: string, password_confirmation: string) =>
    post<AuthResponse>("/auth/register", { name, email, password, password_confirmation }),

  socialLogin: (
    provider: "apple" | "facebook",
    token: string,
    extras?: { name?: string; email?: string },
  ) =>
    post<AuthResponse>("/auth/social-login", {
      provider,
      token,
      ...(extras?.name ? { name: extras.name } : {}),
      ...(extras?.email ? { email: extras.email } : {}),
    }),

  logout: () => post("/auth/logout"),

  forgotPassword: (email: string) =>
    post("/auth/forgot-password", { email }),

  resetPassword: (email: string, token: string, password: string, password_confirmation: string) =>
    post("/auth/reset-password", { email, token, password, password_confirmation }),

  // current_password is omitted when the account has no password yet. The
  // response carries a fresh token: the change signs out every other session.
  updatePassword: (current_password: string | undefined, password: string, password_confirmation: string) =>
    post<{ status: string; token: string }>("/auth/update-password", {
      ...(current_password ? { current_password } : {}),
      password,
      password_confirmation,
    }),
}

// --- User ---

export interface UpdateProfileInput {
  name?: string
  email?: string
  current_password?: string // required to change email when the account has a password
}

export const user = {
  me: () => get<User & { notifications_count: number; unread_notifications_count: number }>("/me"),
  magicLink: (route?: string) => {
    const q = new URLSearchParams()
    if (route) q.set("route", route)
    return get<{ magicLink: string }>(`/magic-link?${q}`)
  },
  // An email change returns a fresh token alongside the user.
  update: (input: UpdateProfileInput) => request<User & { token?: string }>("PATCH", "/me", input),
  deleteAccount: (password?: string) =>
    request<{ status: string }>("DELETE", "/me", { confirm: "DELETE", ...(password ? { password } : {}) }),
}

// --- Messages ---

export const messages = {
  list: (params: {
    before_created_at?: string
    before_id?: number
    per_page?: number
    show_only_filtered?: boolean
    show_unknown?: boolean
    capcode_id?: number
    search?: string
    created_after?: string
  } = {}) => {
    const q = new URLSearchParams()
    // Both halves of the cursor or neither — the server ignores a partial one.
    if (params.before_created_at && params.before_id != null) {
      q.set("before_created_at", params.before_created_at)
      q.set("before_id", String(params.before_id))
    }
    if (params.per_page) q.set("per_page", String(params.per_page))
    if (params.show_only_filtered) q.set("show_only_filtered", "true")
    if (params.show_unknown) q.set("show_unknown", "true")
    if (params.capcode_id) q.set("capcode_id", String(params.capcode_id))
    if (params.search) q.set("search", params.search)
    if (params.created_after) q.set("created_after", params.created_after)
    return get<CursorResponse<Message>>(`/messages?${q}`)
  },

  get: (id: number) => get<Message>(`/messages/${id}`),

  map: (params: { limit?: number; created_after?: string } = {}) => {
    const q = new URLSearchParams()
    if (params.limit) q.set("limit", String(params.limit))
    if (params.created_after) q.set("created_after", params.created_after)
    return get<Message[]>(`/messages/map?${q}`)
  },

  respond: (id: number) => post<{ responding: boolean }>(`/messages/${id}/respond`),
}

// --- Agencies ---

export interface Agency {
  id: number
  name: string
  capcode_count: number
}

export const agencies = {
  list: () => get<Agency[]>("/agencies"),
}

// --- Capcodes ---

export const capcodes = {
  list: (params: { page?: number; per_page?: number; search?: string; agency_id?: number } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set("page", String(params.page))
    if (params.per_page) q.set("per_page", String(params.per_page))
    if (params.search) q.set("search", params.search)
    if (params.agency_id) q.set("agency_id", String(params.agency_id))
    return get<PaginatedResponse<Capcode>>(`/capcodes?${q}`)
  },
}

// --- Capcode Suggestions ---

export interface CapcodeSuggestionInput {
  capcode_id?: number | null
  address: string
  alias?: string
  agency?: string
  icon?: string
  colour?: string
  note?: string
}

export interface MyCapcodeSuggestion {
  id: number
  capcode_id: number | null
  address: string
  alias: string | null
  agency: string | null
  icon: string | null
  colour: string | null
  note: string | null
  created_at: string
  current_alias: string | null
  current_agency: string | null
}

export interface UnknownCapcode {
  capcode_id: number | null
  address: string
  alias: string | null
  agency_name: string | null
  message_count: number
  last_seen_at: string
}

export const capcodeSuggestions = {
  create: (data: CapcodeSuggestionInput) => post<{ status: string; id: number }>("/capcode-suggestions", data),
  listMine: () => get<MyCapcodeSuggestion[]>("/capcode-suggestions/mine"),
}

/** Fetched from the public API (no auth) so we can show unknowns to anyone. */
export async function fetchUnknownCapcodes(days = 30, limit = 200): Promise<{ data: UnknownCapcode[] }> {
  const res = await fetch(`${getBaseUrl()}/api/public/capcodes/unknown?days=${days}&limit=${limit}`)
  if (!res.ok) throw new Error(`Failed to load unknown capcodes (HTTP ${res.status})`)
  return res.json()
}

export interface CapcodeMessageSample {
  id: number
  address: string
  message: string
  created_at: string
  system_name: string | null
}

/** Recent pages for a single capcode address — used as context in the
 *  suggest form so the user (and reviewer) can see what kind of unit
 *  this address pages. Public, no auth. */
export async function fetchCapcodeMessages(address: string, limit = 8): Promise<{ data: CapcodeMessageSample[] }> {
  const res = await fetch(
    `${getBaseUrl()}/api/public/capcodes/messages?address=${encodeURIComponent(address)}&limit=${limit}`,
  )
  if (!res.ok) throw new Error(`Failed to load capcode messages (HTTP ${res.status})`)
  return res.json()
}

// --- Subscriptions ---

export const subscriptions = {
  list: () => get<number[]>("/subscriptions"),
  create: (capcode_id: number) => post("/subscriptions", { capcode_id }),
  delete: (capcode_id: number) => del(`/subscriptions/${capcode_id}`),
}

// --- Filters ---

export const filters = {
  list: () => get<number[]>("/filters"),
  create: (capcode_id: number) => post("/filters", { capcode_id }),
  delete: (capcode_id: number) => del(`/filters/${capcode_id}`),
}

// --- Alertzones ---

export const alertzones = {
  list: () => get<Alertzone[]>("/alertzones"),
  create: (data: { name: string; lat: number; lng: number; radius: number }) =>
    post<Alertzone>("/alertzones", data),
  delete: (id: number) => del(`/alertzones/${id}`),
}

// --- Installations ---

export const installations = {
  upsert: (id: string, data: any) => put(`/installations/${id}`, data),
  delete: (id: string) => del(`/installations/${id}`),
}

// --- Notifications ---

export const notifications = {
  markRead: () => post("/notifications/mark-read"),
}

// --- Incidents ---

export const incidents = {
  majorIncidents: (params: { page?: number; per_page?: number } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set("page", String(params.page))
    if (params.per_page) q.set("per_page", String(params.per_page))
    return get<PaginatedResponse<MajorIncident>>(`/major-incidents?${q}`)
  },

  majorIncident: (id: number | string) => get<MajorIncident>(`/major-incidents/${id}`),

  hazardReductions: (params: { page?: number; per_page?: number } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set("page", String(params.page))
    if (params.per_page) q.set("per_page", String(params.per_page))
    return get<PaginatedResponse<HazardReduction>>(`/hazard-reductions?${q}`)
  },
}

// --- Systems ---

export interface System {
  uuid: string
  name: string
  suburb: string | null
  is_owner?: boolean
  is_pending?: boolean
  // Receiver websocket is up and pinging. Missing from servers before 5.9.1.
  online?: boolean
  // Later of the last decoded message and the last receiver ping.
  last_message_at: string | null
  last_page_at?: string | null
}

export interface MySystem {
  uuid: string
  name: string
  suburb: string | null
  request_notes: string | null
  api_key: string | null
  approved_at: string | null
  created_at: string
  is_pending: boolean
  last_message_at: string | null
  install_command?: string
}

/** "own": has a Pi, dongle and antenna. "needs": would host one if we help with the gear. */
export type HostHardware = "own" | "needs"

export interface CreateSystemInput {
  name: string
  suburb: string
  notes?: string
  hardware?: HostHardware
  accepted_terms: boolean
}

export interface SystemDetail {
  id: number
  system_id: string
  name: string
  suburb: string | null
  frequency: number
  lat: number | null
  lng: number | null
  is_owner: boolean
  is_pending: boolean
  online: boolean
  connected_at: string | null
  last_ping_at: string | null
  last_message_at: string | null
  // Owner-only fields. Undefined for non-owners.
  api_key?: string | null
  created_at?: string | null
  approved_at?: string | null
  request_notes?: string | null
  install_command?: string | null
  receiver?: {
    pod_id?: string | null
    version?: string | null
    os?: string | null
    arch?: string | null
    hostname?: string | null
    dongles?: Array<{
      index: number
      name?: string
      manufacturer?: string
      product?: string
      serial?: string
      tuner?: string
    }> | null
  }
}

export interface SystemEvent {
  id: number
  type: string
  severity: "info" | "warning" | "error"
  message: string
  metadata: Record<string, any>
  created_at: string
}

export const systems = {
  list: () => get<System[]>(`/systems`),
  mine: () => get<MySystem[]>(`/my-systems`),
  create: (body: CreateSystemInput) => post<MySystem>(`/systems`, body),
  get: (uuid: string) => get<SystemDetail>(`/systems/${uuid}`),
  events: (uuid: string, limit = 200) =>
    get<{ data: SystemEvent[] }>(`/systems/${uuid}/events?limit=${limit}`),
  recentLogs: (uuid: string, limit = 200) =>
    get<{ data: SystemLogLine[] }>(`/systems/${uuid}/logs/recent?limit=${limit}`),
}

export interface SystemLogLine {
  ts: number
  level: string
  source: string | null
  message: string
  raw: Record<string, any> | null
}

/** SSE URL for the owner-only log stream. Build the full URL ourselves
 *  because EventSource on RN takes a path; the token rides as a query
 *  string parameter since RN's EventSource doesn't support custom headers. */
export function systemLogStreamURL(uuid: string): string {
  return `${getBaseUrl()}/api/v2/mobile/systems/${uuid}/logs`
}

// --- Photos ---

export const photos = {
  list: (params: { page?: number; per_page?: number; message_id?: number; capcode_id?: number } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set("page", String(params.page))
    if (params.per_page) q.set("per_page", String(params.per_page))
    if (params.message_id) q.set("message_id", String(params.message_id))
    if (params.capcode_id) q.set("capcode_id", String(params.capcode_id))
    return get<Photo[]>(`/photos?${q}`)
  },
}

// --- Bundled export ---

export const api = {
  auth,
  user,
  messages,
  agencies,
  capcodes,
  capcodeSuggestions,
  subscriptions,
  filters,
  alertzones,
  installations,
  notifications,
  incidents,
  photos,
  systems,
}
