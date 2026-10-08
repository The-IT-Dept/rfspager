import { useCallback, useEffect, useRef, useState } from "react"
import {
  keepPreviousData,
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query"

import {
  api,
  isSubscriptionRequired,
  type RadioCall,
  type RadioCallPage,
} from "../services/api-client"
import { openRadioStream, type StreamState } from "../services/radio/radio-stream"

const PAGE_SIZE = 50
// Live calls prepend to the first page; past this the older pages are dropped
// (they load again on scroll).
const MAX_LIVE_ROWS = 1000
// Refresh the stream token this long before it expires, so a URL built from
// it still has time to be fetched.
const REFRESH_MARGIN_MS = 2 * 60_000

const noRetryOn402 = (count: number, e: unknown) => !isSubscriptionRequired(e) && count < 2

export const radioCallsKey = (tgid: number | null) => ["radio", "calls", tgid ?? null] as const

/**
 * The radio streams and recordings are loaded by EventSource and the audio
 * decoder, which can't send our Authorization header. The server swaps the
 * login token for a short-lived, radio-only one that can ride in the URL.
 * `fresh()` returns one valid for at least a couple of minutes.
 */
export function useStreamToken(enabled: boolean) {
  const current = useRef<{ token: string; expiresAt: number } | null>(null)
  const inflight = useRef<Promise<string | null> | null>(null)

  const refresh = useCallback((): Promise<string | null> => {
    if (inflight.current) return inflight.current
    const p = api.radio
      .streamToken()
      .then((body) => {
        current.current = { token: body.token, expiresAt: Date.parse(body.expires_at) }
        return body.token
      })
      .catch(() => null)
      .finally(() => {
        inflight.current = null
      })
    inflight.current = p
    return p
  }, [])

  useEffect(() => {
    if (!enabled) return
    let timer: ReturnType<typeof setTimeout>
    let stopped = false
    const schedule = async () => {
      await refresh()
      if (stopped) return
      const exp = current.current?.expiresAt ?? Date.now() + 60_000
      timer = setTimeout(schedule, Math.max(30_000, exp - Date.now() - REFRESH_MARGIN_MS))
    }
    schedule()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [enabled, refresh])

  const fresh = useCallback(async () => {
    const t = current.current
    if (t && t.expiresAt - Date.now() > REFRESH_MARGIN_MS) return t.token
    return refresh()
  }, [refresh])

  return { fresh }
}

/** The call timeline, newest first, addressed by the last id already held. */
export function useRadioCalls(tgid: number | null, enabled = true) {
  return useInfiniteQuery({
    queryKey: radioCallsKey(tgid),
    queryFn: ({ pageParam }) =>
      api.radio.calls({ tg: tgid, before_id: pageParam, limit: PAGE_SIZE }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => {
      if (!last.has_more) return undefined
      return last.data[last.data.length - 1]?.id
    },
    enabled,
    // /radio/events keeps the cached list current while the screen is up;
    // anything else (a filter visited earlier) is refetched on return.
    staleTime: 0,
    retry: noRetryOn402,
    // Keep the list mounted across a filter change (see useMessages).
    placeholderData: keepPreviousData,
  })
}

/** Talkgroups heard so far (last_seen_at set), most recently active first. */
export function useRadioTalkgroups(enabled = true) {
  return useQuery({
    queryKey: ["radio", "talkgroups"],
    queryFn: () => api.radio.talkgroups(),
    select: (res) => (res.data ?? []).filter((t) => t.last_seen_at),
    enabled,
    refetchInterval: 60_000,
    retry: noRetryOn402,
  })
}

type CallsData = InfiniteData<RadioCallPage, number | undefined>

/**
 * Refetches the timeline from the top. Cached timelines are cut back to their
 * newest page first, so catching up doesn't refetch every page scrolled
 * through so far.
 */
export function refreshRadioCalls(queryClient: QueryClient) {
  queryClient.setQueriesData<CallsData>({ queryKey: ["radio", "calls"] }, (d) =>
    d && d.pages.length > 1
      ? { pages: d.pages.slice(0, 1), pageParams: d.pageParams.slice(0, 1) }
      : d,
  )
  return queryClient.invalidateQueries({ queryKey: ["radio", "calls"] })
}

/** Replaces the call with the same id, or prepends a new one, in the cached timeline. */
function upsertCall(data: CallsData | undefined, call: RadioCall): CallsData | undefined {
  if (!data || data.pages.length === 0) return data
  for (let p = 0; p < data.pages.length; p++) {
    const i = data.pages[p].data.findIndex((c) => c.id === call.id)
    if (i < 0) continue
    const rows = data.pages[p].data.slice()
    rows[i] = call
    const pages = data.pages.slice()
    pages[p] = { ...pages[p], data: rows }
    return { ...data, pages }
  }
  const first = data.pages[0]
  const rows = [call, ...first.data]
  if (rows.length > MAX_LIVE_ROWS) {
    // Keep just the (trimmed) first page; scrolling loads the rest again.
    return {
      pages: [{ ...first, data: rows.slice(0, MAX_LIVE_ROWS), has_more: true }],
      pageParams: [data.pageParams[0]],
    }
  }
  return { ...data, pages: [{ ...first, data: rows }, ...data.pages.slice(1)] }
}

/**
 * Every call the server announces over /radio/events — once when it starts,
 * again when it ends, when its audio lands and when its transcript arrives —
 * upserted by id into the cached timeline for `tgid`. Reconnects with a fresh
 * token; after a gap (connection lost, app backgrounded) the timeline is
 * refetched so nothing that happened meanwhile is missing.
 */
export function useRadioEvents({
  enabled,
  tgid,
  freshToken,
}: {
  enabled: boolean
  tgid: number | null
  freshToken: () => Promise<string | null>
}) {
  const queryClient = useQueryClient()
  const [connection, setConnection] = useState<StreamState>("connecting")
  const tgRef = useRef(tgid)
  tgRef.current = tgid
  const wasEnabledBefore = useRef(false)

  useEffect(() => {
    if (!enabled) return
    let opened = false
    return openRadioStream({
      path: "/events",
      freshToken,
      events: {
        call: (data) => {
          let call: RadioCall
          try {
            call = JSON.parse(data)
          } catch {
            return // ignore a malformed event
          }
          const tg = tgRef.current
          if (tg && call.tgid !== tg) return
          queryClient.setQueryData<CallsData>(radioCallsKey(tg), (d) => upsertCall(d, call))
        },
      },
      onState: (s) => {
        setConnection(s)
        if (s === "live" && !opened) {
          // Re-enabled after a pause (e.g. back from the background): catch up.
          opened = true
          if (wasEnabledBefore.current) refreshRadioCalls(queryClient)
          wasEnabledBefore.current = true
        }
      },
      onReopen: () => refreshRadioCalls(queryClient),
      recycleBytes: 2_000_000,
    })
  }, [enabled, freshToken, queryClient])

  return { connection }
}
