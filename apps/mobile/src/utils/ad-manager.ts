/**
 * AdManager - Centralized ad management service
 * 
 * Manages ad frequency, timing, and session limits for optimal revenue
 * while maintaining good user experience.
 */

import AsyncStorage from "@react-native-async-storage/async-storage"

interface AdSession {
  date: string
  interstitialCount: number
  lastInterstitialTime: number
  sessionStartTime: number
}

export class AdManager {
  // Session tracking
  private currentSession: AdSession
  private readonly STORAGE_KEY = "adManager_session"
  
  // Ad frequency limits
  private readonly MAX_INTERSTITIALS_PER_DAY = 3
  private readonly INTERSTITIAL_COOLDOWN_MS = 5 * 60 * 1000 // 5 minutes
  private readonly SCROLL_THRESHOLD_NATIVE = 100 // Every 100 items
  private readonly SCROLL_THRESHOLD_INTERSTITIAL = 200 // First interstitial at 200
  
  // Tracking state
  public scrollCount = 0
  public lastNativeAdIndex = 0
  public isEmergencyMode = false
  public nativeAdsShown = new Set<number>() // Track which positions have shown ads
  public maxScrollReached = 0 // Track the maximum scroll position reached
  public interstitialAttempted = false // Track if interstitial has been attempted this session
  
  constructor() {
    this.initializeSession()
  }

  private async initializeSession() {
    try {
      const stored = await AsyncStorage.getItem(this.STORAGE_KEY)
      const today = new Date().toDateString()
      
      if (stored) {
        const session = JSON.parse(stored) as AdSession
        if (session.date === today) {
          this.currentSession = session
          return
        }
      }
      
      // New day or no stored session
      this.currentSession = {
        date: today,
        interstitialCount: 0,
        lastInterstitialTime: 0,
        sessionStartTime: Date.now()
      }
      
      await this.saveSession()
    } catch (error) {
      console.error("AdManager: Failed to initialize session", error)
      this.resetSession()
    }
  }

  private async saveSession() {
    try {
      await AsyncStorage.setItem(this.STORAGE_KEY, JSON.stringify(this.currentSession))
    } catch (error) {
      console.error("AdManager: Failed to save session", error)
    }
  }

  private resetSession() {
    this.currentSession = {
      date: new Date().toDateString(),
      interstitialCount: 0,
      lastInterstitialTime: 0,
      sessionStartTime: Date.now()
    }
  }

  /**
   * Updates scroll count and returns whether a native ad should be shown
   */
  public onScroll(itemCount: number): boolean {
    // Always track the maximum scroll position reached
    this.maxScrollReached = Math.max(this.maxScrollReached, itemCount)
    this.scrollCount = Math.max(this.scrollCount, itemCount)
    
    // Don't show ads in emergency mode
    if (this.isEmergencyMode) {
      return false
    }
    
    // Calculate which ad position this should be
    const adPosition = Math.floor(itemCount / this.SCROLL_THRESHOLD_NATIVE) * this.SCROLL_THRESHOLD_NATIVE
    
    // Only show ad at exact threshold positions (100, 200, 300, etc.)
    const isExactThreshold = itemCount > 0 && itemCount % this.SCROLL_THRESHOLD_NATIVE === 0
    
    // Check if we haven't shown an ad at this position yet
    const hasShownAtPosition = this.nativeAdsShown.has(adPosition)
    
    // Additional check: only show if this position is within reasonable range of max scroll
    // This prevents ads from showing again during re-renders when data changes
    const isReasonablePosition = adPosition <= this.maxScrollReached + this.SCROLL_THRESHOLD_NATIVE
    
    const shouldShow = isExactThreshold && 
                      !hasShownAtPosition && 
                      adPosition >= this.SCROLL_THRESHOLD_NATIVE &&
                      isReasonablePosition
    
    if (shouldShow) {
      this.nativeAdsShown.add(adPosition)
      this.lastNativeAdIndex = adPosition
    }
    
    return shouldShow
  }

  /**
   * Checks if an interstitial ad should be shown
   */
  public shouldShowInterstitial(): boolean {
    // Don't show if emergency mode
    if (this.isEmergencyMode) return false
    
    // Don't show if haven't reached threshold
    if (this.scrollCount < this.SCROLL_THRESHOLD_INTERSTITIAL) return false
    
    // Don't show if already attempted this session
    if (this.interstitialAttempted) return false
    
    // Don't show if daily limit reached
    if (this.currentSession.interstitialCount >= this.MAX_INTERSTITIALS_PER_DAY) return false
    
    // Don't show if in cooldown period
    const timeSinceLastAd = Date.now() - this.currentSession.lastInterstitialTime
    if (timeSinceLastAd < this.INTERSTITIAL_COOLDOWN_MS) return false
    
    return true
  }

  /**
   * Records that an interstitial ad was shown
   */
  public async recordInterstitialShown() {
    this.currentSession.interstitialCount++
    this.currentSession.lastInterstitialTime = Date.now()
    this.interstitialAttempted = true
    await this.saveSession()
  }

  /**
   * Marks that an interstitial attempt was made (even if it failed)
   */
  public markInterstitialAttempted() {
    this.interstitialAttempted = true
    console.log("🎯 [AdManager] Interstitial attempt marked - won't try again this session")
  }

  /**
   * Sets emergency mode (disables ads during active incidents)
   */
  public setEmergencyMode(isEmergency: boolean) {
    this.isEmergencyMode = isEmergency
  }

  /**
   * Resets scroll tracking for new screen/session
   */
  public resetScrollTracking() {
    this.scrollCount = 0
    this.lastNativeAdIndex = 0
    this.maxScrollReached = 0
    this.nativeAdsShown.clear()
    this.interstitialAttempted = false
  }

  /**
   * Gets next interstitial cooldown time in minutes
   */
  public getInterstitialCooldownMinutes(): number {
    const timeSinceLastAd = Date.now() - this.currentSession.lastInterstitialTime
    const remainingCooldown = this.INTERSTITIAL_COOLDOWN_MS - timeSinceLastAd
    return Math.max(0, Math.ceil(remainingCooldown / (60 * 1000)))
  }

  /**
   * Gets remaining interstitials for today
   */
  public getRemainingInterstitials(): number {
    return Math.max(0, this.MAX_INTERSTITIALS_PER_DAY - this.currentSession.interstitialCount)
  }

  /**
   * Debug info for development
   */
  public getDebugInfo() {
    return {
      scrollCount: this.scrollCount,
      maxScrollReached: this.maxScrollReached,
      lastNativeAdIndex: this.lastNativeAdIndex,
      isEmergencyMode: this.isEmergencyMode,
      nativeAdsShown: Array.from(this.nativeAdsShown),
      nextNativeAdAt: this.getNextNativeAdPosition(),
      interstitialsToday: this.currentSession.interstitialCount,
      interstitialAttempted: this.interstitialAttempted,
      cooldownMinutes: this.getInterstitialCooldownMinutes(),
      remainingInterstitials: this.getRemainingInterstitials(),
      shouldShowInterstitialNow: this.shouldShowInterstitial()
    }
  }

  /**
   * Gets the next position where a native ad should show
   */
  public getNextNativeAdPosition(): number {
    const currentPosition = Math.floor(this.scrollCount / this.SCROLL_THRESHOLD_NATIVE) * this.SCROLL_THRESHOLD_NATIVE
    return currentPosition + this.SCROLL_THRESHOLD_NATIVE
  }

  /**
   * Checks if an ad should be stable at this position (for re-renders)
   */
  public shouldShowStableAd(itemCount: number): boolean {
    if (this.isEmergencyMode) return false
    
    const adPosition = Math.floor(itemCount / this.SCROLL_THRESHOLD_NATIVE) * this.SCROLL_THRESHOLD_NATIVE
    const isExactThreshold = itemCount > 0 && itemCount % this.SCROLL_THRESHOLD_NATIVE === 0
    const hasShownAtPosition = this.nativeAdsShown.has(adPosition)
    
    // Show ad if it's an exact threshold and we've recorded showing an ad at this position
    // This ensures ads remain visible during re-renders/pagination
    return isExactThreshold && hasShownAtPosition && adPosition >= this.SCROLL_THRESHOLD_NATIVE
  }
}

// Singleton instance
export const adManager = new AdManager()