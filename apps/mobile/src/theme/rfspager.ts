import { usePreferencesStore } from "../stores"

type Colors = {
  // Brand colors
  primary: string
  accent: string

  // Base colors
  white: string
  black: string

  // Background hierarchy
  background: string
  backgroundSecondary: string
  backgroundTertiary: string
  surface: string

  // Text colors
  textPrimary: string
  textSecondary: string
  textTertiary: string
  textDisabled: string
  darkText: string
  text: string

  // Interactive colors
  interactive: string
  interactiveLight: string
  interactiveDark: string

  // Status colors
  success: string
  warning: string
  error: string
  info: string

  // Border and surface colors
  border: string
  borderSecondary: string
  borderActive: string
  cardBackground: string
  cardBorder: string

  // Shadow and overlay
  shadowColor: string
  overlay: string
  overlayLight: string

  // Legacy / convenience
  lightPink: string
  grey: string
  lightGrey: string
  extraLightGrey: string
  black10: string
  black20: string
  black30: string
  black40: string
  red: string
  red10: string
  red20: string
  red30: string
  red40: string
  red50: string
  white10: string
  white20: string
  grey100: string
  green: string
  blue10: string
  grey70: string
  grey10: string
  grey40: string
  lightBlack: string
  greyBorder: string
}

export type AppTheme = {
  dark: boolean
  colors: Colors
}

export const LightTheme: AppTheme = {
  dark: false,
  colors: {
    primary: "#F7241D",
    accent: "#018282",

    white: "#FFFFFF",
    black: "#000000",

    background: "#FFFFFF",
    backgroundSecondary: "#F8F9FA",
    backgroundTertiary: "#F2F2F7",
    surface: "#FFFFFF",

    textPrimary: "#000000",
    textSecondary: "#666666",
    textTertiary: "#8E8E93",
    textDisabled: "#C7C7CC",
    darkText: "#000000",
    text: "#000000",

    interactive: "#007AFF",
    interactiveLight: "#5AA1FF",
    interactiveDark: "#0056CC",

    success: "#34C759",
    warning: "#FF9500",
    error: "#FF3B30",
    info: "#5AC8FA",

    border: "#E5E5EA",
    borderSecondary: "#C6C6C8",
    borderActive: "#F7241D",
    cardBackground: "#FFFFFF",
    cardBorder: "#E5E5EA",

    shadowColor: "#000000",
    overlay: "rgba(0, 0, 0, 0.4)",
    overlayLight: "rgba(0, 0, 0, 0.2)",

    lightPink: "#fedad9",
    grey: "#8C8C8C",
    lightGrey: "#D1D5DB",
    extraLightGrey: "rgba(18, 16, 16, 0.05)",
    black10: "#12100680",
    black20: "#12100633",
    black30: "#1210061a",
    black40: "rgba(18, 16, 16, 0.7)",
    red: "#F7241D",
    red10: "#800000",
    red20: "rgba(128, 0, 0, 0.15)",
    red30: "#FF0000",
    red40: "#FF000050",
    red50: "rgba(255,0,0,0.2)",
    white10: "#fffdfd",
    white20: "#f3f3f3",
    grey100: "#4267b2",
    green: "green",
    blue10: "#2077E2",
    grey70: "#121010b3",
    grey10: "rgba(18, 16, 16, 0.1)",
    grey40: "#12101066",
    lightBlack: "#FFFFFF",
    greyBorder: "rgba(18, 16, 16, 0.1)",
  },
}

export const DarkTheme: AppTheme = {
  dark: true,
  colors: {
    primary: "#F7241D",
    accent: "#018282",

    white: "#FFFFFF",
    black: "#000000",

    background: "#000000",
    backgroundSecondary: "#1C1C1E",
    backgroundTertiary: "#2C2C2E",
    surface: "#1C1C1E",

    textPrimary: "#FFFFFF",
    textSecondary: "#ADADAD",
    textTertiary: "rgba(255, 255, 255, 0.6)",
    textDisabled: "rgba(255, 255, 255, 0.3)",
    darkText: "#FFFFFF",
    text: "#FFFFFF",

    interactive: "#0A84FF",
    interactiveLight: "#409CFF",
    interactiveDark: "#0056CC",

    success: "#30D158",
    warning: "#FF9F0A",
    error: "#FF453A",
    info: "#64D2FF",

    border: "rgba(255, 255, 255, 0.17)",
    borderSecondary: "rgba(255, 255, 255, 0.12)",
    borderActive: "#F7241D",
    cardBackground: "#1C1C1E",
    cardBorder: "rgba(255, 255, 255, 0.17)",

    shadowColor: "#000000",
    overlay: "rgba(0, 0, 0, 0.6)",
    overlayLight: "rgba(0, 0, 0, 0.4)",

    lightPink: "#fedad9",
    grey: "#8C8C8C",
    lightGrey: "#D1D5DB",
    extraLightGrey: "rgba(18, 16, 16, 0.05)",
    black10: "#12100680",
    black20: "#12100633",
    black30: "#1210061a",
    black40: "rgba(18, 16, 16, 0.7)",
    red: "#F7241D",
    red10: "#800000",
    red20: "rgba(128, 0, 0, 0.15)",
    red30: "#FF0000",
    red40: "#FF000050",
    red50: "rgba(255,0,0,0.2)",
    white10: "#fffdfd",
    white20: "#f3f3f3",
    grey100: "#4267b2",
    green: "green",
    blue10: "#2077E2",
    grey70: "rgba(255, 255, 255, 0.7)",
    grey10: "rgba(255, 255, 255, 0.1)",
    grey40: "rgba(255, 255, 255, 0.5)",
    lightBlack: "rgba(255, 255, 255, 0.08)",
    greyBorder: "rgba(255, 255, 255, 0.17)",
  },
}

export const useAppTheme = (): AppTheme => {
  const darkMode = usePreferencesStore((s) => s.darkMode)
  return darkMode ? DarkTheme : LightTheme
}
