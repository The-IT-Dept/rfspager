/**
 * Preferences Store — Zustand store for UI preferences.
 * Persisted to AsyncStorage. All the UI toggles/settings.
 */
import { create } from "zustand"
import { persist, createJSONStorage } from "zustand/middleware"
import AsyncStorage from "@react-native-async-storage/async-storage"

interface PreferencesState {
  onboarded: boolean
  darkMode: boolean
  drawerExpanded: boolean

  // Map
  clusterMapIcons: boolean
  mapPageSize: number
  mapShowHR: boolean
  mapShowUpdates: boolean

  // Pager
  pagerFilter: boolean
  pagerShowUnknown: boolean

  // Actions
  setOnboarded: (v: boolean) => void
  toggleDarkMode: () => void
  setDarkMode: (v: boolean) => void
  toggleDrawerExpanded: () => void
  setClusterMapIcons: (v: boolean) => void
  setMapPageSize: (v: number) => void
  setMapShowHR: (v: boolean) => void
  setMapShowUpdates: (v: boolean) => void
  setPagerFilter: (v: boolean) => void
  setPagerShowUnknown: (v: boolean) => void
}

export const usePreferencesStore = create<PreferencesState>()(
  persist(
    (set) => ({
      onboarded: false,
      darkMode: false,
      drawerExpanded: false,
      clusterMapIcons: true,
      mapPageSize: 100,
      mapShowHR: true,
      mapShowUpdates: true,
      pagerFilter: false,
      pagerShowUnknown: false,

      setOnboarded: (v) => set({ onboarded: v }),
      toggleDarkMode: () => set((s) => ({ darkMode: !s.darkMode })),
      setDarkMode: (v) => set({ darkMode: v }),
      toggleDrawerExpanded: () => set((s) => ({ drawerExpanded: !s.drawerExpanded })),
      setClusterMapIcons: (v) => set({ clusterMapIcons: v }),
      setMapPageSize: (v) => set({ mapPageSize: v }),
      setMapShowHR: (v) => set({ mapShowHR: v }),
      setMapShowUpdates: (v) => set({ mapShowUpdates: v }),
      setPagerFilter: (v) => set({ pagerFilter: v }),
      setPagerShowUnknown: (v) => set({ pagerShowUnknown: v }),
    }),
    {
      name: "preferences-store",
      storage: createJSONStorage(() => AsyncStorage),
    },
  ),
)
