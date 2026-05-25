import React, { FC, useCallback } from "react"
import {
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  ActivityIndicator,
} from "react-native"
import moment from "moment"
import { Ionicons } from "@expo/vector-icons"
import { useRouter } from "expo-router"
import { useSystems } from "../../hooks"
import type { System } from "../../services/api-client"
import { AppTheme, useAppTheme } from "../../theme"

const ONLINE_THRESHOLD_MINUTES = 30

function isOnline(lastMessageAt: string | null): boolean {
  if (!lastMessageAt) return false
  return moment().diff(moment(lastMessageAt), "minutes") <= ONLINE_THRESHOLD_MINUTES
}

function formatLastMessage(lastMessageAt: string | null): string {
  if (!lastMessageAt) return "No messages yet"
  return moment(lastMessageAt).fromNow()
}

export const SystemsScreen: FC = function SystemsScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const { data, isLoading, refetch, isRefetching } = useSystems()

  const renderItem = useCallback(
    ({ item }: { item: System }) => {
      const online = isOnline(item.last_message_at)
      const pending = !!item.is_pending
      const owner = !!item.is_owner

      return (
        <Pressable
          style={({ pressed }) => [styles.row, pressed && styles.rowPressed]}
          onPress={() => {
            if (owner) router.push(`/(tabs)/(stack)/my-system/${item.uuid}` as any)
          }}
        >
          <View
            style={[
              styles.dot,
              {
                backgroundColor: pending
                  ? theme.colors.warning
                  : online
                    ? theme.colors.success
                    : theme.colors.textDisabled,
              },
            ]}
          />
          <View style={styles.content}>
            <View style={styles.titleRow}>
              <Text style={styles.name} numberOfLines={1}>
                {item.name || "Unnamed system"}
              </Text>
              {owner && (
                <View style={[styles.chip, { backgroundColor: theme.colors.primary + "15" }]}>
                  <Text style={[styles.chipText, { color: theme.colors.primary }]}>Owner</Text>
                </View>
              )}
              {pending && (
                <View style={[styles.chip, { backgroundColor: theme.colors.warning + "20" }]}>
                  <Text style={[styles.chipText, { color: theme.colors.warning }]}>Pending approval</Text>
                </View>
              )}
            </View>
            <Text style={styles.subline} numberOfLines={1} ellipsizeMode="middle">
              {item.suburb ? `${item.suburb} · ` : ""}
              {item.uuid}
            </Text>
          </View>
          <View style={styles.right}>
            <Text
              style={[
                styles.status,
                { color: pending ? theme.colors.warning : online ? theme.colors.success : theme.colors.textTertiary },
              ]}
            >
              {pending ? "Pending" : online ? "Online" : "Offline"}
            </Text>
            <Text style={styles.timestamp}>{formatLastMessage(item.last_message_at)}</Text>
          </View>
        </Pressable>
      )
    },
    [router, styles, theme],
  )

  if (isLoading) {
    return (
      <View style={styles.loaderContainer}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
        <Text style={styles.loaderText}>Loading systems…</Text>
      </View>
    )
  }

  return (
    <FlatList
      data={data ?? []}
      keyExtractor={(item) => item.uuid}
      renderItem={renderItem}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={styles.list}
      ItemSeparatorComponent={() => <View style={styles.separator} />}
      ListHeaderComponent={
        <View style={styles.headerActions}>
          <TouchableOpacity
            style={styles.createButton}
            onPress={() => router.push("/(tabs)/(stack)/systems-create" as any)}
          >
            <Ionicons name="add-circle-outline" size={18} color={theme.colors.primary} />
            <Text style={styles.createButtonText}>Register interest to host a system</Text>
            <Ionicons name="chevron-forward" size={16} color={theme.colors.textTertiary} />
          </TouchableOpacity>
        </View>
      }
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text style={styles.emptyTitle}>No systems yet</Text>
          <Text style={styles.emptySubtitle}>Systems will appear here once they're added.</Text>
        </View>
      }
      refreshControl={
        <RefreshControl
          refreshing={isRefetching}
          onRefresh={() => refetch()}
          tintColor={theme.colors.interactive}
        />
      }
    />
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    list: {
      paddingTop: 8,
      paddingBottom: 24,
    },
    headerActions: {
      marginHorizontal: 16,
      marginBottom: 12,
    },
    createButton: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 12,
      paddingHorizontal: 14,
      paddingVertical: 12,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: theme.colors.border,
    },
    createButtonText: {
      flex: 1,
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    row: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
    },
    rowPressed: {
      backgroundColor: theme.colors.backgroundTertiary,
    },
    dot: {
      width: 10,
      height: 10,
      borderRadius: 5,
      marginRight: 12,
    },
    content: {
      flex: 1,
      paddingRight: 12,
    },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      flexWrap: "wrap",
    },
    name: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    chip: {
      paddingHorizontal: 8,
      paddingVertical: 2,
      borderRadius: 10,
    },
    chipText: {
      fontSize: 11,
      fontWeight: "700",
      letterSpacing: 0.3,
    },
    subline: {
      fontSize: 12,
      color: theme.colors.textTertiary,
      marginTop: 2,
      fontFamily: "Menlo",
    },
    right: {
      alignItems: "flex-end",
    },
    status: {
      fontSize: 13,
      fontWeight: "600",
    },
    timestamp: {
      fontSize: 12,
      color: theme.colors.textTertiary,
      marginTop: 2,
    },
    separator: {
      height: StyleSheet.hairlineWidth,
      backgroundColor: theme.colors.border,
      marginHorizontal: 16,
    },
    loaderContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.background,
    },
    loaderText: {
      marginTop: 16,
      fontSize: 15,
      color: theme.colors.textSecondary,
    },
    empty: {
      paddingVertical: 80,
      paddingHorizontal: 32,
      alignItems: "center",
    },
    emptyTitle: {
      fontSize: 18,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 6,
    },
    emptySubtitle: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      textAlign: "center",
    },
  })
