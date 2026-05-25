import React, { useEffect, useState } from "react"
import { Platform, View } from "react-native"
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

export function BannerAd() {
  const validSubscription = useValidSubscription()
  const insets = useSafeAreaInsets()
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

  return (
    <View style={{ position: "absolute", bottom: insets.bottom, left: 0, right: 0, zIndex: 10 }}>
      <BannerAdAdmob
        unitId={adUnitIDBanner}
        size={BannerAdSize.FULL_BANNER}
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
