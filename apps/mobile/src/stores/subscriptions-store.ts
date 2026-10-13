/**
 * Subscriptions Store — Zustand store for capcode subscriptions and filters.
 * These are the user's capcode alert subscriptions and pager view filters.
 */
import { create } from "zustand"
import { api } from "../services/api-client"

interface SubscriptionsState {
  subscriptions: string[]
  filters: string[]
  loaded: boolean

  load: () => Promise<void>
  reset: () => void
  subscribe: (capcodeId: string) => Promise<void>
  unsubscribe: (capcodeId: string) => Promise<void>
  addFilter: (capcodeId: string) => Promise<void>
  removeFilter: (capcodeId: string) => Promise<void>
}

// In-flight fetch, shared so screens mounting together trigger one request.
let loading: Promise<void> | null = null

export const useSubscriptionsStore = create<SubscriptionsState>()((set, get) => ({
  subscriptions: [],
  filters: [],
  loaded: false,

  load: () => {
    if (!loading) {
      loading = (async () => {
        try {
          const [subs, fils] = await Promise.all([api.subscriptions.list(), api.filters.list()])
          set({ subscriptions: subs.map(String), filters: fils.map(String), loaded: true })
        } catch (e) {
          console.error("Failed to load subscriptions/filters:", e)
        } finally {
          loading = null
        }
      })()
    }
    return loading
  },

  reset: () => set({ subscriptions: [], filters: [], loaded: false }),

  subscribe: async (capcodeId) => {
    set((s) => ({ subscriptions: [...s.subscriptions, capcodeId] }))
    await api.subscriptions.create(Number(capcodeId))
  },

  unsubscribe: async (capcodeId) => {
    set((s) => ({ subscriptions: s.subscriptions.filter((id) => id !== capcodeId) }))
    await api.subscriptions.delete(Number(capcodeId))
  },

  addFilter: async (capcodeId) => {
    set((s) => ({ filters: [...s.filters, capcodeId] }))
    await api.filters.create(Number(capcodeId))
  },

  removeFilter: async (capcodeId) => {
    set((s) => ({ filters: s.filters.filter((id) => id !== capcodeId) }))
    await api.filters.delete(Number(capcodeId))
  },
}))
