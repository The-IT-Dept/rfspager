import React, { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ActivityIndicator,
  AppState,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
  type ListRenderItem,
} from "react-native"
import { Stack } from "expo-router"
import { Ionicons } from "@expo/vector-icons"
import { useQueryClient } from "@tanstack/react-query"

import { useRadioCalls, useRadioEvents, useRadioTalkgroups, useStreamToken } from "../../hooks"
import { refreshRadioCalls } from "../../hooks/use-radio"
import { isSubscriptionRequired, type RadioCall } from "../../services/api-client"
import { useClipPlayer } from "../../services/radio/clip-player"
import { useLiveAudio } from "../../services/radio/live-audio"
import { useAuthStore, useValidSubscription } from "../../stores"
import { AppTheme, useAppTheme } from "../../theme"
import { useSubscriptionModal } from "../../utils/use-subscription-modal"
import { CallRow } from "./call-row"
import { OnAir } from "./on-air"
import { RadioPaywall } from "./radio-paywall"
import { dayKey, dayLabel, withoutPatchCopies } from "./radio-format"
import { TalkgroupChips } from "./talkgroup-chips"

// A call whose end we never heard about shouldn't sit on the strip forever.
const STALE_ON_AIR_MS = 2 * 60_000

/**
 * Radio: P25 calls from our receivers, live and as a timeline. Subscriber
 * only; everyone else sees what it is and a way to subscribe. The tab itself
 * is always there.
 */
export function RadioScreen() {
  const theme = useAppTheme()
  const validSubscription = useValidSubscription()
  const refreshUser = useAuthStore((s) => s.refreshUser)
  const { showModal, ModalComponent } = useSubscriptionModal()
  // The server is the authority: a 402 means our cached user is out of date.
  const [serverGated, setServerGated] = useState(false)
  useEffect(() => setServerGated(false), [validSubscription])
  const onSubscriptionRequired = useCallback(() => {
    setServerGated(true)
    refreshUser().catch(() => {})
  }, [refreshUser])

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.backgroundTertiary }}>
      {validSubscription && !serverGated ? (
        <RadioFeed onSubscriptionRequired={onSubscriptionRequired} />
      ) : (
        <RadioPaywall onSubscribe={() => showModal()} />
      )}
      <ModalComponent />
    </View>
  )
}

type Item =
  { type: "day"; key: string; label: string } | { type: "call"; key: string; call: RadioCall }

function useAppActive() {
  const [active, setActive] = useState(AppState.currentState === "active")
  useEffect(() => {
    const sub = AppState.addEventListener("change", (s) => setActive(s === "active"))
    return () => sub.remove()
  }, [])
  return active
}

function useNow(everyMs: number) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), everyMs)
    return () => clearInterval(t)
  }, [everyMs])
  return now
}

function RadioFeed({ onSubscriptionRequired }: { onSubscriptionRequired: () => void }) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const queryClient = useQueryClient()
  const [tgid, setTgid] = useState<number | null>(null)
  const { fresh } = useStreamToken(true)

  const callsQuery = useRadioCalls(tgid)
  const talkgroupsQuery = useRadioTalkgroups()
  const talkgroups = useMemo(() => talkgroupsQuery.data ?? [], [talkgroupsQuery.data])
  const calls = useMemo(
    () => callsQuery.data?.pages.flatMap((p) => p.data) ?? [],
    [callsQuery.data],
  )

  useEffect(() => {
    if (isSubscriptionRequired(callsQuery.error) || isSubscriptionRequired(talkgroupsQuery.error)) {
      onSubscriptionRequired()
    }
  }, [callsQuery.error, talkgroupsQuery.error, onSubscriptionRequired])

  // The same keyup patched onto two talkgroups arrives as two calls: same
  // radio, same moment. Live listening plays it once.
  const callsRef = useRef(new Map<number, { src: number | null; at: number }>())
  callsRef.current = useMemo(
    () => new Map(calls.map((c) => [c.id, { src: c.source_rid, at: Date.parse(c.started_at) }])),
    [calls],
  )
  const isDuplicate = useCallback((a: number, b: number) => {
    const x = callsRef.current.get(a)
    const y = callsRef.current.get(b)
    return Boolean(x && y && x.src && x.src === y.src && Math.abs(x.at - y.at) < 2_000)
  }, [])

  const selectedTG = talkgroups.find((t) => t.tgid === tgid) ?? null
  const filterLabel =
    selectedTG?.alpha_tag ?? (tgid ? `talkgroup ${tgid}` : "every talkgroup we can hear")
  const live = useLiveAudio({
    tgid,
    freshToken: fresh,
    isDuplicate,
    label: selectedTG?.alpha_tag ?? (tgid ? `Talkgroup ${tgid}` : "All talkgroups"),
  })
  // The timeline stream is only needed on screen, or while listening: live
  // listening checks patches against it.
  const appActive = useAppActive()
  const { connection } = useRadioEvents({
    enabled: appActive || live.listening,
    tgid,
    freshToken: fresh,
  })
  const player = useClipPlayer(fresh)

  // Talkgroups on air right now, one chip each.
  const now = useNow(30_000)
  const active = useMemo(() => {
    const cutoff = now - STALE_ON_AIR_MS
    const seen = new Set<number | null>()
    return withoutPatchCopies(calls).filter((c) => {
      if (c.ended_at || Date.parse(c.started_at) < cutoff || seen.has(c.tgid)) return false
      seen.add(c.tgid)
      return true
    })
  }, [calls, now])

  // By default the timeline shows what you can listen to: recorded calls,
  // and clear calls still on air (their recording lands when they end).
  const [showAll, setShowAll] = useState(false)
  const shown = useMemo(() => {
    const deduped = withoutPatchCopies(calls)
    return showAll ? deduped : deduped.filter((c) => c.audio_url || (!c.ended_at && !c.encrypted))
  }, [calls, showAll])
  const hiddenCount = calls.length - shown.length

  const items = useMemo(() => {
    const out: Item[] = []
    let lastDay = ""
    for (const call of shown) {
      const key = dayKey(call.started_at)
      if (key !== lastDay) {
        out.push({ type: "day", key: `day-${key}`, label: dayLabel(key) })
        lastDay = key
      }
      out.push({ type: "call", key: String(call.id), call })
    }
    return out
  }, [shown])

  const renderItem: ListRenderItem<Item> = useCallback(
    ({ item }) => {
      if (item.type === "day") {
        return (
          <View style={styles.day}>
            <Text style={styles.dayText}>{item.label}</Text>
          </View>
        )
      }
      const id = item.call.id
      return (
        <CallRow
          call={item.call}
          active={player.current === id}
          playing={player.current === id && player.playing}
          loading={player.current === id && player.loading}
          failed={player.failed === id}
          progress={player.progress}
          onToggle={player.toggle}
        />
      )
    },
    [
      player.current,
      player.playing,
      player.loading,
      player.failed,
      player.progress,
      player.toggle,
      theme,
    ],
  )

  const select = useCallback((next: number | null) => setTgid(next), [])

  const [refreshing, setRefreshing] = useState(false)
  const onRefresh = useCallback(() => {
    setRefreshing(true)
    Promise.all([refreshRadioCalls(queryClient), talkgroupsQuery.refetch()]).finally(() =>
      setRefreshing(false),
    )
  }, [queryClient, talkgroupsQuery])

  const header = (
    <View>
      <OnAir
        connection={connection}
        active={active}
        live={live}
        talkgroups={talkgroups}
        filterLabel={filterLabel}
      />
      {talkgroups.length > 0 && (
        <TalkgroupChips talkgroups={talkgroups} activeId={tgid} onSelect={select} />
      )}
      <View style={styles.filterBar}>
        <Text style={styles.filterText} numberOfLines={2}>
          {showAll
            ? "Showing every call, including encrypted ones and calls we couldn't record."
            : hiddenCount > 0
              ? `Showing calls you can play. ${hiddenCount} encrypted or unrecorded hidden.`
              : "Showing calls you can play."}
        </Text>
        <Pressable onPress={() => setShowAll(!showAll)} hitSlop={8} accessibilityRole="button">
          <Text style={styles.filterToggle}>{showAll ? "Only playable" : "Show all"}</Text>
        </Pressable>
      </View>
    </View>
  )

  const renderEmpty = () => {
    if (callsQuery.isLoading) {
      return (
        <View style={styles.empty}>
          <ActivityIndicator size="large" color={theme.colors.interactive} />
          <Text style={styles.emptySubtitle}>Loading calls...</Text>
        </View>
      )
    }
    if (callsQuery.isError && !isSubscriptionRequired(callsQuery.error)) {
      return (
        <View style={styles.empty}>
          <Ionicons name="cloud-offline-outline" size={44} color={theme.colors.textDisabled} />
          <Text style={styles.emptyTitle}>Couldn't load calls</Text>
          <Text style={styles.emptySubtitle}>
            Check your connection, then pull down to try again.
          </Text>
        </View>
      )
    }
    return (
      <View style={styles.empty}>
        <Ionicons name="radio-outline" size={44} color={theme.colors.textDisabled} />
        <Text style={styles.emptyTitle}>No calls yet</Text>
        <Text style={styles.emptySubtitle}>
          {tgid
            ? "Nothing has been heard on this talkgroup. Pick another, or wait for the next call."
            : "Calls appear here the moment our receivers hear them."}
        </Text>
      </View>
    )
  }

  return (
    <>
      {/* Listening stays one tap away however far down the timeline you are. */}
      <Stack.Toolbar placement="right">
        <Stack.Toolbar.Button
          icon={live.listening ? "stop.fill" : "headphones"}
          accessibilityLabel={live.listening ? "Stop listening" : "Listen live"}
          onPress={live.listening ? live.stop : live.start}
        />
      </Stack.Toolbar>

      <FlatList
        data={items}
        keyExtractor={(item) => item.key}
        renderItem={renderItem}
        ListHeaderComponent={header}
        ListEmptyComponent={renderEmpty}
        ListFooterComponent={
          callsQuery.isFetchingNextPage ? (
            <View style={styles.footer}>
              <ActivityIndicator size="small" color={theme.colors.interactive} />
              <Text style={styles.footerText}>Loading earlier calls...</Text>
            </View>
          ) : null
        }
        // New calls are inserted under the day header at index 0, which
        // stays put; further down, the rows being read don't move.
        maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
        contentContainerStyle={[styles.list, items.length === 0 && styles.listEmpty]}
        contentInsetAdjustmentBehavior="automatic"
        showsVerticalScrollIndicator={false}
        onEndReachedThreshold={1.5}
        onEndReached={() => {
          if (callsQuery.hasNextPage && !callsQuery.isFetchingNextPage) callsQuery.fetchNextPage()
        }}
        refreshControl={
          <RefreshControl
            tintColor={theme.colors.interactive}
            refreshing={refreshing}
            onRefresh={onRefresh}
          />
        }
      />
    </>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    list: {
      paddingBottom: 24,
    },
    listEmpty: {
      flexGrow: 1,
    },
    filterBar: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 8,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: theme.colors.border,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    filterText: {
      flex: 1,
      fontSize: 13,
      color: theme.colors.textTertiary,
      marginRight: 12,
    },
    filterToggle: {
      fontSize: 13,
      fontWeight: "600",
      color: theme.colors.interactive,
    },
    day: {
      paddingHorizontal: 16,
      paddingTop: 14,
      paddingBottom: 6,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    dayText: {
      fontSize: 13,
      fontWeight: "600",
      color: theme.colors.textSecondary,
    },
    empty: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 32,
      paddingVertical: 48,
    },
    emptyTitle: {
      marginTop: 14,
      fontSize: 20,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
    },
    emptySubtitle: {
      marginTop: 8,
      fontSize: 15,
      lineHeight: 21,
      color: theme.colors.textTertiary,
      textAlign: "center",
    },
    footer: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
      paddingVertical: 16,
    },
    footerText: {
      marginLeft: 8,
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
  })
