import { useCallback, useEffect, useRef, useState } from "react"
import type { AudioBuffer, AudioBufferSourceNode, AudioContext } from "react-native-audio-api"

import { radioStreamURL, type RadioCall } from "../api-client"
import { requireAudioAPI } from "./audio-api"
import { acquireAudioSession, releaseAudioSession } from "./audio-session"

// Recordings are 8 kHz mono WAV; decoding into an 8 kHz context needs no
// resampling (see LiveAudio for why the context runs at the stream's rate).
const SAMPLE_RATE = 8_000
const PROGRESS_MS = 100
// Decoded recordings kept for replay. A 30 s call is ~1 MB of float samples.
const CACHE_SIZE = 6

const newContext = () => new (requireAudioAPI().AudioContext)({ sampleRate: SAMPLE_RATE })

/** Progress of the active clip, 0–1, in a store so only its row re-renders. */
function createProgressStore() {
  let value = 0
  const listeners = new Set<() => void>()
  return {
    get: () => value,
    set: (v: number) => {
      if (v === value) return
      value = v
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
export type ProgressStore = ReturnType<typeof createProgressStore>

/**
 * One player shared by every row: starting a clip stops the last. A clip is
 * fetched with a stream token in the URL, decoded whole, and played from an
 * AudioBufferSourceNode; pause remembers the offset and resume starts a new
 * source there (Web Audio sources can't be paused).
 */
export function useClipPlayer(freshToken: () => Promise<string | null>) {
  const ctx = useRef<AudioContext | null>(null)
  const source = useRef<AudioBufferSourceNode | null>(null)
  const buffer = useRef<AudioBuffer | null>(null)
  const startedAt = useRef(0) // ctx time the current source started
  const offset = useRef(0) // seconds into the clip that source started at
  const cache = useRef(new Map<number, AudioBuffer>())
  const request = useRef(0)

  const [current, setCurrent] = useState<number | null>(null)
  const [playing, setPlaying] = useState(false)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState<number | null>(null)
  const [progress] = useState(createProgressStore)

  const position = () => {
    const c = ctx.current
    if (!c || !source.current) return offset.current
    return offset.current + (c.currentTime - startedAt.current)
  }

  // Once nothing has played for a moment, suspend the context and let the
  // audio session go (so other apps' audio can resume). Deferred so a stop
  // followed straight away by the next play can't race suspend against resume.
  const idleTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const scheduleIdle = useCallback(() => {
    clearTimeout(idleTimer.current)
    idleTimer.current = setTimeout(() => {
      const c = ctx.current
      if (!c || source.current) return
      c.suspend()
        .catch(() => {})
        .finally(() => releaseAudioSession("clip"))
    }, 1_000)
  }, [])

  /** Stops the current source, if any. */
  const halt = useCallback(() => {
    const s = source.current
    source.current = null
    if (s) {
      s.onEnded = null
      try {
        s.stop()
      } catch {
        // already stopped
      }
    }
    scheduleIdle()
  }, [scheduleIdle])

  const play = useCallback(
    async (buf: AudioBuffer, from: number) => {
      if (!ctx.current) ctx.current = newContext()
      const c = ctx.current
      clearTimeout(idleTimer.current)
      acquireAudioSession("clip")
      await c.resume()
      const s = c.createBufferSource()
      s.buffer = buf
      s.connect(c.destination)
      s.onEnded = () => {
        if (source.current !== s) return // stopped by us
        source.current = null
        offset.current = 0
        setPlaying(false)
        progress.set(1)
        scheduleIdle()
      }
      buffer.current = buf
      offset.current = from
      startedAt.current = c.currentTime
      source.current = s
      s.start(0, from)
      setPlaying(true)
    },
    [progress, scheduleIdle],
  )

  const toggle = useCallback(
    async (call: RadioCall) => {
      if (!call.audio_url) return
      if (current === call.id && buffer.current) {
        if (source.current) {
          // Pause.
          const at = position()
          halt()
          offset.current = Math.min(at, buffer.current.duration)
          setPlaying(false)
        } else {
          const from = offset.current >= buffer.current.duration - 0.05 ? 0 : offset.current
          play(buffer.current, from).catch(() => setFailed(call.id))
        }
        return
      }

      const id = ++request.current
      halt()
      setCurrent(call.id)
      setPlaying(false)
      setFailed(null)
      progress.set(0)
      buffer.current = null
      offset.current = 0
      try {
        let buf = cache.current.get(call.id)
        if (!buf) {
          setLoading(true)
          const token = await freshToken()
          if (!token) throw new Error("no stream token")
          if (!ctx.current) ctx.current = newContext()
          buf = await ctx.current.decodeAudioData(radioStreamURL(call.audio_url, token))
          cache.current.set(call.id, buf)
          if (cache.current.size > CACHE_SIZE) {
            cache.current.delete(cache.current.keys().next().value!)
          }
        }
        if (id !== request.current) return // another row was tapped meanwhile
        setLoading(false)
        await play(buf, 0)
      } catch (e) {
        if (id !== request.current) return
        console.warn("[radio] clip failed", e)
        setLoading(false)
        setFailed(call.id)
      }
    },
    [current, freshToken, halt, play, progress],
  )

  // Progress while playing.
  useEffect(() => {
    if (!playing) return
    const t = setInterval(() => {
      const b = buffer.current
      if (b && b.duration > 0) progress.set(Math.min(1, position() / b.duration))
    }, PROGRESS_MS)
    return () => clearInterval(t)
  }, [playing, progress])

  useEffect(
    () => () => {
      halt()
      clearTimeout(idleTimer.current)
      const c = ctx.current
      ctx.current = null
      c?.close()
        .catch(() => {})
        .finally(() => releaseAudioSession("clip"))
    },
    [halt],
  )

  return { current, playing, loading, failed, progress, toggle }
}
