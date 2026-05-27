import React, { FC, useState } from "react"
import {
  ActivityIndicator,
  Alert,
  Modal,
  Pressable,
  ScrollView,
  Share,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
} from "react-native"
import * as Clipboard from "expo-clipboard"
import { Ionicons } from "@expo/vector-icons"
import { Stack } from "expo-router"
import moment from "moment"

import { useSystem } from "../../hooks"
import { SpectrumWaterfall } from "../../components/spectrum-waterfall"
import { AppTheme, useAppTheme } from "../../theme"

export type SystemDetailScreenProps = {
  uuid: string
}

export const SystemDetailScreen: FC<SystemDetailScreenProps> = function SystemDetailScreen({
  uuid,
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const { data, isLoading, error } = useSystem(uuid)
  const [fullscreen, setFullscreen] = useState(false)

  if (isLoading) {
    return (
      <View style={styles.loader}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
      </View>
    )
  }
  if (error || !data) {
    return (
      <View style={styles.loader}>
        <Text style={styles.error}>Couldn't load system.</Text>
      </View>
    )
  }

  const freqMHz = data.frequency > 1000 ? (data.frequency / 1e6).toFixed(4) + " MHz" : null

  const copy = async (label: string, value: string) => {
    await Clipboard.setStringAsync(value)
    Alert.alert("Copied", `${label} copied to clipboard.`)
  }

  return (
    <ScrollView
      style={styles.scroll}
      contentContainerStyle={styles.content}
      contentInsetAdjustmentBehavior="automatic"
    >
      <Stack.Screen options={{ title: data.name }} />

      {/* Status pills row — sits inside the page padding, above the cards */}
      <View style={styles.statusRow}>
        {data.is_pending ? (
          <View style={[styles.pill, styles.pillPending]}>
            <Text style={styles.pillPendingText}>Pending approval</Text>
          </View>
        ) : (
          <View style={[styles.pill, data.online ? styles.pillOnline : styles.pillOffline]}>
            <Text style={data.online ? styles.pillOnlineText : styles.pillOfflineText}>
              {data.online ? "Online" : "Offline"}
            </Text>
          </View>
        )}
        {data.is_owner ? (
          <View style={styles.ownerPill}>
            <Text style={styles.ownerPillText}>Owner</Text>
          </View>
        ) : null}
        {data.suburb ? <Text style={styles.suburbText}>{data.suburb}</Text> : null}
      </View>

      <Section
        styles={styles}
        title="Spectrum"
        right={
          <Pressable
            onPress={() => setFullscreen(true)}
            hitSlop={8}
            style={styles.headerAction}
          >
            <Ionicons name="expand-outline" size={16} color={theme.colors.interactive} />
            <Text style={styles.headerActionText}>Fullscreen</Text>
          </Pressable>
        }
      >
        <View style={styles.spectrumBody}>
          <SpectrumWaterfall systemUuid={data.system_id} />
        </View>
      </Section>

      <SpectrumFullscreen
        visible={fullscreen}
        onClose={() => setFullscreen(false)}
        systemUuid={data.system_id}
      />

      <Section styles={styles} title="Connection">
        <Field styles={styles} label="Connected" value={fmtTime(data.connected_at)} />
        <Field styles={styles} label="Last ping" value={fmtTime(data.last_ping_at)} />
        {data.last_message_at ? (
          <Field
            styles={styles}
            label="Last message"
            value={moment(data.last_message_at).fromNow()}
          />
        ) : null}
        {freqMHz ? <Field styles={styles} label="Frequency" value={freqMHz} last /> : null}
      </Section>

      {data.is_owner && data.is_pending ? (
        <View style={styles.pendingBox}>
          <Ionicons name="hourglass-outline" size={20} color={theme.colors.warning} />
          <Text style={styles.pendingText}>
            Your request is in the admin queue. We'll send you a push when it's approved and your
            install command appears here.
          </Text>
        </View>
      ) : null}

      {data.is_owner && !data.is_pending && data.install_command ? (
        <Section styles={styles} title="Install">
          <View style={styles.installRow}>
            <Text style={styles.installLabel}>API key</Text>
            {data.api_key ? (
              <Pressable
                style={styles.codeBox}
                onPress={() => copy("API key", data.api_key!)}
              >
                <Text style={styles.codeText} selectable>
                  {data.api_key}
                </Text>
                <Ionicons name="copy-outline" size={16} color={theme.colors.textTertiary} />
              </Pressable>
            ) : null}
          </View>

          <View style={styles.installRow}>
            <Text style={styles.installLabel}>Install command</Text>
            <Pressable
              style={styles.codeBox}
              onPress={() => copy("Install command", data.install_command!)}
            >
              <Text style={[styles.codeText, styles.codeSmall]} selectable>
                {data.install_command}
              </Text>
              <Ionicons name="copy-outline" size={16} color={theme.colors.textTertiary} />
            </Pressable>
          </View>

          <Pressable
            style={styles.installAction}
            onPress={() => Share.share({ message: data.install_command! })}
          >
            <Ionicons name="share-outline" size={16} color={theme.colors.interactive} />
            <Text style={styles.installActionText}>Share command</Text>
          </Pressable>

          <Text style={styles.installHint}>
            Run this on your Raspberry Pi over SSH. It pulls the ingest agent, registers it under
            your api key, and starts the service on boot.
          </Text>
        </Section>
      ) : null}

      {data.is_owner && data.receiver ? (
        <Section styles={styles} title="Receiver hardware">
          <Field styles={styles} label="Hostname" value={data.receiver.hostname ?? "—"} />
          <Field styles={styles} label="Version" value={data.receiver.version ?? "—"} mono />
          <Field
            styles={styles}
            label="OS / arch"
            value={`${data.receiver.os ?? "—"} / ${data.receiver.arch ?? "—"}`}
          />
          <Field
            styles={styles}
            label="Pod"
            value={data.receiver.pod_id ?? "—"}
            mono
            last={!data.receiver.dongles?.length}
          />
          {data.receiver.dongles && data.receiver.dongles.length > 0 ? (
            <View style={styles.donglesBlock}>
              <Text style={styles.donglesHeader}>Dongles</Text>
              {data.receiver.dongles.map((d, i) => (
                <View
                  key={d.index}
                  style={[
                    styles.dongleRow,
                    i < data.receiver!.dongles!.length - 1 && styles.dongleRowDivider,
                  ]}
                >
                  <Text style={styles.dongleText}>
                    #{d.index} {d.name ?? "—"}
                    {d.tuner ? `  ·  ${d.tuner}` : ""}
                  </Text>
                  {d.serial ? <Text style={styles.dongleSerial}>{d.serial}</Text> : null}
                </View>
              ))}
            </View>
          ) : null}
        </Section>
      ) : null}

      {data.is_owner ? (
        <Section styles={styles} title="Lifecycle">
          {data.created_at ? (
            <Field
              styles={styles}
              label="Submitted"
              value={moment(data.created_at).format("D MMM YYYY, h:mm a")}
            />
          ) : null}
          {data.approved_at ? (
            <Field
              styles={styles}
              label="Approved"
              value={moment(data.approved_at).format("D MMM YYYY, h:mm a")}
            />
          ) : null}
          {data.request_notes ? (
            <Field styles={styles} label="Notes" value={data.request_notes} last />
          ) : null}
        </Section>
      ) : null}
    </ScrollView>
  )
}

type StyleMap = ReturnType<typeof makeStyles>

const Section: FC<{
  styles: StyleMap
  title: string
  right?: React.ReactNode
  children: React.ReactNode
}> = ({ styles, title, right, children }) => (
  <View style={styles.section}>
    <View style={styles.sectionHeader}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {right}
    </View>
    {children}
  </View>
)

const Field: FC<{
  styles: StyleMap
  label: string
  value: string
  mono?: boolean
  last?: boolean
}> = ({ styles, label, value, mono, last }) => (
  <View style={[styles.field, !last && styles.fieldDivider]}>
    <Text style={styles.fieldLabel}>{label}</Text>
    <Text style={[styles.fieldValue, mono && styles.mono]} numberOfLines={2}>
      {value}
    </Text>
  </View>
)

function SpectrumFullscreen({
  visible,
  onClose,
  systemUuid,
}: {
  visible: boolean
  onClose: () => void
  systemUuid: string
}) {
  const { width: w, height: h } = useWindowDimensions()
  // Render at "landscape" dims (swap w/h) then rotate the wrapper 90deg
  // so it visually fills the portrait screen sideways.
  const landWidth = h
  const landHeight = w
  return (
    <Modal visible={visible} animationType="fade" presentationStyle="overFullScreen" transparent>
      <View style={{ flex: 1, backgroundColor: "#000" }}>
        <View
          style={{
            position: "absolute",
            left: (w - landWidth) / 2,
            top: (h - landHeight) / 2,
            width: landWidth,
            height: landHeight,
            transform: [{ rotate: "90deg" }],
            justifyContent: "center",
            paddingHorizontal: 16,
          }}
        >
          <SpectrumWaterfall
            systemUuid={systemUuid}
            cols={128}
            rows={Math.max(48, Math.min(80, Math.floor((landHeight - 64) / 8)))}
            height={landHeight - 64}
            hideFooter
          />
        </View>
        <Pressable
          onPress={onClose}
          style={{
            position: "absolute",
            top: 50,
            right: 20,
            padding: 10,
            borderRadius: 999,
            backgroundColor: "rgba(255,255,255,0.15)",
          }}
        >
          <Ionicons name="close" size={22} color="#fff" />
        </Pressable>
        <Text
          style={{
            position: "absolute",
            bottom: 30,
            alignSelf: "center",
            color: "#888",
            fontSize: 12,
          }}
        >
          Rotate your phone for the proper view · tap × to close
        </Text>
      </View>
    </Modal>
  )
}

function fmtTime(s?: string | null): string {
  if (!s) return "—"
  return moment(s).fromNow()
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    scroll: { flex: 1, backgroundColor: theme.colors.backgroundTertiary },
    content: { paddingBottom: 32 },
    loader: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
    },
    error: { color: theme.colors.textPrimary },

    // Status header row (suburb + pills) — lives inside the page padding,
    // above the first grouped section.
    statusRow: {
      flexDirection: "row",
      alignItems: "center",
      flexWrap: "wrap",
      gap: 8,
      paddingHorizontal: 16,
      paddingTop: 12,
      paddingBottom: 4,
    },
    pill: {
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 999,
      borderWidth: 1,
    },
    pillOnline: {
      borderColor: theme.colors.success,
      backgroundColor: theme.colors.success + "20",
    },
    pillOffline: { borderColor: theme.colors.border, backgroundColor: "transparent" },
    pillPending: {
      borderColor: theme.colors.warning,
      backgroundColor: theme.colors.warning + "20",
    },
    pillOnlineText: { color: theme.colors.success, fontSize: 12, fontWeight: "600" },
    pillOfflineText: { color: theme.colors.textSecondary, fontSize: 12 },
    pillPendingText: { color: theme.colors.warning, fontSize: 12, fontWeight: "600" },
    ownerPill: {
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 999,
      borderWidth: 1,
      borderColor: theme.colors.interactive,
    },
    ownerPillText: { color: theme.colors.interactive, fontSize: 12, fontWeight: "600" },
    suburbText: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      marginLeft: 4,
    },

    // iOS-style grouped section
    section: {
      marginTop: 24,
      marginHorizontal: 16,
      backgroundColor: theme.colors.cardBackground,
      overflow: "hidden",
    },
    sectionHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingVertical: 10,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    sectionTitle: {
      fontSize: 13,
      fontWeight: "600",
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.5,
    },
    headerAction: {
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      paddingVertical: 2,
    },
    headerActionText: { color: theme.colors.interactive, fontSize: 13, fontWeight: "600" },

    field: {
      paddingHorizontal: 16,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "flex-start",
    },
    fieldDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    fieldLabel: {
      width: 110,
      fontSize: 14,
      color: theme.colors.textSecondary,
    },
    fieldValue: {
      flex: 1,
      fontSize: 14,
      color: theme.colors.textPrimary,
      textAlign: "right",
    },
    mono: { fontFamily: "Menlo" },

    // Spectrum body — the waterfall draws its own background so the section
    // only needs vertical breathing room.
    spectrumBody: {
      paddingVertical: 8,
    },

    installRow: {
      paddingHorizontal: 16,
      paddingVertical: 10,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    installLabel: {
      fontSize: 11,
      textTransform: "uppercase",
      color: theme.colors.textSecondary,
      letterSpacing: 0.5,
      marginBottom: 6,
    },
    codeBox: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      backgroundColor: theme.colors.backgroundTertiary,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    codeText: {
      flex: 1,
      fontFamily: "Menlo",
      fontSize: 13,
      color: theme.colors.textPrimary,
    },
    codeSmall: { fontSize: 12 },
    installAction: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    installActionText: {
      fontSize: 14,
      color: theme.colors.interactive,
      fontWeight: "600",
    },
    installHint: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      fontSize: 13,
      color: theme.colors.textTertiary,
      lineHeight: 18,
    },

    pendingBox: {
      marginTop: 16,
      marginHorizontal: 16,
      backgroundColor: theme.colors.warning + "15",
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

    donglesBlock: {
      paddingHorizontal: 16,
      paddingTop: 10,
      paddingBottom: 4,
    },
    donglesHeader: {
      fontSize: 11,
      textTransform: "uppercase",
      color: theme.colors.textSecondary,
      letterSpacing: 0.5,
      marginBottom: 6,
    },
    dongleRow: {
      paddingVertical: 8,
    },
    dongleRowDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    dongleText: { fontSize: 14, color: theme.colors.textPrimary },
    dongleSerial: {
      fontSize: 12,
      color: theme.colors.textSecondary,
      fontFamily: "Menlo",
      marginTop: 2,
    },
  })
