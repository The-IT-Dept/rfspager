import React, { useEffect, useRef, useSyncExternalStore } from "react"
import { Animated, Pressable, StyleSheet, Text, View } from "react-native"
import Svg, { Rect } from "react-native-svg"
import { Ionicons } from "@expo/vector-icons"

import type { RadioCall, Talkgroup } from "../../services/api-client"
import type { LiveStream, LiveStreamsStore } from "../../services/radio/live-audio"
import type { StreamState } from "../../services/radio/radio-stream"
import { AppTheme, useAppTheme } from "../../theme"
import { agencyColour, callTitle } from "./radio-format"

export interface LiveControls {
  listening: boolean
  start: () => void
  stop: () => void
  muted: boolean
  setMuted: (m: boolean) => void
  error: string | null
  streams: LiveStreamsStore
}

/**
 * The head of the timeline: what's on air right now, and live listening.
 * While listening, the playing call carries a trace of its own audio level,
 * drawn from the decoded voice as it plays.
 */
export function OnAir({
  connection,
  active,
  live,
  talkgroups,
  filterLabel,
}: {
  connection: StreamState
  active: RadioCall[]
  live: LiveControls
  talkgroups: Talkgroup[]
  filterLabel: string
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const status =
    connection === "live"
      ? active.length
        ? `${active.length} on air`
        : "Quiet right now"
      : connection === "reconnecting"
        ? "Reconnecting"
        : "Connecting"
  const dot =
    connection === "live"
      ? active.length
        ? theme.colors.primary
        : theme.colors.textDisabled
      : theme.colors.warning

  return (
    <View style={styles.container}>
      <View style={styles.row}>
        <StatusDot colour={dot} pulsing={connection === "live" && active.length > 0} />
        <View style={styles.statusText}>
          <Text style={styles.status} numberOfLines={1}>
            {status}
          </Text>
          <Text style={styles.sub} numberOfLines={2}>
            {live.listening
              ? `Listening to ${filterLabel}.`
              : `Listen to hear ${filterLabel} as it's spoken.`}
          </Text>
        </View>
        {live.listening && (
          <Pressable
            onPress={() => live.setMuted(!live.muted)}
            accessibilityRole="button"
            accessibilityLabel={live.muted ? "Unmute" : "Mute"}
            hitSlop={6}
            style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
          >
            <Ionicons
              name={live.muted ? "volume-mute" : "volume-high"}
              size={18}
              color={theme.colors.textSecondary}
            />
          </Pressable>
        )}
        <Pressable
          onPress={live.listening ? live.stop : live.start}
          accessibilityRole="button"
          style={({ pressed }) => [
            styles.listenButton,
            live.listening ? styles.listenButtonOn : styles.listenButtonOff,
            pressed && styles.pressed,
          ]}
        >
          <Ionicons
            name={live.listening ? "stop" : "headset"}
            size={15}
            color={live.listening ? theme.colors.textPrimary : theme.colors.white}
          />
          <Text style={[styles.listenText, live.listening && styles.listenTextOn]}>
            {live.listening ? "Stop listening" : "Listen live"}
          </Text>
        </Pressable>
      </View>

      {live.error && <Text style={styles.error}>{live.error}</Text>}

      {live.listening && (
        <LiveStreams store={live.streams} talkgroups={talkgroups} styles={styles} />
      )}

      {!live.listening && active.length > 0 && (
        <View style={styles.chips}>
          {active.map((c) => (
            <View key={c.id} style={styles.chip}>
              <View style={[styles.chipStripe, { backgroundColor: agencyColour(c.tg_agency) }]} />
              <Text style={styles.chipText} numberOfLines={1}>
                {callTitle(c)}
              </Text>
              {c.encrypted && <Text style={styles.chipMuted}>encrypted</Text>}
            </View>
          ))}
        </View>
      )}
    </View>
  )
}

// The playing call (and the one that just finished, fading), then the next
// two in line; the rest of the queue is just a count.
const SHOW_WAITING = 2
function visibleStreams(streams: LiveStream[]) {
  const finished = streams.filter((s) => s.state === "done" && s.levels.length > 0)
  const playing = streams.filter((s) => s.state === "playing")
  const waiting = streams.filter((s) => s.state === "queued")
  return {
    shown: [...finished, ...playing, ...waiting.slice(0, SHOW_WAITING)],
    more: Math.max(0, waiting.length - SHOW_WAITING),
  }
}

/** Subscribes to the engine's ~12 Hz snapshots, so only this re-renders. */
function LiveStreams({
  store,
  talkgroups,
  styles,
}: {
  store: LiveStreamsStore
  talkgroups: Talkgroup[]
  styles: ReturnType<typeof makeStyles>
}) {
  const streams = useSyncExternalStore(store.subscribe, store.get)
  if (streams.length === 0) return null
  const tgByID = new Map(talkgroups.map((t) => [t.tgid, t]))
  const { shown, more } = visibleStreams(streams)
  return (
    <View style={styles.streams}>
      {shown.map((s) => {
        const tg = tgByID.get(s.tgid)
        return (
          <View key={s.callId} style={[styles.streamRow, s.state !== "playing" && styles.faded]}>
            <Text style={styles.streamName} numberOfLines={1}>
              {tg?.alpha_tag ?? `Talkgroup ${s.tgid}`}
            </Text>
            {s.state === "queued" ? (
              <Text style={styles.waiting}>Waiting, {s.bufferedS.toFixed(1)} s held</Text>
            ) : (
              <Trace levels={s.levels} colour={agencyColour(tg?.agency)} />
            )}
          </View>
        )
      })}
      {more > 0 && <Text style={styles.more}>{more} more waiting</Text>}
    </View>
  )
}

/** A scrolling bar trace of loudness: newest on the right, 20 ms per bar. */
const BARS = 64
function Trace({ levels, colour }: { levels: number[]; colour: string }) {
  const shown = levels.slice(-BARS)
  const pad = BARS - shown.length
  return (
    <Svg viewBox={`0 0 ${BARS * 3} 28`} preserveAspectRatio="none" style={{ flex: 1, height: 26 }}>
      {shown.map((v, i) => {
        const h = Math.max(1.5, v * 26)
        return (
          <Rect
            key={i}
            x={(pad + i) * 3}
            y={14 - h / 2}
            width={2}
            height={h}
            rx={1}
            fill={colour}
          />
        )
      })}
    </Svg>
  )
}

function StatusDot({ colour, pulsing }: { colour: string; pulsing: boolean }) {
  const pulse = useRef(new Animated.Value(0)).current
  useEffect(() => {
    if (!pulsing) {
      pulse.setValue(0)
      return
    }
    const loop = Animated.loop(
      Animated.timing(pulse, { toValue: 1, duration: 1400, useNativeDriver: true }),
    )
    loop.start()
    return () => loop.stop()
  }, [pulsing, pulse])
  return (
    <View style={{ width: 10, height: 10, marginRight: 12 }}>
      {pulsing && (
        <Animated.View
          style={{
            position: "absolute",
            width: 10,
            height: 10,
            borderRadius: 5,
            backgroundColor: colour,
            opacity: pulse.interpolate({ inputRange: [0, 1], outputRange: [0.6, 0] }),
            transform: [
              { scale: pulse.interpolate({ inputRange: [0, 1], outputRange: [1, 2.4] }) },
            ],
          }}
        />
      )}
      <View style={{ width: 10, height: 10, borderRadius: 5, backgroundColor: colour }} />
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 12,
    },
    row: {
      flexDirection: "row",
      alignItems: "center",
    },
    statusText: {
      flex: 1,
      minWidth: 0,
      marginRight: 8,
    },
    status: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    sub: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      marginTop: 1,
    },
    iconButton: {
      width: 34,
      height: 34,
      borderRadius: 8,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: theme.colors.borderSecondary,
      alignItems: "center",
      justifyContent: "center",
      marginRight: 8,
    },
    listenButton: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 12,
      height: 34,
      borderRadius: 8,
    },
    listenButtonOff: {
      backgroundColor: theme.colors.primary,
    },
    listenButtonOn: {
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: theme.colors.borderSecondary,
      backgroundColor: theme.colors.cardBackground,
    },
    listenText: {
      marginLeft: 6,
      fontSize: 14,
      fontWeight: "600",
      color: theme.colors.white,
    },
    listenTextOn: {
      color: theme.colors.textPrimary,
    },
    pressed: {
      opacity: 0.7,
    },
    error: {
      marginTop: 8,
      fontSize: 13,
      color: theme.colors.warning,
    },
    streams: {
      marginTop: 12,
    },
    streamRow: {
      flexDirection: "row",
      alignItems: "center",
      height: 30,
    },
    faded: {
      opacity: 0.4,
    },
    streamName: {
      width: 128,
      marginRight: 10,
      fontSize: 14,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    waiting: {
      fontSize: 13,
      color: theme.colors.textTertiary,
    },
    more: {
      marginTop: 2,
      marginLeft: 138,
      fontSize: 13,
      color: theme.colors.textTertiary,
    },
    chips: {
      flexDirection: "row",
      flexWrap: "wrap",
      marginTop: 10,
      gap: 6,
    },
    chip: {
      flexDirection: "row",
      alignItems: "center",
      maxWidth: "100%",
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 8,
      paddingVertical: 5,
      paddingLeft: 8,
      paddingRight: 10,
    },
    chipStripe: {
      width: 3,
      height: 14,
      borderRadius: 2,
      marginRight: 7,
    },
    chipText: {
      flexShrink: 1,
      fontSize: 13,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    chipMuted: {
      marginLeft: 6,
      fontSize: 13,
      color: theme.colors.textTertiary,
    },
  })
