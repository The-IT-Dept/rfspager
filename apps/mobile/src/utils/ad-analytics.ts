/**
 * AdAnalytics - Simple analytics tracking for ad performance
 * 
 * Tracks ad impressions, clicks, and conversion events for optimization.
 * Integrates with existing Firebase Analytics if available.
 */

import { Platform } from "react-native"
import AsyncStorage from "@react-native-async-storage/async-storage"

interface AdEvent {
  type: "impression" | "click" | "conversion"
  adType: "banner" | "native" | "interstitial" | "rewarded"
  placement: string
  timestamp: number
  sessionId: string
}

interface AdMetrics {
  totalImpressions: number
  totalClicks: number
  totalConversions: number
  ctr: number // Click-through rate
  cvr: number // Conversion rate
  revenuePerImpression: number
}

class AdAnalytics {
  private sessionId: string
  private sessionStartTime: number
  private events: AdEvent[] = []
  private readonly STORAGE_KEY = "adAnalytics_metrics"
  private readonly MAX_EVENTS_CACHE = 100

  constructor() {
    this.sessionId = this.generateSessionId()
    this.sessionStartTime = Date.now()
    this.loadStoredMetrics()
  }

  private generateSessionId(): string {
    return `${Date.now()}_${Math.random().toString(36).substr(2, 9)}`
  }

  private async loadStoredMetrics() {
    try {
      const stored = await AsyncStorage.getItem(this.STORAGE_KEY)
      if (stored) {
        // Could load historical metrics here if needed
      }
    } catch (error) {
      console.error("AdAnalytics: Failed to load stored metrics", error)
    }
  }

  /**
   * Track an ad impression
   */
  public trackImpression(adType: AdEvent["adType"], placement: string) {
    this.addEvent({
      type: "impression",
      adType,
      placement,
      timestamp: Date.now(),
      sessionId: this.sessionId
    })

    console.log(`[AdAnalytics] Impression: ${adType} at ${placement}`)
    
    // Send to Firebase Analytics if available
    this.sendToFirebase("ad_impression", {
      ad_type: adType,
      placement,
      session_id: this.sessionId
    })
  }

  /**
   * Track an ad click
   */
  public trackClick(adType: AdEvent["adType"], placement: string) {
    this.addEvent({
      type: "click",
      adType,
      placement,
      timestamp: Date.now(),
      sessionId: this.sessionId
    })

    console.log(`[AdAnalytics] Click: ${adType} at ${placement}`)
    
    // Send to Firebase Analytics if available
    this.sendToFirebase("ad_click", {
      ad_type: adType,
      placement,
      session_id: this.sessionId
    })
  }

  /**
   * Track a conversion (e.g., subscription purchase)
   */
  public trackConversion(adType: AdEvent["adType"], placement: string, revenue?: number) {
    this.addEvent({
      type: "conversion",
      adType,
      placement,
      timestamp: Date.now(),
      sessionId: this.sessionId
    })

    console.log(`[AdAnalytics] Conversion: ${adType} at ${placement}${revenue ? ` ($${revenue})` : ""}`)
    
    // Send to Firebase Analytics if available
    this.sendToFirebase("ad_conversion", {
      ad_type: adType,
      placement,
      revenue: revenue || 0,
      session_id: this.sessionId
    })
  }

  /**
   * Get current session metrics
   */
  public getSessionMetrics(): AdMetrics {
    const impressions = this.events.filter(e => e.type === "impression").length
    const clicks = this.events.filter(e => e.type === "click").length
    const conversions = this.events.filter(e => e.type === "conversion").length

    return {
      totalImpressions: impressions,
      totalClicks: clicks,
      totalConversions: conversions,
      ctr: impressions > 0 ? (clicks / impressions) * 100 : 0,
      cvr: clicks > 0 ? (conversions / clicks) * 100 : 0,
      revenuePerImpression: impressions > 0 ? (conversions * 4.99) / impressions : 0 // Assume $4.99 subscription
    }
  }

  /**
   * Get metrics by placement
   */
  public getPlacementMetrics(placement: string): AdMetrics {
    const placementEvents = this.events.filter(e => e.placement === placement)
    const impressions = placementEvents.filter(e => e.type === "impression").length
    const clicks = placementEvents.filter(e => e.type === "click").length
    const conversions = placementEvents.filter(e => e.type === "conversion").length

    return {
      totalImpressions: impressions,
      totalClicks: clicks,
      totalConversions: conversions,
      ctr: impressions > 0 ? (clicks / impressions) * 100 : 0,
      cvr: clicks > 0 ? (conversions / clicks) * 100 : 0,
      revenuePerImpression: impressions > 0 ? (conversions * 4.99) / impressions : 0
    }
  }

  /**
   * Get debug information for development
   */
  public getDebugInfo() {
    const metrics = this.getSessionMetrics()
    const sessionDuration = (Date.now() - this.sessionStartTime) / 1000 / 60 // minutes

    return {
      sessionId: this.sessionId,
      sessionDuration: sessionDuration.toFixed(1),
      totalEvents: this.events.length,
      metrics,
      recentEvents: this.events.slice(-10),
      placementBreakdown: this.getPlacementBreakdown()
    }
  }

  private getPlacementBreakdown() {
    const placements = [...new Set(this.events.map(e => e.placement))]
    return placements.map(placement => ({
      placement,
      metrics: this.getPlacementMetrics(placement)
    }))
  }

  private addEvent(event: AdEvent) {
    this.events.push(event)
    
    // Keep cache size manageable
    if (this.events.length > this.MAX_EVENTS_CACHE) {
      this.events = this.events.slice(-this.MAX_EVENTS_CACHE)
    }
  }

  private async sendToFirebase(eventName: string, parameters: any) {
    try {
      const { getAnalytics, logEvent } = require("@react-native-firebase/analytics")
      await logEvent(getAnalytics(), eventName, {
        ...parameters,
        platform: Platform.OS,
        timestamp: Date.now()
      })
    } catch (error) {
      // Firebase not available or not configured - that's fine
      console.log("AdAnalytics: Firebase Analytics not available, using local tracking only")
    }
  }

  /**
   * Export analytics data for external analysis
   */
  public async exportData() {
    const data = {
      sessionInfo: {
        sessionId: this.sessionId,
        startTime: this.sessionStartTime,
        duration: Date.now() - this.sessionStartTime,
        platform: Platform.OS
      },
      metrics: this.getSessionMetrics(),
      events: this.events,
      placementBreakdown: this.getPlacementBreakdown()
    }

    try {
      await AsyncStorage.setItem(`adAnalytics_export_${Date.now()}`, JSON.stringify(data))
      return data
    } catch (error) {
      console.error("AdAnalytics: Failed to export data", error)
      return null
    }
  }
}

// Singleton instance
export const adAnalytics = new AdAnalytics()

// Convenience functions for easy usage
export const trackAdImpression = (adType: AdEvent["adType"], placement: string) => {
  adAnalytics.trackImpression(adType, placement)
}

export const trackAdClick = (adType: AdEvent["adType"], placement: string) => {
  adAnalytics.trackClick(adType, placement)
}

export const trackAdConversion = (adType: AdEvent["adType"], placement: string, revenue?: number) => {
  adAnalytics.trackConversion(adType, placement, revenue)
}