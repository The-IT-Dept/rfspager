import { NativeTabs } from "expo-router/unstable-native-tabs"
import { useAppTheme } from "../../theme"

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

  return (
    <NativeTabs
      tintColor={theme.colors.primary}
      minimizeBehavior="onScrollDown"
    >
      <NativeTabs.Trigger name="(pager)">
        <NativeTabs.Trigger.Icon sf="antenna.radiowaves.left.and.right" />
        <NativeTabs.Trigger.Label>Pager</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(incidents)">
        <NativeTabs.Trigger.Icon sf="flame" />
        <NativeTabs.Trigger.Label>Incidents</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(map)">
        <NativeTabs.Trigger.Icon sf="map" />
        <NativeTabs.Trigger.Label>Map</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="(settings)">
        <NativeTabs.Trigger.Icon sf="gearshape" />
        <NativeTabs.Trigger.Label>Settings</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>
    </NativeTabs>
  )
}
