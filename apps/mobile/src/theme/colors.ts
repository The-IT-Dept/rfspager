// RFS Professional Color System
// Primary brand: Venetian Red (#F7241D) - used sparingly for key actions and branding

const brandColors = {
  // Primary brand color - use sparingly for key actions
  primary: "#F7241D", // RFS Venetian Red
  primaryLight: "#FF4C3B", // Light red for hover/active states
  primaryDark: "#D01E16", // Dark red for pressed states

  // Secondary brand color
  secondary: "#018282", // Teal accent
  secondaryLight: "#20A0A0", // Light teal
  secondaryDark: "#015656", // Dark teal
}

const uiColors = {
  // Interactive colors (professional, not overpowering)
  interactive: "#007AFF", // iOS blue for secondary actions
  interactiveLight: "#5AA1FF", // Light blue for hover
  interactiveDark: "#0056CC", // Dark blue for pressed

  // Status colors
  success: "#34C759", // Green for success states
  warning: "#FF9500", // Orange for warnings
  error: "#FF3B30", // Red for errors
  info: "#5AC8FA", // Light blue for info
}

const neutralColors = {
  // Base colors
  white: "#FFFFFF",
  black: "#000000",

  // Background hierarchy
  backgroundPrimary: "#FFFFFF", // Main background
  backgroundSecondary: "#F8F9FA", // Secondary areas
  backgroundTertiary: "#F2F2F7", // iOS light gray background

  // Text hierarchy
  textPrimary: "#000000", // Main text
  textSecondary: "#666666", // Secondary text
  textTertiary: "#8E8E93", // Tertiary text (iOS gray)
  textDisabled: "#C7C7CC", // Disabled text

  // Border and dividers
  border: "#E5E5EA", // Primary borders (iOS)
  borderSecondary: "#C6C6C8", // Secondary borders
  borderActive: "#F7241D", // Active/focused borders (brand)

  // Card and surface colors
  cardBackground: "#FFFFFF",
  cardBorder: "#E5E5EA",
  cardShadow: "rgba(0, 0, 0, 0.1)",

  // Overlay colors
  overlay: "rgba(0, 0, 0, 0.4)",
  overlayLight: "rgba(0, 0, 0, 0.2)",
}

// Legacy colors for backwards compatibility
const legacyColors = {
  venetianRed: "#F7241D",
  grey5: "#0c0a0a0d",
  grey10: "rgba(18, 16, 16, 0.1)",
  grey20: "#12101033",
  grey40: "#12101066",
  grey50: "#12101080",
  grey70: "#121010b3",
  grey300: "#D1D5DB",
  grey700: "#374151",
  lightgrey: "#D9D9D9",
  offgrey: "#CFCFCF",
}

const colors = {
  ...brandColors,
  ...uiColors,
  ...neutralColors,
  ...legacyColors,
}

export { colors }
