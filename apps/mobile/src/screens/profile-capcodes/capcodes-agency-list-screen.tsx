/* eslint-disable react-native/no-inline-styles */

import React, { FC, useState, useMemo, useCallback } from "react"
import { StyleSheet, View, TouchableOpacity, ScrollView } from "react-native"
import { Text, ActivityIndicator } from "react-native"
import { BannerAd } from "../../utils/admob"
import { useCapcodesData } from "./use-capcodes-data"
import { AppIcon } from "../../components/app-icon/app-icon"
import { Stack, useRouter } from "expo-router"
import { useAppTheme, AppTheme } from "../../theme"

export const CapcodesAgencyListScreen: FC =
  function CapcodesAgencyListScreen() {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const [search, setSearch] = useState<string>("")

    // Use shared data hook - no duplicate loading!
    const { groupedData, loading, initialLoaded } = useCapcodesData()

    // Filter agencies by name
    const filteredGroupedData = useMemo(() => {
      if (!search.trim()) return groupedData
      const searchLower = search.toLowerCase()
      return groupedData.filter((group) => group.agency.toLowerCase().includes(searchLower))
    }, [groupedData, search])

    const handleAgencyPress = useCallback(
      (agencyData: (typeof groupedData)[0]) => {
        router.push(`/settings/capcode-detail?agencyId=${agencyData.agencyId}&agency=${encodeURIComponent(agencyData.agency)}`)
      },
      [router],
    )


    if (!initialLoaded || loading) {
      return (
        <View style={styles.container}>
          <View style={styles.loadingContainer}>
            <ActivityIndicator size="large" color={theme.colors.primary} />
            <Text style={styles.loadingText}>Loading capcodes...</Text>
          </View>
        </View>
      )
    }

    return (
      <View style={styles.container}>
        <Stack.Screen
          options={{
            headerSearchBarOptions: {
              placeholder: "Search agencies...",
              onChangeText: (e: any) => setSearch(e?.nativeEvent?.text ?? ""),
              onClose: () => setSearch(""),
              autoCapitalize: "none",
            },
          }}
        />

        {/* Agency List */}
        {filteredGroupedData.length === 0 ? (
          <View style={styles.emptyContainer}>
            <Text style={styles.emptyText}>
              {search ? "No agencies found" : "No capcodes available"}
            </Text>
            <Text style={styles.emptySubtext}>
              {search ? "Try adjusting your search" : "Check back later for new capcodes"}
            </Text>
          </View>
        ) : (
          <ScrollView
            style={styles.scrollView}
            showsVerticalScrollIndicator={false}
            contentInsetAdjustmentBehavior="automatic"
          >
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>AGENCIES</Text>
            </View>
            <View style={styles.sectionContainer}>
              {filteredGroupedData.map((item, index) => {
                const isLastItem = index === filteredGroupedData.length - 1
                const hasActiveItems = item.subscribedCount > 0 || item.filteredCount > 0

                return (
                  <TouchableOpacity
                    key={item.agency}
                    style={[styles.agencyItem, isLastItem && styles.lastAgencyItem]}
                    onPress={() => handleAgencyPress(item)}
                    activeOpacity={0.7}
                  >
                    <View style={styles.agencyContent}>
                      <Text style={styles.agencyName}>{item.agency}</Text>
                      <Text style={styles.agencyCount}>
                        {hasActiveItems
                          ? [
                              item.subscribedCount > 0 && `${item.subscribedCount} subscribed`,
                              item.filteredCount > 0 && `${item.filteredCount} filtered`,
                            ]
                              .filter(Boolean)
                              .join(", ")
                          : `${item.totalCount} ${item.totalCount === 1 ? "capcode" : "capcodes"}`}
                      </Text>
                    </View>
                    <View style={styles.agencyRight}>
                      {hasActiveItems && <View style={styles.activeBadge} />}
                      <AppIcon
                        sf="chevron.right" ion="chevron-forward"
                        size={16}
                        color={theme.colors.textTertiary}
                      />
                    </View>
                  </TouchableOpacity>
                )
              })}
            </View>
          </ScrollView>
        )}

        {/* Banner Ad - conditionally rendered at bottom */}
        <BannerAd />
      </View>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.backgroundTertiary,
      flex: 1,
    },
    searchContainer: {
      margin: 16,
      marginTop: 8,
      borderRadius: 0,
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
      flexDirection: "row",
      paddingHorizontal: 12,
      paddingVertical: 12,
    },
    searchIcon: {
      marginRight: 8,
    },
    searchInput: {
      flex: 1,
      fontSize: 16,
      color: theme.colors.textPrimary,
    },
    loadingContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loadingText: {
      marginTop: 16,
      fontSize: 16,
      color: theme.colors.textSecondary,
      fontWeight: "400",
    },
    emptyContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 32,
      paddingTop: 60,
    },
    emptyText: {
      fontSize: 18,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
      marginBottom: 8,
    },
    emptySubtext: {
      fontSize: 15,
      color: theme.colors.textSecondary,
      textAlign: "center",
      lineHeight: 20,
    },
    sectionHeader: {
      paddingHorizontal: 16,
      paddingTop: 20,
      paddingBottom: 6,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    sectionTitle: {
      fontSize: 13,
      fontWeight: "400",
      color: theme.colors.textSecondary,
      letterSpacing: 0.4,
    },
    sectionContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      borderRadius: 0,
      overflow: "hidden",
      marginBottom: 20,
    },
    scrollView: {
      flex: 1,
    },
    agencyItem: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 11,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    lastAgencyItem: {
      borderBottomWidth: 0,
    },
    agencyContent: {
      flex: 1,
    },
    agencyName: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 4,
    },
    agencyCount: {
      fontSize: 14,
      color: theme.colors.textSecondary,
    },
    agencyRight: {
      flexDirection: "row",
      alignItems: "center",
    },
    activeBadge: {
      width: 8,
      height: 8,
      borderRadius: 4,
      backgroundColor: theme.colors.primary,
      marginRight: 8,
    },
    clearButton: {
      paddingLeft: 8,
      paddingVertical: 4,
    },
    bottomPadding: {
      height: 100, // Space for banner ad
    },
  })
