/* eslint-disable react-native/no-inline-styles */

import React, { FC, useState, useMemo, useCallback } from "react"
import { StyleSheet, View, ScrollView } from "react-native"
import { Text, ActivityIndicator } from "react-native"
import { Capcode } from "../../components"
import { useAgencyCapcodes } from "./use-capcodes-data"
import type { Capcode as CapcodeType } from "../../services/api-client"
import { BannerAd } from "../../utils/admob"
import { Stack, useLocalSearchParams } from "expo-router"
import { useAppTheme, AppTheme } from "../../theme"

export const CapcodesDetailScreen: FC =
  function CapcodesDetailScreen() {
    const { agencyId } = useLocalSearchParams<{ agencyId: string; agency: string }>()
    const theme = useAppTheme()
    const styles = makeStyles(theme)
    const [search, setSearch] = useState<string>("")

    const { capcodes: allCapcodes, loading } = useAgencyCapcodes(Number(agencyId))

    const agencyCapcodes = useMemo(() => {
      if (!search.trim()) return allCapcodes
      const searchLower = search.toLowerCase()
      return allCapcodes.filter(
        (c) => c.alias?.toLowerCase().includes(searchLower) || c.address?.toLowerCase().includes(searchLower),
      )
    }, [allCapcodes, search])

    const initialLoaded = !loading

    const renderCapcodeItem = useCallback(
      (capcode: CapcodeType, index: number) => {
        const isLastItem = index === agencyCapcodes.length - 1
        return (
          <Capcode key={capcode.id} capcode={capcode} editable={false} isLastItem={isLastItem} />
        )
      },
      [agencyCapcodes.length],
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
              placeholder: "Search in agency...",
              onChangeText: (e: any) => setSearch(e?.nativeEvent?.text ?? ""),
              onClose: () => setSearch(""),
              autoCapitalize: "none",
            },
          }}
        />

        {/* Capcodes List */}
        <ScrollView
          style={styles.scrollView}
          showsVerticalScrollIndicator={false}
          contentInsetAdjustmentBehavior="automatic"
        >
          {agencyCapcodes.length > 0 && (
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>CAPCODES</Text>
            </View>
          )}
          <View style={styles.sectionContainer}>
            {agencyCapcodes.length > 0 ? (
              agencyCapcodes.map((capcode, index) => renderCapcodeItem(capcode, index))
            ) : (
              <View style={styles.emptyContainer}>
                <Text style={styles.emptyText}>
                  {search ? "No matching capcodes" : "No capcodes in this agency"}
                </Text>
                {search && <Text style={styles.emptySubtext}>Try adjusting your search</Text>}
              </View>
            )}
          </View>
        </ScrollView>

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
    emptyContainer: {
      paddingHorizontal: 32,
      paddingVertical: 60,
      alignItems: "center",
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
    clearButton: {
      paddingLeft: 8,
      paddingVertical: 4,
    },
    bottomPadding: {
      height: 100, // Space for banner ad
    },
  })
