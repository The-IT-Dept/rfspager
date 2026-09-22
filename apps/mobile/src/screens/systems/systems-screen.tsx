import React, { FC, useCallback } from "react"
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from "react-native"
import moment from "moment"
import { AppIcon } from "../../components/app-icon/app-icon"
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

  const items = data ?? []

  const renderItem = useCallback(
    ({ item, index }: { item: System; index: number }) => {
      const online = isOnline(item.last_message_at)
      const pending = !!item.is_pending
      const owner = !!item.is_owner
      const isLast = index === items.length - 1

      const statusColor = pending
        ? theme.colors.warning
        : online
          ? theme.colors.success
          : theme.colors.textTertiary
      const statusLabel = pending ? "Pending" : online ? "Online" : "Offline"

      return (
        <Pressable
          style={({ pressed }) => [
            styles.row,
            !isLast && styles.rowDivider,
            pressed && styles.rowPressed,
          ]}
          onPress={() => router.push(`/system/${item.uuid}`)}
        >
          <View style={[styles.dot, { backgroundColor: statusColor }]} />
          <View style={styles.rowContent}>
            <View style={styles.titleRow}>
              <Text style={styles.name} numberOfLines={1}>
                {item.name || "Unnamed system"}
              </Text>
              {owner && (
                <View style={[styles.chip, { backgroundColor: theme.colors.primary + "15" }]}>
                  <Text style={[styles.chipText, { color: theme.colors.primary }]}>OWNER</Text>
                </View>
              )}
            </View>
            <Text style={styles.subline} numberOfLines={1} ellipsizeMode="middle">
              {item.suburb ? `${item.suburb} · ` : ""}
              {item.uuid}
            </Text>
          </View>
          <View style={styles.rowRight}>
            <Text style={[styles.status, { color: statusColor }]}>{statusLabel}</Text>
            <Text style={styles.timestamp}>{formatLastMessage(item.last_message_at)}</Text>
          </View>
          <AppIcon
            sf="chevron.right" ion="chevron-forward"
            size={16}
            color={theme.colors.textTertiary}
            style={styles.chevron}
          />
        </Pressable>
      )
    },
    [items.length, router, styles, theme],
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
      style={styles.container}
      data={items}
      keyExtractor={(item) => item.uuid}
      renderItem={renderItem}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={styles.listContent}
      ListHeaderComponent={
        <View style={styles.headerSection}>
          <Pressable
            style={({ pressed }) => [styles.actionRow, pressed && styles.rowPressed]}
            onPress={() => router.push("/systems-create")}
          >
            <AppIcon sf="plus.circle" ion="add-circle-outline" size={20} color={theme.colors.primary} />
            <Text style={styles.actionText}>Register interest to host a system</Text>
            <AppIcon sf="chevron.right" ion="chevron-forward" size={16} color={theme.colors.textTertiary} />
          </Pressable>
        </View>
      }
      ListEmptyComponent={
        <View style={styles.emptySection}>
          <Text style={styles.emptyTitle}>No systems yet</Text>
          <Text style={styles.emptySubtitle}>
            Systems appear here as they come online. Register interest above to host one yourself.
          </Text>
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
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    listContent: {
      paddingBottom: 32,
    },
    headerSection: {
      marginTop: 16,
      marginHorizontal: 16,
      backgroundColor: theme.colors.cardBackground,
      marginBottom: 24,
      overflow: "hidden",
    },
    actionRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 16,
      paddingVertical: 14,
    },
    actionText: {
      flex: 1,
      fontSize: 15,
      fontWeight: "500",
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
    rowDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
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
    rowContent: {
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
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 3,
    },
    chipText: {
      fontSize: 10,
      fontWeight: "700",
      letterSpacing: 0.4,
    },
    subline: {
      fontSize: 12,
      color: theme.colors.textTertiary,
      marginTop: 2,
      fontFamily: "Menlo",
    },
    rowRight: {
      alignItems: "flex-end",
      marginRight: 6,
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
    chevron: {
      marginLeft: 2,
    },
    loaderContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loaderText: {
      marginTop: 16,
      fontSize: 15,
      color: theme.colors.textSecondary,
    },
    emptySection: {
      marginHorizontal: 16,
      paddingVertical: 40,
      paddingHorizontal: 24,
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
    },
    emptyTitle: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 6,
    },
    emptySubtitle: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      textAlign: "center",
      lineHeight: 20,
    },
  })
