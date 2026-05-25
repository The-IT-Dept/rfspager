/**
 * REST API Client for the Go backend
 * Replaces mst-gql / GraphQL
 */

const DEFAULT_BASE = "https://v3.rfspager.app"

function getBaseUrl(): string {
  const env = process.env.EXPO_PUBLIC_API_URL
  if (env) {
    // Strip /graphql if present (old env format)
    return env.replace("/graphql", "")
  }
  return DEFAULT_BASE
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
}

export interface Message {
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

  updatePassword: (current_password: string, password: string, password_confirmation: string) =>
    post("/auth/update-password", { current_password, password, password_confirmation }),
}

// --- User ---

export interface UpdateProfileInput {
  name?: string
  email?: string
  current_password?: string // required when email is being changed
}

export const user = {
  me: () => get<User & { notifications_count: number; unread_notifications_count: number }>("/me"),
  magicLink: (route?: string) => {
    const q = new URLSearchParams()
    if (route) q.set("route", route)
    return get<{ magicLink: string }>(`/magic-link?${q}`)
  },
  update: (input: UpdateProfileInput) => request<User>("PATCH", "/me", input),
  deleteAccount: (password: string) =>
    request<{ status: string }>("DELETE", "/me", { confirm: "DELETE", password }),
}

// --- Messages ---

export const messages = {
  list: (params: {
    page?: number
    per_page?: number
    show_only_filtered?: boolean
    show_unknown?: boolean
    capcode_id?: number
    search?: string
    created_after?: string
  } = {}) => {
    const q = new URLSearchParams()
    if (params.page) q.set("page", String(params.page))
    if (params.per_page) q.set("per_page", String(params.per_page))
    if (params.show_only_filtered) q.set("show_only_filtered", "true")
    if (params.show_unknown) q.set("show_unknown", "true")
    if (params.capcode_id) q.set("capcode_id", String(params.capcode_id))
    if (params.search) q.set("search", params.search)
    if (params.created_after) q.set("created_after", params.created_after)
    return get<PaginatedResponse<Message>>(`/messages?${q}`)
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
  last_message_at: string | null
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

export interface CreateSystemInput {
  name: string
  suburb: string
  notes?: string
  accepted_terms: boolean
}

export const systems = {
  list: () => get<System[]>(`/systems`),
  mine: () => get<MySystem[]>(`/my-systems`),
  create: (body: CreateSystemInput) => post<MySystem>(`/systems`, body),
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
  subscriptions,
  filters,
  alertzones,
  installations,
  notifications,
  incidents,
  photos,
  systems,
}
