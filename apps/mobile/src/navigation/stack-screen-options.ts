import { Platform } from "react-native"
import type { AppTheme } from "../theme"

/**
 * Screen options shared by every tab's stack.
 *
 * Each tab owns its own Stack so that pushing a detail screen keeps the native
 * tab bar in place and preserves that tab's back history. Extracting the
 * options keeps the four layouts from drifting apart.
 */
export const stackScreenOptions = (theme: AppTheme) =>
  ({
    headerTransparent: Platform.OS === "ios",
    headerStyle:
      Platform.OS === "ios"
        ? { backgroundColor: "transparent" }
        : { backgroundColor: theme.colors.background },
    headerTintColor: theme.colors.textPrimary,
    headerTitleStyle: { color: theme.colors.textPrimary },
    headerShadowVisible: false,
    headerLargeStyle: { backgroundColor: "transparent" },
    contentStyle: { backgroundColor: theme.colors.background },
    // iOS 26 Liquid Glass: scrollEdgeEffects.top renders the proper translucent
    // header that doesn't pick up the colour of content scrolling under it.
    // Combining this with headerBlurEffect stacks two effects (per
    // react-native-screens docs), so it is used alone.
    scrollEdgeEffects: { top: "soft" },
  }) as const
