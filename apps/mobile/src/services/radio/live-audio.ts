import { useCallback, useEffect, useRef, useState } from "react"
import { AppState } from "react-native"
import type { AudioContext, GainNode } from "react-native-audio-api"

import type { RadioLiveChunk } from "../api-client"
import { audioAPI, requireAudioAPI } from "./audio-api"
import { acquireAudioSession, releaseAudioSession } from "./audio-session"
import { decodePCM } from "./pcm"
import { openRadioStream } from "./radio-stream"

// A port of the web app's live engine (apps/web/app/components/radio/live-audio.ts).

const SAMPLE_RATE = 8_000
// Chunks arrive every ~180 ms; play this far behind so network jitter
// doesn't open gaps in the audio.
const JITTER_S = 0.35
// One meter bar per 20 ms vocoder frame; keep a few seconds of history.
const LEVEL_BLOCK = 160
const MAX_LEVELS = 160
// A call that has waited this long behind others is dropped rather than
// played late: live listening shouldn't drift far behind the air.
const MAX_WAIT_MS = 20_000
// No audio for this long and no end event: treat the call as over.
const STALL_MS = 3_000
// Keep a finished call on the strip briefly so it doesn't vanish mid-word.
const LINGER_MS = 2_500
// Recycle the /live connection after ~4 MB of base64 (a few minutes of
// traffic); see radio-stream.ts.
const RECYCLE_BYTES = 4_000_000

export type LiveState = "playing" | "queued" | "done"

export interface LiveStream {
  callId: string
  call: number
  tgid: number
  state: LiveState
  /** Loudness per 20 ms of what has been played, 0–1, oldest first. */
  levels: number[]
  /** Seconds of audio waiting to be played (queued calls). */
  bufferedS: number
}

interface Track {
  callId: string
  call: number
  tgid: number
  firstAt: number
  lastChunkAt: number
  lastSeq: number
  chunks: Float32Array<ArrayBuffer>[]
  played: number // chunks scheduled so far
  ended: boolean
  doneAt: number
  levels: number[]
}

/**
 * Plays /radio/live the way a scanner does: one call at a time. Calls that
 * start while another is playing wait their turn (briefly); the same
 * transmission patched onto a second talkgroup is heard once.
 *
 * The context runs at the stream's own 8 kHz. react-native-audio-api's buffer
 * sources don't resample to the context rate, but the platform output does
 * (AVAudioEngine's mixer on iOS, Oboe on Android).
 */
class LiveAudio {
  private ctx: AudioContext
  private gain: GainNode
  private tracks = new Map<string, Track>()
  private order: string[] = [] // arrival order
  private current: string | null = null
  private nextStart = 0

  constructor(private isDuplicate: (call: number, of: number) => boolean) {
    this.ctx = new (requireAudioAPI().AudioContext)({ sampleRate: SAMPLE_RATE })
    this.gain = this.ctx.createGain()
    this.gain.connect(this.ctx.destination)
  }

  resume() {
    return this.ctx.resume()
  }

  setMuted(muted: boolean) {
    this.gain.gain.value = muted ? 0 : 1
  }

  push(chunk: RadioLiveChunk) {
    let t = this.tracks.get(chunk.call_id)
    if (t?.doneAt) return // dropped: a duplicate, or waited too long
    if (!t) {
      t = {
        callId: chunk.call_id,
        call: chunk.call,
        tgid: chunk.tgid,
        firstAt: Date.now(),
        lastChunkAt: 0,
        lastSeq: -1,
        chunks: [],
        played: 0,
        ended: false,
        doneAt: 0,
        levels: [],
      }
      this.tracks.set(chunk.call_id, t)
      this.order.push(chunk.call_id)
    }
    // While a connection is being recycled both are open for a moment and
    // the same chunk can arrive twice.
    if (typeof chunk.seq === "number") {
      if (chunk.seq <= t.lastSeq) return
      t.lastSeq = chunk.seq
    }
    t.lastChunkAt = Date.now()
    // Call details can reach the app after the first audio, so keep asking
    // until the call is playing.
    if (this.current !== t.callId && this.duplicate(t)) {
      t.doneAt = Date.now()
      return
    }
    t.chunks.push(decodePCM(chunk.pcm))
    if (!this.current) this.advance()
    if (this.current === t.callId) this.schedule(t)
  }

  end(callId: string) {
    const t = this.tracks.get(callId)
    if (t) t.ended = true
  }

  /** Nothing playing or waiting: a good moment to swap connections. */
  idle(): boolean {
    return !this.current && this.order.every((id) => this.tracks.get(id)?.doneAt)
  }

  /** Schedules every buffered chunk of the current call, back to back. */
  private schedule(t: Track) {
    for (; t.played < t.chunks.length; t.played++) {
      const pcm = t.chunks[t.played]
      if (!pcm.length) continue
      const buf = this.ctx.createBuffer(1, pcm.length, SAMPLE_RATE)
      buf.copyToChannel(pcm, 0)
      const src = this.ctx.createBufferSource()
      src.buffer = buf
      src.connect(this.gain)
      const start = Math.max(this.ctx.currentTime + JITTER_S, this.nextStart)
      src.start(start)
      this.nextStart = start + buf.duration
      for (let i = 0; i + LEVEL_BLOCK <= pcm.length; i += LEVEL_BLOCK) {
        let sum = 0
        for (let j = i; j < i + LEVEL_BLOCK; j++) sum += pcm[j] * pcm[j]
        // Speech RMS sits roughly between 0.01 and 0.3; spread that over 0–1.
        const db = 20 * Math.log10(Math.sqrt(sum / LEVEL_BLOCK) + 1e-6)
        t.levels.push(Math.min(1, Math.max(0, (db + 50) / 40)))
      }
      if (t.levels.length > MAX_LEVELS) t.levels.splice(0, t.levels.length - MAX_LEVELS)
      t.chunks[t.played] = new Float32Array(0) // free it; it's scheduled
    }
  }

  /** A patch carries one keyup on several talkgroups: same radio, same moment. */
  private duplicate(t: Track): boolean {
    for (const other of this.tracks.values()) {
      if (other !== t && other.played > 0 && this.isDuplicate(t.call, other.call)) return true
    }
    return false
  }

  /** Moves to the oldest waiting call that's still worth playing. */
  private advance() {
    this.current = null
    const now = Date.now()
    for (const id of this.order) {
      const t = this.tracks.get(id)!
      if (t.doneAt) continue
      if (this.duplicate(t)) {
        t.doneAt = now
        continue
      }
      if (now - t.firstAt > MAX_WAIT_MS && t.played === 0) {
        t.doneAt = now // waited too long behind other calls
        continue
      }
      this.current = id
      this.schedule(t)
      return
    }
  }

  /** Housekeeping, called a few times a second. */
  tick(now: number) {
    const cur = this.current ? this.tracks.get(this.current) : null
    if (cur) {
      const drained =
        cur.played >= cur.chunks.length && this.ctx.currentTime >= this.nextStart - 0.05
      if (drained && (cur.ended || now - cur.lastChunkAt > STALL_MS)) {
        cur.doneAt = now
        this.advance()
      }
    } else if (this.order.length) {
      this.advance()
    }
    for (const [id, t] of this.tracks) {
      if (t.doneAt && now - t.doneAt > LINGER_MS) {
        this.tracks.delete(id)
        this.order = this.order.filter((o) => o !== id)
      }
    }
  }

  snapshot(): LiveStream[] {
    return this.order.map((id) => {
      const t = this.tracks.get(id)!
      const state: LiveState = t.doneAt ? "done" : id === this.current ? "playing" : "queued"
      const bufferedS = t.chunks.slice(t.played).reduce((s, c) => s + c.length, 0) / SAMPLE_RATE
      return { callId: id, call: t.call, tgid: t.tgid, state, levels: t.levels.slice(), bufferedS }
    })
  }

  close() {
    return this.ctx.close()
  }
}

/** A tiny external store, so only the meters re-render ~12 times a second. */
function createStreamsStore() {
  let streams: LiveStream[] = []
  const listeners = new Set<() => void>()
  return {
    get: () => streams,
    set: (next: LiveStream[]) => {
      if (next.length === 0 && streams.length === 0) return
      streams = next
      listeners.forEach((l) => l())
    },
    subscribe: (l: () => void) => {
      listeners.add(l)
      return () => {
        listeners.delete(l)
      }
    },
  }
}
export type LiveStreamsStore = ReturnType<typeof createStreamsStore>

const notifications = () => audioAPI()?.PlaybackNotificationManager ?? null

const NOTIFICATION_CONTROLS_OFF = [
  "nextTrack",
  "previousTrack",
  "skipForward",
  "skipBackward",
  "seekTo",
] as const

/**
 * Live listening. `isDuplicate(a, b)` says whether radio_calls a and b are the
 * same transmission (a patch). Keeps playing in the background: the audio
 * session is "playback" with the audio background mode, and a now-playing
 * notification (lock screen on iOS, a foreground service on Android) lets
 * the listener stop it from there.
 */
export function useLiveAudio({
  tgid,
  freshToken,
  isDuplicate,
  label,
}: {
  tgid: number | null
  freshToken: () => Promise<string | null>
  isDuplicate: (call: number, of: number) => boolean
  /** What's being listened to, for the lock-screen notification. */
  label: string
}) {
  const engine = useRef<LiveAudio | null>(null)
  const dupRef = useRef(isDuplicate)
  dupRef.current = isDuplicate
  const [listening, setListening] = useState(false)
  const [muted, setMuted] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [store] = useState(createStreamsStore)

  const teardown = useCallback(() => {
    const e = engine.current
    engine.current = null
    store.set([])
    notifications()
      ?.hide()
      .catch(() => {})
    if (e) {
      e.close()
        .catch(() => {})
        .finally(() => releaseAudioSession("live"))
    }
  }, [store])

  const start = useCallback(() => {
    if (engine.current) return
    try {
      acquireAudioSession("live")
      engine.current = new LiveAudio((a, b) => dupRef.current(a, b))
      engine.current.setMuted(muted)
      engine.current.resume().catch(() => {})
    } catch (e) {
      console.warn("[radio] live audio unavailable", e)
      engine.current = null
      releaseAudioSession("live")
      setError("Live audio isn't available in this build of the app.")
      return
    }
    setError(null)
    setListening(true)
  }, [muted])

  const stop = useCallback(() => {
    setListening(false)
    teardown()
  }, [teardown])

  useEffect(() => {
    engine.current?.setMuted(muted)
  }, [muted])

  // The stream itself, reopened when the talkgroup filter changes.
  useEffect(() => {
    if (!listening) return
    const params: Record<string, string> = {}
    if (tgid) params.tg = String(tgid)
    return openRadioStream({
      path: "/live",
      params,
      freshToken: async () => {
        const t = await freshToken()
        setError(t ? null : "Couldn't start live audio. Check your connection and subscription.")
        return t
      },
      events: {
        audio: (data) => {
          try {
            engine.current?.push(JSON.parse(data))
          } catch {
            // ignore a malformed chunk
          }
        },
        end: (data) => {
          try {
            engine.current?.end(JSON.parse(data).call_id)
          } catch {
            // ignore
          }
        },
      },
      onState: (s) => {
        if (s === "live") setError(null)
      },
      recycleBytes: RECYCLE_BYTES,
      canRecycle: () => engine.current?.idle() ?? true,
    })
  }, [listening, tgid, freshToken])

  // Advance the queue ~12 times a second; repaint the meters only while the
  // app is on screen.
  useEffect(() => {
    if (!listening) return
    const t = setInterval(() => {
      const e = engine.current
      if (!e) return
      e.tick(Date.now())
      if (AppState.currentState === "active") store.set(e.snapshot())
    }, 80)
    return () => clearInterval(t)
  }, [listening, store])

  // A phone call or Siri stops the audio engine; pick up again afterwards.
  useEffect(() => {
    if (!listening) return
    const AudioManager = audioAPI()?.AudioManager
    if (!AudioManager) return
    let sub: { remove: () => void } | undefined
    try {
      AudioManager.observeAudioInterruptions(true)
      sub = AudioManager.addSystemEventListener("interruption", (ev) => {
        if (ev.type === "ended") engine.current?.resume().catch(() => {})
      })
    } catch {
      // not available; nothing to resume
    }
    return () => {
      sub?.remove()
      try {
        AudioManager.observeAudioInterruptions(false)
      } catch {
        // ignore
      }
    }
  }, [listening])

  // Now-playing notification: lock-screen controls on iOS, and the foreground
  // service that keeps Android playing with the screen off. Pause or stop from
  // there ends listening; there is nothing to skip or seek in a live stream.
  useEffect(() => {
    const manager = notifications()
    if (!listening || !manager) return
    let cancelled = false
    ;(async () => {
      try {
        await manager.show({
          title: "Radio: listening live",
          artist: label,
          album: "RFSPager",
          state: "playing",
        })
        for (const c of NOTIFICATION_CONTROLS_OFF) {
          if (cancelled) return
          await manager.enableControl(c, false)
        }
      } catch (e) {
        console.warn("[radio] now-playing notification failed", e)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [listening, label])

  useEffect(() => {
    const manager = notifications()
    if (!listening || !manager) return
    const subs: { remove: () => void }[] = []
    try {
      subs.push(manager.addEventListener("playbackNotificationPause", () => stop()))
      subs.push(manager.addEventListener("playbackNotificationStop", () => stop()))
    } catch {
      // ignore
    }
    return () => subs.forEach((s) => s.remove())
  }, [listening, stop])

  useEffect(() => () => teardown(), [teardown])

  return { listening, start, stop, muted, setMuted, error, streams: store }
}
