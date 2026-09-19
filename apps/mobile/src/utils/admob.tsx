import React, { useEffect, useState } from "react"
import { Platform, View, type ViewStyle } from "react-native"
import { isLiquidGlassAvailable } from "expo-glass-effect"
import { useSafeAreaInsets } from "react-native-safe-area-context"

import { BannerAd as BannerAdAdmob, BannerAdSize } from "react-native-google-mobile-ads"

import { useValidSubscription } from "../stores"
import { getTrackingStatus, TrackingStatus } from "react-native-tracking-transparency"

// Development/Test Ad Unit IDs (always use these during development)
const adUnitIDBannerDEV = Platform.select({
  ios: "ca-app-pub-3940256099942544/2435281174", // iOS test banner
  android: "ca-app-pub-3940256099942544/9214589741", // Android test banner
})

const adUnitIDRewardDEV = Platform.select({
  ios: "ca-app-pub-3940256099942544/1712485313", // iOS test rewarded
  android: "ca-app-pub-3940256099942544/5224354917", // Android test rewarded
})

// Production Ad Unit IDs
const adUnitIDBannerLIVE = Platform.select({
  ios: "ca-app-pub-2990192963224757/7544854817",
  android: "ca-app-pub-2990192963224757/8810190245",
})

const adUnitIDRewardLIVE = Platform.select({
  ios: "ca-app-pub-2990192963224757/5138554459",
  android: "ca-app-pub-2990192963224757/8761911170",
})

// Use development IDs in dev mode, production IDs in production
const isDevelopment = __DEV__ || process.env.NODE_ENV === 'development'

export const adUnitIDBanner = isDevelopment ? adUnitIDBannerDEV : adUnitIDBannerLIVE
export const adUnitIDReward = isDevelopment ? adUnitIDRewardDEV : adUnitIDRewardLIVE

// Debug logging
console.log(`🎯 [AdMob] Environment: ${isDevelopment ? 'DEVELOPMENT' : 'PRODUCTION'}`)
console.log(`🎯 [AdMob] Banner Ad Unit ID: ${adUnitIDBanner}`)
console.log(`🎯 [AdMob] Reward Ad Unit ID: ${adUnitIDReward}`)

/**
 * Where the banner is mounted.
 *
 * - "accessory": inside NativeTabs.BottomAccessory, the iOS 26 slot that sits
 *   directly above the tab bar and moves with it as it minimises. One instance
 *   for the whole tab UI, owned by the tabs layout.
 * - "floating" (default): absolutely positioned at the bottom of a screen.
 *   On iOS 26 this returns null, because the accessory already shows the ad
 *   and a second one would sit over the glass tab bar. Elsewhere it clears the
 *   native tab bar, whose height is not part of the window safe-area insets.
 */
export type BannerAdPlacement = "floating" | "accessory"

// UITabBar is 49pt; Material 3 navigation bar is 80dp. Both platforms' native
// tab bars sit outside the window safe area reported by SafeAreaProvider.
const NATIVE_TAB_BAR_HEIGHT = Platform.OS === "ios" ? 49 : 80

export function BannerAd({ placement = "floating" }: { placement?: BannerAdPlacement } = {}) {
  const validSubscription = useValidSubscription()
  const insets = useSafeAreaInsets()
  const glassTabBar = Platform.OS === "ios" && isLiquidGlassAvailable()
  const [trackingStatus, setTrackingStatus] = useState<TrackingStatus>()
  const [adLoaded, setAdLoaded] = useState(false)
  const [adFailed, setAdFailed] = useState(false)

  useEffect(() => {
    console.log("🎯 [BannerAd] Component mounted")
    console.log(`🎯 [BannerAd] Valid Subscription: ${validSubscription}`)
    console.log(`🎯 [BannerAd] Ad Loaded: ${adLoaded}`)
  }, [adLoaded]);

  useEffect(() => {
    getTrackingStatus().then(setTrackingStatus)
  }, [])

  if (validSubscription) return null
  if (adFailed) return null // Don't render anything if ad failed to load
  // On iOS 26 the tabs layout renders the ad as a BottomAccessory; a floating
  // copy would double up and overlap the glass tab bar.
  if (placement === "floating" && glassTabBar) return null

  // The accessory is a rounded glass capsule the system draws, measured at
  // 322x48pt in both the regular and inline placements. The 320x50 BANNER fits
  // the width exactly and overshoots the height by 1pt top and bottom, which
  // the clip absorbs; capsule radius is half the slot height.
  const size = placement === "accessory" ? BannerAdSize.BANNER : BannerAdSize.FULL_BANNER
  const containerStyle: ViewStyle =
    placement === "accessory"
      ? {
          height: 48,
          alignSelf: "stretch",
          alignItems: "center",
          justifyContent: "center",
          overflow: "hidden",
          borderRadius: 24,
          borderCurve: "continuous",
        }
      : {
          position: "absolute" as const,
          bottom: insets.bottom + NATIVE_TAB_BAR_HEIGHT,
          left: 0,
          right: 0,
          zIndex: 10,
        }

  return (
    <View
      style={containerStyle}
    >
      <BannerAdAdmob
        unitId={adUnitIDBanner}
        size={size}
        requestOptions={{ requestNonPersonalizedAdsOnly: trackingStatus === "denied" }}
        onAdLoaded={() => {
          setAdLoaded(true)
          console.log("🎯 [BannerAd] Ad loaded successfully")
        }}
        onAdFailedToLoad={(error) => {
          console.error("🎯 [BannerAd] Ad failed to load:", error)
          console.error("🎯 [BannerAd] Ad Unit ID:", adUnitIDBanner)
          console.error("🎯 [BannerAd] Tracking Status:", trackingStatus)
          setAdFailed(true)
        }}
        onAdOpened={() => {
          console.log("🎯 [BannerAd] Ad opened")
        }}
        onAdClosed={() => {
          console.log("🎯 [BannerAd] Ad closed")
        }}
      />
    </View>
  )
}
