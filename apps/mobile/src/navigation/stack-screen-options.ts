import { Platform } from "react-native"
import type { AppTheme } from "../theme"

/**
 * Screen options shared by every tab's stack.
 *
 * Each tab owns its own Stack so pushing a detail screen keeps the native tab
 * bar in place and preserves that tab's back history. Extracting the options
 * keeps the four layouts from drifting apart.
 *
 * On iOS the header is deliberately left almost entirely alone. UINavigationBar
 * renders Liquid Glass itself on iOS 26 and handles content scrolling beneath
 * it. An earlier version set `headerTransparent` with transparent backgrounds
 * on both `headerStyle` and `headerLargeStyle`, which removed the bar's
 * background — leaving nothing for the system to apply the material to, so all
 * that remained was the scroll-edge fade over bare content. That is what made
 * the headers look like imitation glass rather than the real thing.
 *
 * `scrollEdgeEffects` is likewise left at its `automatic` default: UIKit picks
 * the right treatment per screen, and forcing `soft` everywhere overrode that.
 * Note that `blurEffect` must not be combined with it — react-native-screens
 * warns the two stack into overlapping effects.
 */
export const stackScreenOptions = (theme: AppTheme) =>
  ({
    headerTintColor: theme.colors.textPrimary,
    headerShadowVisible: false,
    contentStyle: { backgroundColor: theme.colors.background },
    // Android has no Liquid Glass, so it still needs an explicit surface.
    ...(Platform.OS === "android"
      ? { headerStyle: { backgroundColor: theme.colors.background } }
      : null),
  }) as const
