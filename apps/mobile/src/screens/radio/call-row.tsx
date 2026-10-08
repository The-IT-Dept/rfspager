import React, { memo, useSyncExternalStore } from "react"
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from "react-native"
import Svg, { Circle } from "react-native-svg"
import { Ionicons } from "@expo/vector-icons"

import type { RadioCall } from "../../services/api-client"
import type { ProgressStore } from "../../services/radio/clip-player"
import { AppTheme, useAppTheme } from "../../theme"
import {
  agencyColour,
  agencyName,
  callSeconds,
  callTitle,
  clockTime,
  formatSeconds,
} from "./radio-format"

/**
 * One transmission, built like the pager feed's rows: whose channel it was,
 * who keyed up, and what was said, with the recording one tap away.
 */
export const CallRow = memo(function CallRow({
  call,
  active,
  playing,
  loading,
  failed,
  progress,
  onToggle,
}: {
  call: RadioCall
  active: boolean
  playing: boolean
  loading: boolean
  failed: boolean
  progress: ProgressStore
  onToggle: (c: RadioCall) => void
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const secs = callSeconds(call)
  const agency = agencyName(call.tg_agency)
  const talker = call.source_alias || (call.source_rid ? `Radio ${call.source_rid}` : null)
  const meta = [talker, secs != null && secs > 0 ? formatSeconds(secs) : null]
    .filter(Boolean)
    .join(", ")

  return (
    <View style={styles.container}>
      <View style={[styles.stripe, { backgroundColor: agencyColour(call.tg_agency) }]} />
      <PlayButton
        call={call}
        active={active}
        playing={playing}
        loading={loading}
        progress={progress}
        onToggle={onToggle}
        theme={theme}
      />
      <View style={styles.content}>
        <View style={styles.titleRow}>
          <Text style={styles.title} numberOfLines={1}>
            {callTitle(call)}
          </Text>
          <Text style={styles.time}>{clockTime(call.started_at)}</Text>
        </View>
        {(agency || meta || call.emergency) && (
          <View style={styles.metaRow}>
            <Text style={styles.meta} numberOfLines={1}>
              {[agency, meta].filter(Boolean).join(" · ")}
            </Text>
            {call.emergency && (
              <View style={styles.emergency}>
                <Ionicons name="warning" size={11} color={theme.colors.primary} />
                <Text style={styles.emergencyText}>Emergency</Text>
              </View>
            )}
          </View>
        )}
        <Transcript call={call} failed={failed} styles={styles} theme={theme} />
      </View>
    </View>
  )
})

function Transcript({
  call,
  failed,
  styles,
  theme,
}: {
  call: RadioCall
  failed: boolean
  styles: ReturnType<typeof makeStyles>
  theme: AppTheme
}) {
  if (call.encrypted) {
    return (
      <View style={styles.stateRow}>
        <Ionicons name="lock-closed" size={12} color={theme.colors.textTertiary} />
        <Text style={[styles.stateText, styles.stateTextInline]}>
          Encrypted. This transmission can't be heard.
        </Text>
      </View>
    )
  }
  if (failed) return <Text style={styles.stateText}>Couldn't load this recording. Try again.</Text>
  if (call.transcript) return <Text style={styles.transcript}>“{call.transcript}”</Text>
  if (call.transcript_error === "poor_audio") return <Text style={styles.stateText}>Too broken up to transcribe.</Text>
  if (call.transcribed_at) return <Text style={styles.stateText}>No speech recognised.</Text>
  if (!call.ended_at) return <Text style={styles.stateText}>On air now.</Text>
  if (call.audio_url) return <Text style={styles.stateText}>Transcribing…</Text>
  return <Text style={styles.stateText}>Not recorded.</Text>
}

const SIZE = 36
const R = 16
const CIRC = 2 * Math.PI * R

function PlayButton({
  call,
  active,
  playing,
  loading,
  progress,
  onToggle,
  theme,
}: {
  call: RadioCall
  active: boolean
  playing: boolean
  loading: boolean
  progress: ProgressStore
  onToggle: (c: RadioCall) => void
  theme: AppTheme
}) {
  if (!call.audio_url) return <View style={{ width: SIZE, marginRight: 12 }} />
  const bg = active ? theme.colors.textPrimary : theme.colors.backgroundTertiary
  const fg = active ? theme.colors.background : theme.colors.textPrimary
  const title = callTitle(call)
  return (
    <Pressable
      onPress={() => onToggle(call)}
      hitSlop={8}
      accessibilityRole="button"
      accessibilityLabel={active && playing ? `Pause ${title}` : `Play ${title}`}
      style={({ pressed }) => [
        { width: SIZE, height: SIZE, borderRadius: SIZE / 2, marginRight: 12, marginTop: 2 },
        {
          backgroundColor: bg,
          alignItems: "center",
          justifyContent: "center",
          opacity: pressed ? 0.7 : 1,
        },
      ]}
    >
      {active && <ProgressRing progress={progress} />}
      {active && loading ? (
        <ActivityIndicator size="small" color={fg} />
      ) : (
        <Ionicons
          name={active && playing ? "pause" : "play"}
          size={15}
          color={fg}
          style={active && playing ? undefined : { marginLeft: 2 }}
        />
      )}
    </Pressable>
  )
}

/** Only the active row subscribes to playback progress. */
function ProgressRing({ progress }: { progress: ProgressStore }) {
  const value = useSyncExternalStore(progress.subscribe, progress.get)
  return (
    <Svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 36 36"
      style={StyleSheet.absoluteFill}
      pointerEvents="none"
    >
      <Circle
        cx={18}
        cy={18}
        r={R}
        fill="none"
        stroke="#F7241D"
        strokeWidth={2.5}
        strokeDasharray={`${CIRC} ${CIRC}`}
        strokeDashoffset={CIRC * (1 - value)}
        strokeLinecap="round"
        transform="rotate(-90 18 18)"
      />
    </Svg>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.cardBackground,
      flexDirection: "row",
      paddingLeft: 16,
      paddingRight: 16,
      paddingVertical: 12,
      marginBottom: StyleSheet.hairlineWidth,
    },
    stripe: {
      position: "absolute",
      left: 0,
      top: 0,
      bottom: 0,
      width: 3,
    },
    content: {
      flex: 1,
      minWidth: 0,
    },
    titleRow: {
      flexDirection: "row",
      alignItems: "baseline",
    },
    title: {
      flex: 1,
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    time: {
      marginLeft: 8,
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontVariant: ["tabular-nums"],
    },
    metaRow: {
      flexDirection: "row",
      alignItems: "center",
      marginTop: 1,
    },
    meta: {
      flexShrink: 1,
      fontSize: 13,
      color: theme.colors.textTertiary,
    },
    emergency: {
      flexDirection: "row",
      alignItems: "center",
      marginLeft: 8,
      paddingHorizontal: 6,
      paddingVertical: 1,
      borderRadius: 4,
      backgroundColor: theme.dark ? "rgba(247, 36, 29, 0.2)" : "#FEF2F2",
    },
    emergencyText: {
      marginLeft: 3,
      fontSize: 12,
      fontWeight: "600",
      color: theme.colors.primary,
    },
    transcript: {
      marginTop: 5,
      fontSize: 15,
      lineHeight: 21,
      color: theme.colors.textPrimary,
    },
    stateRow: {
      flexDirection: "row",
      alignItems: "center",
      marginTop: 5,
    },
    stateText: {
      marginTop: 5,
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
    stateTextInline: {
      marginTop: 0,
      marginLeft: 5,
      flexShrink: 1,
    },
  })
