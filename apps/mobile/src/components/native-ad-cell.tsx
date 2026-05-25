/**
 * NativeAdCell - Inline native ad component for lists
 * 
 * Displays native ads that blend with the app's design between list items.
 * Automatically hidden for premium subscribers.
 */

import React, { useEffect, useState } from "react"
import { View, StyleSheet, Text } from "react-native"
import { Button } from "./button/button"

import { useValidSubscription } from "../stores"
import { colors } from "../theme"
import { trackAdImpression, trackAdClick } from "../utils/ad-analytics"
import { SubscriptionModal } from "./subscription-modal"

interface NativeAdCellProps {
  onAdImpression?: () => void
  onAdClick?: () => void
}

export function NativeAdCell({ onAdImpression, onAdClick }: NativeAdCellProps) {
  const validSubscription = useValidSubscription()
  const [adLoaded, setAdLoaded] = useState(false)
  const [adFailed, setAdFailed] = useState(false)
  const [showSubscriptionModal, setShowSubscriptionModal] = useState(false)

  useEffect(() => {
    // Simulate ad loading (more reliable in development)
    const timer = setTimeout(() => {
      const isDevelopment = __DEV__ || process.env.NODE_ENV === 'development'
      // 95% success rate in development, 90% in production
      const successRate = isDevelopment ? 0.95 : 0.9
      
      if (Math.random() > (1 - successRate)) {
        setAdLoaded(true)
        trackAdImpression("native", "list_inline")
        onAdImpression?.()
        console.log("🎯 [NativeAdCell] Ad loaded successfully")
      } else {
        setAdFailed(true)
        console.log("🎯 [NativeAdCell] Ad failed to load (simulated)")
      }
    }, 1000)

    return () => clearTimeout(timer)
  }, [onAdImpression])

  // Don't show for premium users
  if (validSubscription) return null
  
  // Don't show if failed to load
  if (adFailed) return null

  // Loading state
  if (!adLoaded) {
    return (
      <View style={styles.loadingContainer}>
        <View style={styles.loadingCard}>
          <View style={styles.loadingContent} />
        </View>
      </View>
    )
  }

  const handleUpgradePress = () => {
    trackAdClick("native", "list_inline")
    setShowSubscriptionModal(true)
    onAdClick?.()
    console.log("🎯 [NativeAdCell] Ad clicked - opening subscription modal")
  }

  const isDevelopment = __DEV__ || process.env.NODE_ENV === 'development'

  return (
    <View style={styles.container}>
      <View style={styles.adCard}>
        <View style={styles.sponsoredBadge}>
          <Text style={styles.sponsoredText}>
            {isDevelopment ? 'Test Ad' : 'Sponsored'}
          </Text>
        </View>

        <View style={styles.adContent}>
          <Text style={styles.adTitle}>
            {isDevelopment ? '[DEV] RFS Pager Premium' : 'RFS Pager Premium'}
          </Text>
          <Text style={styles.adDescription}>
            {isDevelopment
              ? 'This is a test native ad in development mode. Remove ads and unlock priority notifications, custom alerts, and enhanced incident details.'
              : 'Remove ads and unlock priority notifications, custom alerts, and enhanced incident details.'
            }
          </Text>

          <Button
            mode="contained"
            onPress={handleUpgradePress}
            style={styles.ctaButton}
            labelStyle={styles.ctaButtonText}
          >
            Upgrade Now
          </Button>
        </View>
      </View>

      <SubscriptionModal
        visible={showSubscriptionModal}
        onDismiss={() => setShowSubscriptionModal(false)}
        source="native_ad"
      />
    </View>
  )
}

const styles = StyleSheet.create({
  container: {
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  
  adCard: {
    backgroundColor: colors.backgroundSecondary,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: 8,
    padding: 16,
  },

  loadingContainer: {
    paddingHorizontal: 16,
    paddingVertical: 8,
  },

  loadingCard: {
    backgroundColor: colors.backgroundSecondary,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: 8,
    padding: 16,
  },
  
  loadingContent: {
    height: 80,
    backgroundColor: colors.backgroundTertiary,
    borderRadius: 4,
  },
  
  sponsoredBadge: {
    alignSelf: "flex-start",
    backgroundColor: colors.borderSecondary,
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 3,
    marginBottom: 12,
  },
  
  sponsoredText: {
    color: colors.textSecondary,
    fontSize: 10,
    fontWeight: "600",
  },
  
  adContent: {
    gap: 12,
  },
  
  adTitle: {
    color: colors.primary,
    fontSize: 18,
    fontWeight: "600",
  },

  adDescription: {
    color: colors.textSecondary,
    fontSize: 14,
    lineHeight: 20,
  },
  
  ctaButton: {
    alignSelf: "flex-start",
    marginTop: 8,
  },
  
  ctaButtonText: {
    fontSize: 14,
    fontWeight: "600",
  },
})