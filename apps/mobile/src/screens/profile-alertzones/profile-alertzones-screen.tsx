/* eslint-disable react-native/no-inline-styles */
/**
 * ProfileAlertzonesScreen
 * @format
 */

import React, { FC, useEffect } from "react"
import { Text, View, ScrollView } from "react-native"
import { useNavigation } from "expo-router"
import { Ionicons } from "@expo/vector-icons"

import { useAlertzones } from "../../hooks"
import { BannerAd } from "../../utils/admob"
import { Alertzone } from "../../components"
import { makestyles } from "./styles"
import { useAppTheme } from "../../theme"

export const ProfileAlertzonesScreen: FC =
  function ProfileAlertzonesScreen() {
    const navigation = useNavigation()
    const theme = useAppTheme()
    const styles = makestyles(theme)
    const { data, refetch } = useAlertzones()

    useEffect(() => {
      navigation.addListener("focus", () => {
        refetch()
      })
    }, [])


    return (
      <ScrollView
        style={styles.container}
        showsVerticalScrollIndicator={false}
        contentInsetAdjustmentBehavior="automatic"
      >
        {/* Description Section */}
        <View style={styles.mainContainer}>
          <View style={styles.headerRow}>
            <View style={[styles.iconContainer, { backgroundColor: theme.colors.primary }]}>
              <Ionicons name="location" size={10} color="white" />
            </View>
            <View style={styles.headerContent}>
              <Text style={styles.sectionTitle}>Location-Based Alerts</Text>
              <Text style={styles.sectionSubtitle}>Geographic notification zones</Text>
            </View>
          </View>

          <View style={styles.dataRow}>
            <Text style={styles.dataLabel}>Purpose</Text>
            <Text style={styles.dataValue}>
              Alternative to capcode alerts - triggers when emergency messages match your zones
            </Text>
          </View>
        </View>

        {/* Alert Zones List */}
        {data && data.length > 0 ? (
          <View style={styles.zonesContainer}>
            <View style={styles.zonesHeader}>
              <Text style={styles.zonesTitle}>Your Alert Zones ({data.length})</Text>
            </View>
            {data.map((alertzone, index) => (
              <View key={alertzone.id}>
                <Alertzone
                  alertzone={alertzone}
                  onDelete={() => {
                    refetch()
                  }}
                />
              </View>
            ))}
          </View>
        ) : (
          <View style={styles.emptyContainer}>
            <View style={styles.emptyHeader}>
              <Text style={styles.emptyTitle}>No Alert Zones</Text>
            </View>
            <View style={styles.emptyContent}>
              <Text style={styles.emptyText}>
                You haven't created any alert zones yet. Tap the + button above to create your first
                zone.
              </Text>
            </View>
          </View>
        )}

        <BannerAd />
      </ScrollView>
    )
  }
