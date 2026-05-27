import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { LayoutChangeEvent, Text, View } from "react-native"
import { SvgXml } from "react-native-svg"
import { useFocusEffect } from "expo-router"

import { useAuthStore } from "../stores/auth-store"
import { apiBaseUrl } from "../services/api-client"

interface Frame {
  ts?: number
  centre_hz: number
  sample_rate: number
  bins: number[]
}

interface Props {
  systemUuid: string
  /** Number of downsampled columns rendered horizontally. */
  cols?: number
  /** Number of waterfall rows visible at once. */
  rows?: number
  /** Logical height in CSS px. */
  height?: number
  /** Hide the small dB / centre status footer. */
  hideFooter?: boolean
}

function colour(db: number, minDb: number, maxDb: number): string {
  const t = Math.max(0, Math.min(1, (db - minDb) / (maxDb - minDb)))
  const stops: Array<[number, [number, number, number]]> = [
    [0.0, [0, 0, 30]],
    [0.25, [0, 0, 200]],
    [0.5, [0, 200, 200]],
    [0.7, [200, 220, 0]],
    [1.0, [255, 60, 60]],
  ]
  for (let i = 1; i < stops.length; i++) {
    if (t <= stops[i][0]) {
      const [t0, c0] = stops[i - 1]
      const [t1, c1] = stops[i]
      const f = (t - t0) / (t1 - t0)
      const r = c0[0] + (c1[0] - c0[0]) * f
      const g = c0[1] + (c1[1] - c0[1]) * f
      const b = c0[2] + (c1[2] - c0[2]) * f
      return `rgb(${r | 0},${g | 0},${b | 0})`
    }
  }
  const [r, g, b] = stops[stops.length - 1][1]
  return `rgb(${r},${g},${b})`
}

function downsample(bins: number[], cols: number): number[] {
  const out = new Array<number>(cols)
  const step = bins.length / cols
  for (let x = 0; x < cols; x++) {
    const start = Math.floor(x * step)
    const end = Math.floor((x + 1) * step)
    let sum = 0
    for (let i = start; i < end; i++) sum += bins[i]
    out[x] = sum / (end - start || 1)
  }
  return out
}

function buildSvg(
  frames: number[][],
  cols: number,
  width: number,
  height: number,
  gMin: number,
  gMax: number,
): string {
  if (frames.length === 0 || width <= 0) return ""
  const cellW = width / cols
  const cellH = height / frames.length
  // Pre-allocate. ~60 chars per rect is a good upper bound; sized this way
  // we avoid V8 grow-and-copy on the concat chain.
  const parts: string[] = new Array(frames.length * cols + 2)
  let p = 0
  parts[p++] = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" shape-rendering="crispEdges">`
  const wAttr = (cellW + 0.5).toFixed(2)
  const hAttr = (cellH + 0.5).toFixed(2)
  for (let y = 0; y < frames.length; y++) {
    const row = frames[y]
    const yPx = (y * cellH).toFixed(2)
    for (let x = 0; x < row.length; x++) {
      const xPx = (x * cellW).toFixed(2)
      parts[p++] = `<rect x="${xPx}" y="${yPx}" width="${wAttr}" height="${hAttr}" fill="${colour(row[x], gMin, gMax)}"/>`
    }
  }
  parts[p++] = "</svg>"
  parts.length = p
  return parts.join("")
}

/**
 * Live spectrum waterfall. Subscribes to the per-system WebSocket and
 * renders the last `rows` frames as a single natively-rasterised SVG.
 *
 * Performance notes:
 * - The waterfall is rendered as ONE `<SvgXml>` rather than `rows*cols`
 *   `<Rect>` children. The previous approach reconciled ~6k React nodes
 *   per frame which saturated the JS thread.
 * - Frame state updates are coalesced inside a rAF tick so a burst of
 *   WS frames produces a single render.
 * - The WS is gated on screen focus via `useFocusEffect`; expo-router's
 *   native stack keeps prior screens mounted when you push, so without
 *   this the waterfall would keep pumping frames behind a screen the
 *   user has navigated away from.
 */
export function SpectrumWaterfall({
  systemUuid,
  cols = 64,
  rows = 48,
  height = 240,
  hideFooter = false,
}: Props) {
  const token = useAuthStore((s) => s.token)
  const [width, setWidth] = useState(0)
  const [frames, setFrames] = useState<number[][]>([])
  const [info, setInfo] = useState<{
    centreMHz: number
    spanMHz: number
    min: number
    max: number
  } | null>(null)
  const [status, setStatus] = useState<"connecting" | "live" | "idle" | "error">("connecting")
  const lastFrameAt = useRef(0)
  const pendingRef = useRef<number[][]>([])
  const flushRaf = useRef<number | null>(null)
  const [focused, setFocused] = useState(true)

  const onLayout = (e: LayoutChangeEvent) => {
    const w = Math.floor(e.nativeEvent.layout.width)
    if (w > 0 && w !== width) setWidth(w)
  }

  // Track screen focus so the WS pauses while another screen is on top.
  useFocusEffect(
    useCallback(() => {
      setFocused(true)
      return () => setFocused(false)
    }, []),
  )

  useEffect(() => {
    if (!focused || !token || !systemUuid) return
    const base = apiBaseUrl().replace(/^http/, "ws")
    const url = `${base}/api/v2/mobile/systems/${systemUuid}/spectrum`
    const Ctor = WebSocket as unknown as new (
      url: string,
      protocols?: string | string[],
      options?: { headers?: Record<string, string> },
    ) => WebSocket
    const ws = new Ctor(url, undefined, { headers: { Authorization: `Bearer ${token}` } })

    const flushPending = () => {
      flushRaf.current = null
      const incoming = pendingRef.current
      if (incoming.length === 0) return
      pendingRef.current = []
      setFrames((cur) => {
        // Newest-first: incoming arrived in chronological order, so newest
        // is at the end. Reverse to place it at the front.
        const next = incoming.length === 1 ? [incoming[0], ...cur] : [...incoming.reverse(), ...cur]
        if (next.length > rows) next.length = rows
        return next
      })
    }

    const scheduleFlush = () => {
      if (flushRaf.current != null) return
      flushRaf.current = requestAnimationFrame(flushPending)
    }

    ws.onopen = () => setStatus("connecting")
    ws.onerror = () => setStatus("error")
    ws.onmessage = (ev) => {
      let env: { type: string; data: any }
      try {
        env = JSON.parse(ev.data)
      } catch {
        return
      }
      lastFrameAt.current = Date.now()
      setStatus("live")
      if (env.type === "history") {
        const fs = (env.data?.frames || []) as Frame[]
        if (fs.length === 0) return
        const sliced = fs.slice(-rows).map((f) => downsample(f.bins, cols))
        // Reverse so newest ends up at the top (index 0) like the live feed.
        setFrames(sliced.reverse())
        const last = fs[fs.length - 1]
        if (last) setInfo(infoFrom(last))
      } else if (env.type === "frame") {
        const f = env.data as Frame
        pendingRef.current.push(downsample(f.bins, cols))
        setInfo(infoFrom(f))
        scheduleFlush()
      }
    }

    const idleTimer = setInterval(() => {
      if (Date.now() - lastFrameAt.current > 4000) {
        setStatus((s) => (s === "error" ? s : "idle"))
      }
    }, 1000)

    return () => {
      ws.close()
      clearInterval(idleTimer)
      if (flushRaf.current != null) {
        cancelAnimationFrame(flushRaf.current)
        flushRaf.current = null
      }
      pendingRef.current = []
    }
  }, [focused, systemUuid, token, cols, rows])

  const { gMin, gMax } = useMemo(() => {
    if (frames.length === 0) return { gMin: -80, gMax: -40 }
    let mn = Number.POSITIVE_INFINITY
    let mx = Number.NEGATIVE_INFINITY
    for (const row of frames) {
      for (const v of row) {
        if (v < mn) mn = v
        if (v > mx) mx = v
      }
    }
    if (mx - mn < 20) mx = mn + 20
    return { gMin: mn, gMax: mx }
  }, [frames])

  const svgXml = useMemo(
    () => buildSvg(frames, cols, width, height, gMin, gMax),
    [frames, cols, width, height, gMin, gMax],
  )

  return (
    <View style={{ width: "100%" }} onLayout={onLayout}>
      <View style={{ width: "100%", height, backgroundColor: "#0a0a0a", borderRadius: 6, overflow: "hidden" }}>
        {width > 0 && svgXml ? (
          <SvgXml xml={svgXml} width={width} height={height} />
        ) : null}
      </View>
      {hideFooter ? null : (
        <Text style={{ marginTop: 4, fontSize: 11, color: "#888" }}>
          {status === "live" && info
            ? `centre ${info.centreMHz.toFixed(4)} MHz · span ${info.spanMHz.toFixed(3)} MHz · ${info.min.toFixed(1)} … ${info.max.toFixed(1)} dB`
            : status === "connecting"
              ? "Waiting for spectrum…"
              : status === "idle"
                ? "No frames (receiver offline or dongle silent)"
                : "Spectrum stream failed"}
        </Text>
      )}
    </View>
  )
}

function infoFrom(f: Frame) {
  let mn = Number.POSITIVE_INFINITY
  let mx = Number.NEGATIVE_INFINITY
  for (const v of f.bins) {
    if (v < mn) mn = v
    if (v > mx) mx = v
  }
  return {
    centreMHz: f.centre_hz / 1e6,
    spanMHz: f.sample_rate / 1e6,
    min: mn,
    max: mx,
  }
}
