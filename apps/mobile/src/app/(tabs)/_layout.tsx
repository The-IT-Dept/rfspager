import { useState } from "react"
import { Platform } from "react-native"
import { NativeTabs } from "expo-router/unstable-native-tabs"
import { isLiquidGlassAvailable } from "expo-glass-effect"
import { useAppTheme, type AppTheme } from "../../theme"
import { BannerAd } from "../../utils/admob"
import { useShowsAds } from "../../stores"

/**
 * Native bottom tabs.
 *
 * This replaces a JS drawer (react-native-drawer-layout). A drawer is an
 * Android pattern; on iOS it never picked up the system's blur, spring or
 * scroll behaviour because it was drawn in JS. NativeTabs renders the real
 * UITabBar, which brings iOS 26 Liquid Glass for free — including the tab bar
 * shrinking as content scrolls, which cannot be reproduced from JS.
 *
 * Four tabs, because iOS collapses anything past five into a dated "More"
 * table. Capcodes, Alert Zones and Systems live under Settings: they are
 * configuration you visit occasionally, not daily destinations.
 *
 * Each `name` is a route group, so the URLs are unchanged — "(pager)" and the
 * rest are invisible to hrefs.
 */
export default function TabsLayout() {
  const theme = useAppTheme()
  const showsAds = useShowsAds()
  // iOS draws the accessory's glass capsule whenever the accessory exists,
  // whatever it contains, so it must not be mounted unless an ad will fill it.
  const [adUnavailable, setAdUnavailable] = useState(false)

  return (
    <NativeTabs
      tintColor={theme.colors.primary}
      minimizeBehavior="onScrollDown"
      {...(Platform.OS === "android" ? androidTabBar(theme) : {})}
    >
      <NativeTabs.Trigger name="(pager)">
        <NativeTabs.Trigger.Icon sf="antenna.radiowaves.left.and.right" md="cell_tower" />
        <NativeTabs.Trigger.Label>Pager</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(incidents)">
        <NativeTabs.Trigger.Icon sf="flame" md="local_fire_department" />
        <NativeTabs.Trigger.Label>Incidents</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(map)">
        <NativeTabs.Trigger.Icon sf="map" md="map" />
        <NativeTabs.Trigger.Label>Map</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(settings)">
        <NativeTabs.Trigger.Icon sf="gearshape" md="settings" />
        <NativeTabs.Trigger.Label>Settings</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      {/* iOS 26 only: the accessory slot rides directly above the tab bar and
          minimises with it. Screens' own floating BannerAd returns null on
          iOS 26 so the ad is not shown twice. Subscribers, and anyone whose ad
          failed to load, get no accessory at all rather than an empty capsule. */}
      {Platform.OS === "ios" && isLiquidGlassAvailable() && showsAds && !adUnavailable && (
        <NativeTabs.BottomAccessory>
          <AccessoryAd onUnavailable={() => setAdUnavailable(true)} />
        </NativeTabs.BottomAccessory>
      )}
    </NativeTabs>
  )
}

/**
 * The accessory has two placements. "regular" is the full-width capsule above
 * the expanded tab bar; "inline" is the small slot beside the minimised bar,
 * far too narrow for a banner. Render nothing there so the ad folds away with
 * the bar instead of being clipped to a sliver. Must be a child of
 * BottomAccessory: usePlacement reads that component's context.
 */
function AccessoryAd({ onUnavailable }: { onUnavailable: () => void }) {
  // Both placements measure 322x48pt on device, so the same banner serves the
  // expanded capsule and the inline slot beside the minimised bar.
  return <BannerAd placement="accessory" onUnavailable={onUnavailable} />
}

/**
 * Android draws a Material 3 navigation bar. Without these it fell back to
 * Material's default lavender surface and indicator, and with only SF Symbols
 * (iOS-only) the tabs had no icons, so with four tabs (labels shown only on the
 * selected one) three of them were invisible. Icons come from each trigger's
 * `md` Material Symbol; this styles the bar in the app's own colours.
 */
function androidTabBar(theme: AppTheme) {
  const primary = theme.colors.primary
  // 15% alpha of the brand colour for the selected-tab pill.
  const indicator = /^#[0-9a-f]{6}$/i.test(primary) ? `${primary}26` : primary
  return {
    backgroundColor: theme.colors.backgroundSecondary,
    indicatorColor: indicator,
    iconColor: { default: theme.colors.textSecondary, selected: primary },
    labelStyle: {
      default: { color: theme.colors.textSecondary },
      selected: { color: primary, fontWeight: "600" as const },
    },
    labelVisibilityMode: "labeled" as const,
  }
}
