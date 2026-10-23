/* eslint-disable react-native/no-inline-styles */
import React, { FC, useCallback, useMemo, useState } from "react"
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native"
import { AppIcon } from "../../components/app-icon/app-icon"
import { Stack, useRouter } from "expo-router"

import { useAppTheme, AppTheme } from "../../theme"
import { useCapcodesData } from "../profile-capcodes/use-capcodes-data"
import { useUnknownCapcodes } from "../../hooks"
import { suggestCapcodeHref } from "./suggest-href"

type Tab = "agencies" | "unknown"

export const CapcodesScreen: FC = function CapcodesScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const [tab, setTab] = useState<Tab>("agencies")
  const [search, setSearch] = useState("")

  const { groupedData, loading, initialLoaded } = useCapcodesData()
  const unknownQuery = useUnknownCapcodes()
  const unknowns = unknownQuery.data?.data ?? []

  const filteredAgencies = useMemo(() => {
    if (!search.trim()) return groupedData
    const s = search.toLowerCase()
    return groupedData.filter((g) => g.agency.toLowerCase().includes(s))
  }, [groupedData, search])

  const filteredUnknowns = useMemo(() => {
    if (!search.trim()) return unknowns
    const s = search.toLowerCase()
    return unknowns.filter(
      (u) =>
        u.address.toLowerCase().includes(s) ||
        (u.agency_name ?? "").toLowerCase().includes(s),
    )
  }, [unknowns, search])

  const handleAgencyPress = useCallback(
    (item: (typeof groupedData)[0]) => {
      router.push(
        `/settings/capcode-detail?agencyId=${item.agencyId}&agency=${encodeURIComponent(item.agency)}`,
      )
    },
    [router],
  )

  const handleUnknownPress = useCallback(
    (u: (typeof unknowns)[0]) => {
      router.push(
        suggestCapcodeHref({
          capcode_id: u.capcode_id,
          address: u.address,
          agency: u.agency_name,
        }),
      )
    },
    [router],
  )

  const initialAgenciesLoading = !initialLoaded || loading
  const showLoading =
    (tab === "agencies" && initialAgenciesLoading) ||
    (tab === "unknown" && unknownQuery.isLoading)

  return (
    <View style={styles.container}>
      <Stack.Screen
        options={{
          headerSearchBarOptions: {
            placeholder:
              tab === "agencies" ? "Search agencies…" : "Search unknown capcodes…",
            onChangeText: (e: any) => setSearch(e?.nativeEvent?.text ?? ""),
            onClose: () => setSearch(""),
            autoCapitalize: "none",
          },
        }}
      />

      <ScrollView
        style={styles.scrollView}
        showsVerticalScrollIndicator={false}
        contentInsetAdjustmentBehavior="automatic"
        contentContainerStyle={{ paddingBottom: 40 }}
      >
        {/* Segmented tab control */}
        <View style={styles.segmentWrapper}>
          <View style={styles.segment}>
            <TouchableOpacity
              activeOpacity={0.7}
              onPress={() => setTab("agencies")}
              style={[styles.segmentItem, tab === "agencies" && styles.segmentItemActive]}
            >
              <Text style={[styles.segmentText, tab === "agencies" && styles.segmentTextActive]}>
                Agencies
              </Text>
            </TouchableOpacity>
            <TouchableOpacity
              activeOpacity={0.7}
              onPress={() => setTab("unknown")}
              style={[styles.segmentItem, tab === "unknown" && styles.segmentItemActive]}
            >
              <Text style={[styles.segmentText, tab === "unknown" && styles.segmentTextActive]}>
                Unknown
                {unknowns.length > 0 ? `  ·  ${unknowns.length}` : ""}
              </Text>
            </TouchableOpacity>
          </View>
        </View>

        {showLoading ? (
          <View style={styles.loadingContainer}>
            <ActivityIndicator size="large" color={theme.colors.primary} />
            <Text style={styles.loadingText}>
              Loading {tab === "agencies" ? "capcodes" : "unknown addresses"}…
            </Text>
          </View>
        ) : tab === "agencies" ? (
          <AgenciesList
            theme={theme}
            styles={styles}
            data={filteredAgencies}
            onPress={handleAgencyPress}
            searchActive={!!search.trim()}
          />
        ) : (
          <UnknownsList
            theme={theme}
            styles={styles}
            data={filteredUnknowns}
            onPress={handleUnknownPress}
            searchActive={!!search.trim()}
          />
        )}
      </ScrollView>
    </View>
  )
}

function AgenciesList({
  styles,
  theme,
  data,
  onPress,
  searchActive,
}: {
  styles: ReturnType<typeof makeStyles>
  theme: AppTheme
  data: ReturnType<typeof useCapcodesData>["groupedData"]
  onPress: (g: any) => void
  searchActive: boolean
}) {
  if (data.length === 0) {
    return (
      <View style={styles.emptyContainer}>
        <Text style={styles.emptyText}>
          {searchActive ? "No agencies found" : "No capcodes available"}
        </Text>
        <Text style={styles.emptySubtext}>
          {searchActive ? "Try adjusting your search" : "Check back later"}
        </Text>
      </View>
    )
  }
  return (
    <>
      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle}>AGENCIES</Text>
      </View>
      <View style={styles.sectionContainer}>
        {data.map((item, index) => {
          const isLast = index === data.length - 1
          const hasActive = item.subscribedCount > 0 || item.filteredCount > 0
          return (
            <TouchableOpacity
              key={item.agency}
              style={[styles.agencyItem, isLast && styles.lastAgencyItem]}
              onPress={() => onPress(item)}
              activeOpacity={0.7}
            >
              <View style={styles.agencyContent}>
                <Text style={styles.agencyName}>{item.agency}</Text>
                <Text style={styles.agencyCount}>
                  {hasActive
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
                {hasActive && <View style={styles.activeBadge} />}
                <AppIcon sf="chevron.right" ion="chevron-forward" size={16} color={theme.colors.textTertiary} />
              </View>
            </TouchableOpacity>
          )
        })}
      </View>
    </>
  )
}

function UnknownsList({
  styles,
  theme,
  data,
  onPress,
  searchActive,
}: {
  styles: ReturnType<typeof makeStyles>
  theme: AppTheme
  data: { capcode_id: number | null; address: string; agency_name: string | null; message_count: number; last_seen_at: string }[]
  onPress: (u: any) => void
  searchActive: boolean
}) {
  if (data.length === 0) {
    return (
      <View style={styles.emptyContainer}>
        <Text style={styles.emptyText}>
          {searchActive ? "Nothing matches" : "All clear"}
        </Text>
        <Text style={styles.emptySubtext}>
          {searchActive
            ? "Try adjusting your search"
            : "No unlabelled capcodes from the last 30 days."}
        </Text>
      </View>
    )
  }
  return (
    <>
      <View style={styles.hintCard}>
        <Text style={styles.hintText}>
          These addresses appeared in recent pager messages but we don't have a
          name for them yet. If you recognise one — a brigade, a unit, a
          station — tap to suggest a label.
        </Text>
      </View>

      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle}>UNIDENTIFIED ADDRESSES</Text>
      </View>
      <View style={styles.sectionContainer}>
        {data.map((u, index) => {
          const isLast = index === data.length - 1
          return (
            <TouchableOpacity
              key={`${u.capcode_id ?? "x"}-${u.address}`}
              style={[styles.agencyItem, isLast && styles.lastAgencyItem]}
              onPress={() => onPress(u)}
              activeOpacity={0.7}
            >
              <View style={styles.unknownLeft}>
                <Text style={styles.unknownAddress}>{u.address}</Text>
                <Text style={styles.agencyCount}>
                  {u.agency_name ? `${u.agency_name} · ` : ""}
                  {u.message_count} {u.message_count === 1 ? "message" : "messages"} · {timeAgo(u.last_seen_at)}
                </Text>
              </View>
              <AppIcon sf="chevron.right" ion="chevron-forward" size={16} color={theme.colors.textTertiary} />
            </TouchableOpacity>
          )
        })}
      </View>
    </>
  )
}

function timeAgo(s: string): string {
  const mins = Math.floor((Date.now() - new Date(s).getTime()) / 60000)
  if (mins < 1) return "just now"
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.backgroundTertiary,
      flex: 1,
    },
    scrollView: {
      flex: 1,
    },
    loadingContainer: {
      paddingVertical: 60,
      alignItems: "center",
    },
    loadingText: {
      marginTop: 16,
      fontSize: 16,
      color: theme.colors.textSecondary,
      fontWeight: "400",
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
    // Tabs
    segmentWrapper: {
      paddingHorizontal: 16,
      paddingTop: 16,
      paddingBottom: 4,
    },
    segment: {
      flexDirection: "row",
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 9,
      padding: 2,
    },
    segmentItem: {
      flex: 1,
      paddingVertical: 7,
      paddingHorizontal: 12,
      borderRadius: 7,
      alignItems: "center",
    },
    segmentItemActive: {
      backgroundColor: theme.colors.backgroundTertiary,
      shadowColor: "rgba(0,0,0,0.12)",
      shadowOpacity: 1,
      shadowRadius: 2,
      shadowOffset: { width: 0, height: 1 },
    },
    segmentText: {
      fontSize: 13,
      fontWeight: "500",
      color: theme.colors.textSecondary,
    },
    segmentTextActive: {
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    // Reused agency-list visual language
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
    // Unknown-specific
    hintCard: {
      marginHorizontal: 16,
      marginTop: 12,
      paddingHorizontal: 14,
      paddingVertical: 12,
      backgroundColor: "#FFF7ED",
      borderRadius: 10,
      borderWidth: 0.5,
      borderColor: "#FBD9B6",
    },
    hintText: {
      fontSize: 13,
      lineHeight: 18,
      color: "#7C3300",
    },
    unknownLeft: {
      flex: 1,
    },
    unknownAddress: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 4,
      fontVariant: ["tabular-nums"],
    },
  })
