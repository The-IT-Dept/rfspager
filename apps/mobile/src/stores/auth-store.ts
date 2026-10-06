/**
 * Auth Store — Zustand store for authentication state.
 * Persisted to AsyncStorage. Holds token, current user, and auth actions.
 */
import { create } from "zustand"
import { persist, createJSONStorage } from "zustand/middleware"
import AsyncStorage from "@react-native-async-storage/async-storage"
import {
  api,
  apiBaseUrl,
  setApiToken,
  setOnUnauthorized,
  type UpdateProfileInput,
  type User,
} from "../services/api-client"
import { setIAPAuthToken, setOnSubscriptionSynced } from "../services/purchase-service"
import { getInstallationId, getInstallationInput } from "../utils/installation"
import { logEvent, setUserEmail, setUserId } from "../utils/analytics"
import { captureSession, sessionIsCurrent } from "./session-guard"

interface AuthState {
  token: string | null
  user: User | null
  lastUserRefresh: number
  logoutMessage: string

  // Actions
  login: (email: string, password: string) => Promise<string>
  register: (name: string, email: string, password: string, passwordConfirmation: string) => Promise<string>
  socialLogin: (
    provider: "apple" | "facebook",
    token: string,
    extras?: { name?: string; email?: string },
  ) => Promise<string>
  forgotPassword: (email: string) => Promise<string>
  updateProfile: (input: UpdateProfileInput) => Promise<string>
  updatePassword: (current: string, next: string, nextConfirmation: string) => Promise<string>
  deleteAccount: (password: string) => Promise<string>
  logout: (message?: string) => void
  resetApp: () => void
  refreshUser: () => Promise<void>
  ensureFreshUser: () => Promise<void>
  upsertInstallation: () => Promise<void>
  deleteInstallation: () => Promise<void>
  markNotificationsRead: () => Promise<boolean>
  clearLogoutMessage: () => void

  // Hydration
  _hasHydrated: boolean
  _setHasHydrated: (v: boolean) => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => {
      // Wire up token on rehydration
      const syncToken = (token: string | null) => {
        setApiToken(token)
        setIAPAuthToken(token)
      }

      // Capture the old credentials before clearing local state. Background
      // requests must not pick up another account's token after a quick login.
      const revokeRemoteSession = () => {
        const { token, user } = get()
        if (!token || !user) return
        const headers = { Authorization: `Bearer ${token}` }
        void (async () => {
          try {
            const id = await getInstallationId(user.user_id)
            await fetch(`${apiBaseUrl()}/api/v2/mobile/installations/${id}`, { method: "DELETE", headers })
          } finally {
            await fetch(`${apiBaseUrl()}/api/v2/mobile/auth/logout`, { method: "POST", headers })
          }
        })().catch(() => {})
      }

      return {
        token: null,
        user: null,
        lastUserRefresh: 0,
        logoutMessage: "",
        _hasHydrated: false,

        _setHasHydrated: (v) => set({ _hasHydrated: v }),

        clearLogoutMessage: () => set({ logoutMessage: "" }),

        login: async (email, password) => {
          try {
            const result = await api.auth.login(email, password)
            syncToken(result.token)
            set({ token: result.token })

            const user = await api.user.me()
            set({ user, lastUserRefresh: Date.now() })

            await get().upsertInstallation()
            logEvent("login")
            setUserId(user.user_id)
            setUserEmail(user.email)
            return "OK"
          } catch (e: any) {
            if (e?.message?.includes("invalid credentials")) return "Invalid email address or password"
            if (e?.message?.includes("Network") || e?.message?.includes("fetch")) return "Network error. Please check your connection."
            return e?.message || "Login failed. Please try again."
          }
        },

        register: async (name, email, password, passwordConfirmation) => {
          try {
            const result = await api.auth.register(name, email, password, passwordConfirmation)
            syncToken(result.token)
            set({ token: result.token })

            const user = await api.user.me()
            set({ user, lastUserRefresh: Date.now() })

            await get().upsertInstallation()
            logEvent("register")
            setUserId(user.user_id)
            setUserEmail(user.email)
            return "OK"
          } catch (e: any) {
            return e?.message || "Email already exists"
          }
        },

        socialLogin: async (provider, token, extras) => {
          try {
            const result = await api.auth.socialLogin(provider, token, extras)
            syncToken(result.token)
            set({ token: result.token })

            const user = await api.user.me()
            set({ user, lastUserRefresh: Date.now() })

            await get().upsertInstallation()
            logEvent("socialLogin")
            setUserId(user.user_id)
            setUserEmail(user.email)
            return "OK"
          } catch (e: any) {
            return e?.message || "Login failed"
          }
        },

        forgotPassword: async (email) => {
          try {
            logEvent("forgot_password")
            await api.auth.forgotPassword(email)
            return "OK"
          } catch (e: any) {
            return e?.message || "Failed"
          }
        },

        updateProfile: async (input) => {
          try {
            const updated = await api.user.update(input)
            // Merge into the cached user so downstream selectors (name/email
            // changes) reflect immediately without a full /me round-trip.
            const current = get().user
            set({
              user: current ? { ...current, ...updated } : updated,
              lastUserRefresh: Date.now(),
            })
            if (updated.email) setUserEmail(updated.email)
            logEvent("profile_updated")
            return "OK"
          } catch (e: any) {
            return e?.message || "Failed to update profile"
          }
        },

        updatePassword: async (current, next, nextConfirmation) => {
          try {
            await api.auth.updatePassword(current, next, nextConfirmation)
            logEvent("password_updated")
            return "OK"
          } catch (e: any) {
            return e?.message || "Failed to update password"
          }
        },

        deleteAccount: async (password) => {
          try {
            await api.user.deleteAccount(password)
            logEvent("account_deleted")
            // Tear down local session — same path as a normal logout, minus
            // the server installation-delete (the cascade already removed it).
            syncToken(null)
            set({ token: null, user: null, lastUserRefresh: 0, logoutMessage: "" })
            return "OK"
          } catch (e: any) {
            return e?.message || "Failed to delete account"
          }
        },

        logout: (message) => {
          revokeRemoteSession()
          logEvent("logout")
          syncToken(null)
          set({ token: null, user: null, lastUserRefresh: 0, logoutMessage: message || "" })
        },

        resetApp: () => {
          revokeRemoteSession()
          logEvent("app_reset")
          syncToken(null)
          set({ token: null, user: null, lastUserRefresh: 0, logoutMessage: "" })
        },

        refreshUser: async () => {
          try {
            const session = get()
            const currentUser = session.user
            const captured = captureSession(session.token, currentUser?.user_id)
            if (!captured) return
            const user = await api.user.me()
            const latest = get()
            if (!sessionIsCurrent(captured, { token: latest.token, userID: latest.user?.user_id })) return
            set({ user, lastUserRefresh: Date.now() })
          } catch (e) {
            console.error("Failed to refresh user:", e)
          }
        },

        ensureFreshUser: async () => {
          const CACHE_TTL = 5 * 60 * 1000
          const { user, lastUserRefresh } = get()
          if (!user || Date.now() - lastUserRefresh > CACHE_TTL) {
            await get().refreshUser()
          }
        },

        upsertInstallation: async () => {
          try {
            const session = get()
            const user = session.user
            const captured = captureSession(session.token, user?.user_id)
            if (!captured || !user) return
            const installationId = await getInstallationId(user.user_id)
            const input = await getInstallationInput()
            const latest = get()
            if (!sessionIsCurrent(captured, { token: latest.token, userID: latest.user?.user_id })) return
            await api.installations.upsert(installationId, input)
          } catch (e) {
            console.error("Failed to upsert installation:", e)
          }
        },

        deleteInstallation: async () => {
          try {
            const user = get().user
            if (!user) return
            const installationId = await getInstallationId(user.user_id)
            await api.installations.delete(installationId)
          } catch (e) {
            console.error("Failed to delete installation:", e)
          }
        },

        markNotificationsRead: async () => {
          try {
            await api.notifications.markRead()
            return true
          } catch {
            return false
          }
        },
      }
    },
    {
      name: "auth-store",
      storage: createJSONStorage(() => AsyncStorage),
      partialize: (state) => ({
        token: state.token,
        user: state.user,
        lastUserRefresh: state.lastUserRefresh,
      }),
      onRehydrateStorage: () => (state) => {
        if (state?.token) {
          setApiToken(state.token)
          setIAPAuthToken(state.token)
          setOnUnauthorized(() => state.logout("You have been logged out. Please log in again."))
          setOnSubscriptionSynced(() => state.refreshUser())
        }
        state?._setHasHydrated(true)
      },
    },
  ),
)

// Derived selectors
export const useUser = () => useAuthStore((s) => s.user)
export const useIsAuthenticated = () => useAuthStore((s) => !!s.token)
export const useValidSubscription = () =>
  useAuthStore((s) => {
    const v = s.user?.subscription_valid
    return v === true
  })
// Whether this account sees ads: only once the persisted user has loaded, so a
// subscriber never glimpses an ad slot while the store rehydrates.
export const useShowsAds = () =>
  useAuthStore((s) => s._hasHydrated && s.user != null && s.user.subscription_valid !== true)
export const useProductName = () =>
  useAuthStore((s) => {
    const pid = s.user?.subscription_product_id
    if (pid === "io.devlan.rfspager.monthly") return "Monthly"
    if (pid === "io.devlan.rfspager.yearly") return "Yearly"
    if (pid === "io.devlan.rfspager.gifted") return "Gifted"
    return "Not Subscribed"
  })
