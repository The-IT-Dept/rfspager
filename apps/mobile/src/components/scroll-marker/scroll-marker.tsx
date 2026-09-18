import type { ReactNode } from "react"
import { Platform, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native"
import { ScrollViewMarker } from "react-native-screens/experimental"

export interface ScrollMarkerProps {
  /** Required: ScrollViewMarker types children as NonNullable<ReactNode>. */
  children: NonNullable<ReactNode>
  style?: StyleProp<ViewStyle>
}

/**
 * Tells iOS which scroll view drives the chrome.
 *
 * Two behaviours depend on UIKit knowing the screen's "content scroll view":
 * the tab bar minimising on scroll, and the navigation bar swapping to its
 * scrolled (Liquid Glass) appearance instead of an opaque one.
 *
 * react-native-screens resolves that scroll view two different ways, and only
 * one of them has a fallback:
 *
 *   - The stack/header path (RNSScreen.mm) walks
 *     `findScrollViewInFirstDescendantChainFrom`, which follows `subviews[0]`
 *     strictly. Any sibling rendered before the list — a filter row, a
 *     conditional empty state — breaks the chain and the search returns nil.
 *   - The tabs path (RNSTabsScreenComponentView) has no search at all. Its
 *     `_contentScrollView` is only ever set by
 *     `registerDescendantScrollView:fromMarker:`, so without a marker the tab
 *     bar can never minimise.
 *
 * Wrapping the scroll view here registers it explicitly for both, which is why
 * it fixes the opaque header and the static tab bar together.
 *
 * Android has neither behaviour, so it renders a plain View there.
 */
export function ScrollMarker({ children, style }: ScrollMarkerProps) {
  if (Platform.OS !== "ios") {
    return <View style={[styles.fill, style]}>{children}</View>
  }
  return <ScrollViewMarker style={[styles.fill, style]}>{children}</ScrollViewMarker>
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
})
