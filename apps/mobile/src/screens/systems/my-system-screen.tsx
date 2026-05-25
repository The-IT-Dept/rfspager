import React, { FC } from "react"
import {
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  Share,
  StyleSheet,
  Text,
  View,
} from "react-native"
import * as Clipboard from "expo-clipboard"
import { Ionicons } from "@expo/vector-icons"
import moment from "moment"
import { Stack } from "expo-router"
import { useMySystems } from "../../hooks"
import type { MySystem } from "../../services/api-client"
import { AppTheme, useAppTheme } from "../../theme"

export type MySystemScreenProps = {
  uuid?: string
}

export const MySystemScreen: FC<MySystemScreenProps> = function MySystemScreen({ uuid }) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const { data, isLoading } = useMySystems()

  if (isLoading) {
    return (
      <View style={styles.loader}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
      </View>
    )
  }

  const list = data ?? []
  const system = uuid ? list.find((s) => s.uuid === uuid) : list[0]

  if (!system) {
    return (
      <View style={styles.empty}>
        <Stack.Screen options={{ title: "My system" }} />
        <Text style={styles.emptyTitle}>System not found</Text>
        <Text style={styles.emptyBody}>
          {list.length === 0
            ? "Once you register interest, your system will show up here with its install command."
            : "You don't own a system with that ID, or it was removed."}
        </Text>
      </View>
    )
  }

  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.scroll}>
      <Stack.Screen options={{ title: system.name || "My system" }} />
      <SystemCard system={system} theme={theme} />
    </ScrollView>
  )
}

function SystemCard({ system, theme }: { system: MySystem; theme: AppTheme }) {
  const styles = makeStyles(theme)

  const copy = async (label: string, value: string) => {
    await Clipboard.setStringAsync(value)
    Alert.alert("Copied", `${label} copied to clipboard.`)
  }

  return (
    <View style={styles.card}>
      <View style={styles.cardHeaderRow}>
        <Text style={styles.cardTitle}>{system.name}</Text>
        {system.is_pending ? (
          <View style={[styles.chip, { backgroundColor: theme.colors.warning + "20" }]}>
            <Text style={[styles.chipText, { color: theme.colors.warning }]}>Pending</Text>
          </View>
        ) : (
          <View style={[styles.chip, { backgroundColor: theme.colors.success + "20" }]}>
            <Text style={[styles.chipText, { color: theme.colors.success }]}>Approved</Text>
          </View>
        )}
      </View>

      <Row label="Suburb" value={system.suburb || "—"} theme={theme} />
      <Row label="UUID" value={system.uuid} mono theme={theme} onCopy={() => copy("UUID", system.uuid)} />
      <Row
        label="Submitted"
        value={moment(system.created_at).format("D MMM YYYY, h:mm a")}
        theme={theme}
      />
      {system.approved_at && (
        <Row
          label="Approved"
          value={moment(system.approved_at).format("D MMM YYYY, h:mm a")}
          theme={theme}
        />
      )}
      {system.last_message_at && (
        <Row
          label="Last message"
          value={moment(system.last_message_at).fromNow()}
          theme={theme}
        />
      )}

      {system.is_pending ? (
        <View style={styles.pendingBox}>
          <Ionicons name="hourglass-outline" size={20} color={theme.colors.warning} />
          <Text style={styles.pendingText}>
            Your request is in the admin queue. We'll send you a push when it's approved and your
            install command appears here.
          </Text>
        </View>
      ) : (
        <View style={styles.installBlock}>
          <Text style={styles.installLabel}>API key</Text>
          <Pressable
            style={styles.codeBox}
            onPress={() => system.api_key && copy("API key", system.api_key)}
          >
            <Text style={styles.codeText} selectable>
              {system.api_key}
            </Text>
            <Ionicons name="copy-outline" size={16} color={theme.colors.textTertiary} />
          </Pressable>

          <Text style={styles.installLabel}>Install command</Text>
          <Pressable
            style={styles.codeBox}
            onPress={() => system.install_command && copy("Install command", system.install_command)}
          >
            <Text style={[styles.codeText, { fontSize: 12 }]} selectable>
              {system.install_command}
            </Text>
            <Ionicons name="copy-outline" size={16} color={theme.colors.textTertiary} />
          </Pressable>

          <Pressable
            style={styles.shareRow}
            onPress={() => system.install_command && Share.share({ message: system.install_command })}
          >
            <Ionicons name="share-outline" size={16} color={theme.colors.interactive} />
            <Text style={styles.shareText}>Share command</Text>
          </Pressable>

          <Text style={styles.installHint}>
            Run this on your Raspberry Pi over SSH. It pulls the ingest agent, registers it under
            your api key, and starts the service on boot.
          </Text>
        </View>
      )}
    </View>
  )
}

function Row({
  label,
  value,
  mono,
  theme,
  onCopy,
}: {
  label: string
  value: string
  mono?: boolean
  theme: AppTheme
  onCopy?: () => void
}) {
  const styles = makeStyles(theme)
  return (
    <Pressable style={styles.row} onPress={onCopy} disabled={!onCopy}>
      <Text style={styles.rowLabel}>{label}</Text>
      <Text
        style={[styles.rowValue, mono && { fontFamily: "Menlo", fontSize: 13 }]}
        numberOfLines={1}
        ellipsizeMode="middle"
      >
        {value}
      </Text>
    </Pressable>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    scroll: {
      padding: 16,
      paddingBottom: 48,
    },
    loader: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.background,
    },
    empty: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      padding: 32,
      backgroundColor: theme.colors.background,
    },
    emptyTitle: {
      fontSize: 18,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 6,
    },
    emptyBody: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      textAlign: "center",
      lineHeight: 20,
    },
    card: {
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 14,
      padding: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: theme.colors.border,
    },
    cardHeaderRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      marginBottom: 12,
    },
    cardTitle: {
      fontSize: 18,
      fontWeight: "700",
      color: theme.colors.textPrimary,
      flex: 1,
      marginRight: 8,
    },
    chip: {
      paddingHorizontal: 10,
      paddingVertical: 3,
      borderRadius: 10,
    },
    chipText: {
      fontSize: 11,
      fontWeight: "700",
      letterSpacing: 0.3,
    },
    row: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "center",
      paddingVertical: 8,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    rowLabel: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      width: 110,
    },
    rowValue: {
      flex: 1,
      fontSize: 14,
      color: theme.colors.textPrimary,
      textAlign: "right",
    },
    pendingBox: {
      marginTop: 16,
      backgroundColor: theme.colors.warning + "15",
      borderRadius: 10,
      padding: 12,
      flexDirection: "row",
      gap: 10,
    },
    pendingText: {
      flex: 1,
      fontSize: 13,
      color: theme.colors.textSecondary,
      lineHeight: 18,
    },
    installBlock: {
      marginTop: 14,
    },
    installLabel: {
      fontSize: 12,
      fontWeight: "700",
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.4,
      marginTop: 10,
      marginBottom: 6,
    },
    codeBox: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 8,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    codeText: {
      flex: 1,
      fontFamily: "Menlo",
      fontSize: 13,
      color: theme.colors.textPrimary,
    },
    shareRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      marginTop: 10,
    },
    shareText: {
      fontSize: 14,
      color: theme.colors.interactive,
      fontWeight: "600",
    },
    installHint: {
      marginTop: 12,
      fontSize: 13,
      color: theme.colors.textTertiary,
      lineHeight: 18,
    },
  })
