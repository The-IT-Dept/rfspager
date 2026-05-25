/**
 * AdDebugPanel - Development debugging panel for ad system
 *
 * Renders a small floating "AD" toggle button in dev builds. Tapping
 * expands a panel showing current ad state, scroll position, and next ad
 * thresholds. Hidden entirely in production.
 */

import React, { useState } from "react"
import { Pressable, StyleSheet, Text, View } from "react-native"

import { adManager } from "../utils/ad-manager"
import { interstitialAdManager } from "../utils/interstitial-ad-manager"
import { colors } from "../theme"

export const AdDebugPanel = () => {
  const isDevelopment = __DEV__ || process.env.NODE_ENV === "development"
  const [open, setOpen] = useState(false)

  if (!isDevelopment) return null

  return (
    <View style={styles.container} pointerEvents="box-none">
      <Pressable
        onPress={() => setOpen((v) => !v)}
        style={({ pressed }) => [styles.toggle, pressed && styles.togglePressed]}
        accessibilityLabel={open ? "Hide ad debug panel" : "Show ad debug panel"}
        hitSlop={8}
      >
        <Text style={styles.toggleText}>{open ? "×" : "AD"}</Text>
      </Pressable>

      {open && <DebugCard />}
    </View>
  )
}

const DebugCard = () => {
  const debugInfo = adManager.getDebugInfo()

  return (
    <View style={styles.card}>
      <Text style={styles.title}>🎯 Ad Debug Panel</Text>

      <Row label="Scroll Count" value={String(debugInfo.scrollCount)} />
      <Row label="Max Scroll" value={String(debugInfo.maxScrollReached)} />
      <Row label="Next Native Ad" value={String(debugInfo.nextNativeAdAt)} />
      <Row
        label="Native Ads Shown"
        value={`[${debugInfo.nativeAdsShown.join(", ")}]`}
      />
      <Row label="Interstitials Today" value={`${debugInfo.interstitialsToday}/3`} />

      <Row
        label="Interstitial Attempted"
        value={debugInfo.interstitialAttempted ? "YES" : "NO"}
        valueColor={debugInfo.interstitialAttempted ? colors.error : colors.success}
      />

      <Row
        label="Can Show Interstitial"
        value={debugInfo.shouldShowInterstitialNow ? "YES" : "NO"}
        valueColor={debugInfo.shouldShowInterstitialNow ? colors.success : colors.error}
      />

      <Row
        label="Interstitial Loaded"
        value={interstitialAdManager.isInterstitialReady() ? "YES" : "NO"}
        valueColor={
          interstitialAdManager.isInterstitialReady() ? colors.success : colors.error
        }
      />

      {debugInfo.cooldownMinutes > 0 && (
        <Row label="Cooldown" value={`${debugInfo.cooldownMinutes}m`} />
      )}

      {debugInfo.isEmergencyMode && (
        <Row
          label="EMERGENCY MODE"
          value="ACTIVE"
          labelColor={colors.error}
          valueColor={colors.error}
        />
      )}
    </View>
  )
}

const Row = ({
  label,
  value,
  labelColor,
  valueColor,
}: {
  label: string
  value: string
  labelColor?: string
  valueColor?: string
}) => (
  <View style={styles.row}>
    <Text style={[styles.label, labelColor ? { color: labelColor } : null]}>{label}:</Text>
    <Text style={[styles.value, valueColor ? { color: valueColor } : null]}>{value}</Text>
  </View>
)

const styles = StyleSheet.create({
  container: {
    position: "absolute",
    top: 100,
    right: 10,
    zIndex: 1000,
    alignItems: "flex-end",
  },

  toggle: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: colors.primary,
    alignItems: "center",
    justifyContent: "center",
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.25,
    shadowRadius: 3,
    elevation: 4,
  },

  togglePressed: {
    opacity: 0.7,
  },

  toggleText: {
    color: colors.white,
    fontSize: 13,
    fontWeight: "700",
  },

  card: {
    marginTop: 8,
    width: 250,
    backgroundColor: colors.white,
    opacity: 0.95,
    padding: 12,
    borderRadius: 8,
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.15,
    shadowRadius: 4,
    elevation: 3,
  },

  title: {
    fontSize: 14,
    fontWeight: "700",
    color: colors.primary,
    marginBottom: 8,
  },

  row: {
    flexDirection: "row",
    justifyContent: "space-between",
    marginBottom: 4,
  },

  label: {
    fontSize: 12,
    color: colors.textSecondary,
    flex: 1,
  },

  value: {
    fontSize: 12,
    fontWeight: "600",
    color: colors.textPrimary,
    textAlign: "right",
  },
})
