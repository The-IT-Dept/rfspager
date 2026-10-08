import EventSource from "react-native-sse"
import { radioStreamURL } from "../api-client"

/**
 * One of the radio SSE streams (/radio/events, /radio/live), kept open.
 *
 * Authorised by a stream token in the URL, so every (re)connect fetches a
 * fresh one: react-native-sse would otherwise retry the same URL, and the
 * token in it may have expired. Its own polling is turned off for that reason.
 *
 * Two things react-native-sse doesn't do for us:
 *
 *  - It is built on XMLHttpRequest, which keeps the whole response body in
 *    memory for as long as the request lives. /live carries ~21 kB/s of base64
 *    per active call, so a long session is recycled after `recycleBytes`:
 *    a second connection is opened and the first closed once it is up. Both
 *    streams tolerate the overlap (calls upsert by id; live chunks are
 *    de-duplicated by seq).
 *  - It reports nothing when the server ends a stream cleanly (the server does
 *    that when a client falls behind or a subscription lapses), and a dead
 *    mobile connection can stay "open" for minutes. A watchdog reconnects
 *    when the request has finished or nothing, not even the server's 15 s
 *    keepalive comment, has arrived for STALE_MS.
 */

export type StreamState = "connecting" | "live" | "reconnecting"

const RECONNECT_MS = 3_000
const MAX_RECONNECT_MS = 30_000
const WATCHDOG_MS = 5_000
// The server writes a keepalive comment every 15 s.
const STALE_MS = 40_000

interface Conn {
  es: EventSource<string>
  bytes: number
  lastLength: number
  lastActivity: number
  opened: boolean
  dead: boolean
  createdAt: number
}

// react-native-sse keeps its XMLHttpRequest on a private field. Reading it is
// the only way to see a cleanly finished stream or keepalive traffic.
function xhrOf(es: EventSource<string>): XMLHttpRequest | null {
  return (es as unknown as { _xhr?: XMLHttpRequest | null })._xhr ?? null
}

export function openRadioStream(opts: {
  path: string
  params?: Record<string, string>
  freshToken: () => Promise<string | null>
  /** Handlers by SSE event name; each gets the raw `data` string. */
  events: Record<string, (data: string) => void>
  onState?: (state: StreamState) => void
  /** Called when a connection opens after one was lost (not on first open or a recycle). */
  onReopen?: () => void
  /** Reopen the stream after this many characters of event data. */
  recycleBytes?: number
  /** Whether now is a good moment to recycle (e.g. between calls). Forced at 2x recycleBytes. */
  canRecycle?: () => boolean
}): () => void {
  let closed = false
  let current: Conn | null = null
  let pending: Conn | null = null // a recycle connection not yet open
  let recycling = false // a recycle is fetching its token
  let retry: ReturnType<typeof setTimeout> | undefined
  let failures = 0
  let everOpened = false
  let lostSinceOpen = false

  const kill = (c: Conn | null) => {
    if (!c || c.dead) return
    c.dead = true
    c.es.removeAllEventListeners()
    c.es.close()
  }

  const scheduleReconnect = () => {
    if (closed || retry) return
    const delay = Math.min(MAX_RECONNECT_MS, RECONNECT_MS * Math.max(1, failures))
    retry = setTimeout(() => {
      retry = undefined
      connect(false)
    }, delay)
  }

  const fail = (c: Conn) => {
    if (c.dead) return
    kill(c)
    if (c === pending) {
      // A recycle attempt failed; the current connection is still serving.
      pending = null
      return
    }
    if (c !== current) return
    current = null
    kill(pending)
    pending = null
    failures++
    lostSinceOpen = true
    opts.onState?.("reconnecting")
    scheduleReconnect()
  }

  const connect = async (recycle: boolean) => {
    if (recycle) recycling = true
    const token = await opts.freshToken().finally(() => {
      if (recycle) recycling = false
    })
    if (closed || (recycle && !current)) return
    if (!token) {
      if (!recycle) {
        failures++
        opts.onState?.("reconnecting")
        scheduleReconnect()
      }
      return
    }
    const es = new EventSource<string>(radioStreamURL(opts.path, token, opts.params), {
      pollingInterval: 0, // we reconnect ourselves, with a fresh token
      timeoutBeforeConnection: 0,
    })
    const c: Conn = {
      es,
      bytes: 0,
      lastLength: 0,
      lastActivity: Date.now(),
      opened: false,
      dead: false,
      createdAt: Date.now(),
    }
    if (recycle) pending = c
    else {
      kill(current)
      current = c
      if (!everOpened) opts.onState?.("connecting")
    }

    es.addEventListener("open", () => {
      if (c.dead) return
      c.opened = true
      c.lastActivity = Date.now()
      failures = 0
      if (c === pending) {
        // Make before break: the new stream is up, retire the old one.
        kill(current)
        current = c
        pending = null
      } else if (lostSinceOpen && everOpened) {
        opts.onReopen?.()
      }
      everOpened = true
      lostSinceOpen = false
      opts.onState?.("live")
    })
    for (const [name, handler] of Object.entries(opts.events)) {
      es.addEventListener(name, (e) => {
        if (c.dead) return
        const data = (e as { data?: string | null }).data
        if (!data) return
        c.bytes += data.length
        c.lastActivity = Date.now()
        handler(data)
      })
    }
    es.addEventListener("error", () => fail(c))
  }

  const watchdog = setInterval(() => {
    const now = Date.now()
    // A recycle connection that never came up must not block the next one.
    if (pending && !pending.opened && now - pending.createdAt > STALE_MS) fail(pending)
    const c = current
    if (closed || !c || c.dead) return
    const xhr = xhrOf(c.es)
    if (xhr) {
      if (xhr.readyState === 4) {
        fail(c) // the server ended the stream
        return
      }
      const len = (xhr.responseText ?? "").length
      if (len !== c.lastLength) {
        c.lastLength = len
        c.lastActivity = now
      }
    }
    if (now - c.lastActivity > STALE_MS) {
      fail(c)
      return
    }
    const limit = opts.recycleBytes
    if (
      limit &&
      c.opened &&
      !pending &&
      !recycling &&
      c.bytes > limit &&
      (c.bytes > 2 * limit || (opts.canRecycle?.() ?? true))
    ) {
      connect(true)
    }
  }, WATCHDOG_MS)

  connect(false)

  return () => {
    closed = true
    clearInterval(watchdog)
    clearTimeout(retry)
    kill(pending)
    kill(current)
    pending = null
    current = null
  }
}
