/**
 * InterstitialAdManager - Manages interstitial and rewarded video ads
 * 
 * Handles loading, displaying, and tracking of full-screen ads with
 * proper frequency limits and user experience considerations.
 */

import { Platform, Alert } from "react-native"
import { 
  InterstitialAd, 
  RewardedAd, 
  AdEventType,
  RewardedAdEventType 
} from "react-native-google-mobile-ads"
import { adManager } from "./ad-manager"
import { useValidSubscription } from "../stores"
import { getTrackingStatus, TrackingStatus } from "react-native-tracking-transparency"
import { trackAdImpression } from "./ad-analytics"

// Development/Test Ad Unit IDs
const interstitialAdUnitIdDEV = Platform.select({
  ios: "ca-app-pub-3940256099942544/4411468910", // iOS test interstitial
  android: "ca-app-pub-3940256099942544/1033173712", // Android test interstitial
}) || ""

const rewardedAdUnitIdDEV = Platform.select({
  ios: "ca-app-pub-3940256099942544/1712485313", // iOS test rewarded
  android: "ca-app-pub-3940256099942544/5224354917", // Android test rewarded
}) || ""

// Production Ad Unit IDs
const interstitialAdUnitIdLIVE = Platform.select({
  ios: "ca-app-pub-2990192963224757/8921059000",
  android: "ca-app-pub-2990192963224757/3953471654", 
}) || ""

const rewardedAdUnitIdLIVE = Platform.select({
  ios: "ca-app-pub-2990192963224757/5138554459",
  android: "ca-app-pub-2990192963224757/8761911170",
}) || ""

// Use development IDs in dev mode, production IDs in production
const isDevelopment = __DEV__ || process.env.NODE_ENV === 'development'

const interstitialAdUnitId = isDevelopment ? interstitialAdUnitIdDEV : interstitialAdUnitIdLIVE
const rewardedAdUnitId = isDevelopment ? rewardedAdUnitIdDEV : rewardedAdUnitIdLIVE

class InterstitialAdManager {
  private interstitialAd: InterstitialAd | null = null
  private rewardedAd: RewardedAd | null = null
  private isLoading = false
  private trackingStatus: TrackingStatus = "not-determined"

  constructor() {
    this.initializeAds()
    this.setupTrackingStatus()
  }

  private async setupTrackingStatus() {
    this.trackingStatus = await getTrackingStatus()
  }

  private initializeAds() {
    try {
      // Initialize interstitial ad
      this.interstitialAd = InterstitialAd.createForAdRequest(interstitialAdUnitId, {
        requestNonPersonalizedAdsOnly: this.trackingStatus === "denied"
      })

      this.interstitialAd.addAdEventListener(AdEventType.LOADED, () => {
        this.isLoading = false
      })

      this.interstitialAd.addAdEventListener(AdEventType.ERROR, (error) => {
        this.isLoading = false
      })

      this.interstitialAd.addAdEventListener(AdEventType.OPENED, () => {
        trackAdImpression("interstitial", "scroll_threshold")
      })

      this.interstitialAd.addAdEventListener(AdEventType.CLOSED, () => {
        // Preload next ad
        this.loadInterstitialAd()
      })

      // Initialize rewarded ad
      this.rewardedAd = RewardedAd.createForAdRequest(rewardedAdUnitId, {
        requestNonPersonalizedAdsOnly: this.trackingStatus === "denied"
      })

      this.rewardedAd.addAdEventListener(RewardedAdEventType.LOADED, () => {
      })

      this.rewardedAd.addAdEventListener(AdEventType.ERROR, (error) => {
        console.error("Rewarded ad error:", error)
      })

      this.rewardedAd.addAdEventListener(RewardedAdEventType.EARNED_REWARD, (reward) => {
        trackAdImpression("rewarded", "user_initiated")
        // Could grant temporary premium features here
      })

      // Load initial ads
      this.loadInterstitialAd()
      this.loadRewardedAd()
    } catch (error) {
    }
  }

  private loadInterstitialAd() {
    if (this.isLoading || !this.interstitialAd) {
      return
    }
    
    this.isLoading = true
    this.interstitialAd.load()
  }

  private loadRewardedAd() {
    if (!this.rewardedAd) return
    this.rewardedAd.load()
  }

  /**
   * Shows interstitial ad if conditions are met
   */
  public async showInterstitialAd(): Promise<boolean> {
    if (!adManager.shouldShowInterstitial()) {
      adManager.markInterstitialAttempted() // Mark attempt even if blocked
      return false
    }

    if (!this.interstitialAd) {
      adManager.markInterstitialAttempted() // Mark attempt even if failed
      return false
    }

    if (!this.interstitialAd.loaded) {
      this.loadInterstitialAd()
      adManager.markInterstitialAttempted() // Mark attempt even if not loaded
      return false
    }

    try {
      await this.interstitialAd.show()
      await adManager.recordInterstitialShown() // This also marks attempted
      return true
    } catch (error) {
      adManager.markInterstitialAttempted() // Mark attempt even if failed
      return false
    }
  }

  /**
   * Shows rewarded ad with user consent
   */
  public async showRewardedAd(): Promise<boolean> {
    if (!this.rewardedAd?.loaded) {
      return false
    }

    return new Promise((resolve) => {
      Alert.alert(
        "Watch Ad for Premium Features",
        "Watch a short video to unlock premium features for 1 hour",
        [
          {
            text: "Cancel",
            style: "cancel",
            onPress: () => resolve(false)
          },
          {
            text: "Watch",
            onPress: async () => {
              try {
                await this.rewardedAd?.show()
                resolve(true)
              } catch (error) {
                console.error("Failed to show rewarded ad:", error)
                resolve(false)
              }
            }
          }
        ]
      )
    })
  }

  /**
   * Checks if interstitial ad is ready to show
   */
  public isInterstitialReady(): boolean {
    return this.interstitialAd?.loaded === true && adManager.shouldShowInterstitial()
  }

  /**
   * Checks if rewarded ad is ready to show
   */
  public isRewardedReady(): boolean {
    return this.rewardedAd?.loaded === true
  }

  /**
   * Shows ad with silent fallback (no alerts)
   */
  public async showInterstitialWithFallback(onUpgradePress?: () => void): Promise<void> {
    const shown = await this.showInterstitialAd()
    
    // If ad wasn't shown due to frequency limits, just log it
    if (!shown) {
      const debugInfo = adManager.getDebugInfo()
      console.log("🎯 [InterstitialAdManager] Ad not shown - limits reached:", {
        remainingInterstitials: debugInfo.remainingInterstitials,
        cooldownMinutes: debugInfo.cooldownMinutes,
        interstitialsToday: debugInfo.interstitialsToday
      })
    }
  }
}

// Singleton instance
export const interstitialAdManager = new InterstitialAdManager()

/**
 * Hook for components to easily trigger interstitial ads
 */
export const useInterstitialAd = () => {
  const validSubscription = useValidSubscription()

  const showAd = async () => {
    if (validSubscription) return false
    return await interstitialAdManager.showInterstitialAd()
  }

  const showAdWithFallback = async (onUpgradePress?: () => void) => {
    if (validSubscription) return
    await interstitialAdManager.showInterstitialWithFallback(onUpgradePress)
  }

  const showRewardedAd = async () => {
    return await interstitialAdManager.showRewardedAd()
  }

  return {
    showAd,
    showAdWithFallback,
    showRewardedAd,
    isReady: interstitialAdManager.isInterstitialReady(),
    isRewardedReady: interstitialAdManager.isRewardedReady()
  }
}