import { Platform } from "react-native"
import type { AppTheme } from "../theme"

/**
 * Screen options shared by every tab's stack.
 *
 * iOS 26 does not put glass on the *bar*; it puts glass on the *items*.
 * The system look is a transparent navigation bar with a scroll-edge blur
 * beneath it, and each button, menu and the search field drawn as its own
 * Liquid Glass capsule. react-native-screens renders those items natively
 * (UIBarButtonItem with shared-background grouping on iOS 26) when they are
 * declared through Stack.Toolbar / Stack.SearchBar, so the right recipe on the
 * regular Stack is:
 *
 *   - headerTransparent: no bar background at all — the content and the
 *     system scroll-edge effect show through, exactly like Mail or Notes.
 *   - no headerStyle background and no headerBlurEffect: either one installs
 *     an opaque or frosted UINavigationBarAppearance and defeats the look.
 *   - scrollEdgeEffects left at the `automatic` default: UIKit chooses the
 *     right edge treatment per screen; forcing `soft` everywhere earlier is
 *     what made the header read as a flat fade.
 *
 * An earlier revision moved these stacks to ExperimentalStack to get a glass
 * bar. That stack cannot host search bars or header items yet, which cost the
 * Pager its native search; it was solving the wrong half of the problem.
 */
export const stackScreenOptions = (theme: AppTheme) =>
  ({
    headerTintColor: theme.colors.textPrimary,
    headerShadowVisible: false,
    contentStyle: { backgroundColor: theme.colors.background },
    ...(Platform.OS === "ios"
      ? { headerTransparent: true }
      : { headerStyle: { backgroundColor: theme.colors.background } }),
  }) as const
