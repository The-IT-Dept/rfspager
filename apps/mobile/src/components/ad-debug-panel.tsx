/**
 * AdDebugPanel - Development debugging panel for ad system
 * 
 * Shows current ad state, scroll position, and next ad thresholds.
 * Only visible in development mode.
 */

import React from "react"
import { View, StyleSheet, Text } from "react-native"

import { adManager } from "../utils/ad-manager"
import { interstitialAdManager } from "../utils/interstitial-ad-manager"
import { colors } from "../theme"

export const AdDebugPanel = (() => {
  const isDevelopment = __DEV__ || process.env.NODE_ENV === 'development'
  
  if (!isDevelopment) return null

  const debugInfo = adManager.getDebugInfo()
  
  return (
    <View style={styles.container}>
      <View style={styles.card}>
        <Text style={styles.title}>🎯 Ad Debug Panel</Text>

        <View style={styles.row}>
          <Text style={styles.label}>Scroll Count:</Text>
          <Text style={styles.value}>{debugInfo.scrollCount}</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Max Scroll:</Text>
          <Text style={styles.value}>{debugInfo.maxScrollReached}</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Next Native Ad:</Text>
          <Text style={styles.value}>{debugInfo.nextNativeAdAt}</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Native Ads Shown:</Text>
          <Text style={styles.value}>[{debugInfo.nativeAdsShown.join(', ')}]</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Interstitials Today:</Text>
          <Text style={styles.value}>{debugInfo.interstitialsToday}/3</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Interstitial Attempted:</Text>
          <Text style={[styles.value, { color: debugInfo.interstitialAttempted ? colors.error : colors.success }]}>
            {debugInfo.interstitialAttempted ? 'YES' : 'NO'}
          </Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Can Show Interstitial:</Text>
          <Text style={[styles.value, { color: debugInfo.shouldShowInterstitialNow ? colors.success : colors.error }]}>
            {debugInfo.shouldShowInterstitialNow ? 'YES' : 'NO'}
          </Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.label}>Interstitial Loaded:</Text>
          <Text style={[styles.value, { color: interstitialAdManager.isInterstitialReady() ? colors.success : colors.error }]}>
            {interstitialAdManager.isInterstitialReady() ? 'YES' : 'NO'}
          </Text>
        </View>

        {debugInfo.cooldownMinutes > 0 && (
          <View style={styles.row}>
            <Text style={styles.label}>Cooldown:</Text>
            <Text style={styles.value}>{debugInfo.cooldownMinutes}m</Text>
          </View>
        )}

        {debugInfo.isEmergencyMode && (
          <View style={styles.row}>
            <Text style={[styles.label, { color: colors.error }]}>EMERGENCY MODE:</Text>
            <Text style={[styles.value, { color: colors.error }]}>ACTIVE</Text>
          </View>
        )}
      </View>
    </View>
  )
})

const styles = StyleSheet.create({
  container: {
    position: 'absolute',
    top: 100,
    right: 10,
    zIndex: 1000,
    width: 250,
  },
  
  card: {
    backgroundColor: colors.white,
    opacity: 0.95,
    padding: 12,
    borderRadius: 8,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.15,
    shadowRadius: 4,
    elevation: 3,
  },
  
  title: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.primary,
    marginBottom: 8,
  },
  
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 4,
  },
  
  label: {
    fontSize: 12,
    color: colors.textSecondary,
    flex: 1,
  },
  
  value: {
    fontSize: 12,
    fontWeight: '600',
    color: colors.textPrimary,
    textAlign: 'right',
  },
})