import { useEffect, useRef, useState } from "react";

interface SpectrumFrame {
  ts?: number;
  centre_hz: number;
  sample_rate: number;
  bins: number[];
}

interface ServerEnvelope {
  type: "history" | "frame";
  data: SpectrumFrame | { frames: SpectrumFrame[] };
}

interface Props {
  /**
   * URL to the spectrum SSE endpoint. Either the admin endpoint
   * (`/api/v1/admin/systems/{id}/spectrum?token=...`) or the public
   * UUID-keyed one (`/api/public/systems/{uuid}/spectrum`).
   *
   * If omitted, falls back to the legacy admin URL built from `systemId`.
   */
  url?: string;
  systemId?: number;
  height?: number;
}

function colourMap(db: number, minDb: number, maxDb: number): [number, number, number] {
  const t = Math.max(0, Math.min(1, (db - minDb) / (maxDb - minDb)));
  const stops: Array<[number, [number, number, number]]> = [
    [0.0, [0, 0, 30]],
    [0.25, [0, 0, 200]],
    [0.5, [0, 200, 200]],
    [0.7, [200, 220, 0]],
    [1.0, [255, 60, 60]],
  ];
  for (let i = 1; i < stops.length; i++) {
    if (t <= stops[i][0]) {
      const [t0, c0] = stops[i - 1];
      const [t1, c1] = stops[i];
      const f = (t - t0) / (t1 - t0);
      return [
        c0[0] + (c1[0] - c0[0]) * f,
        c0[1] + (c1[1] - c0[1]) * f,
        c0[2] + (c1[2] - c0[2]) * f,
      ];
    }
  }
  return stops[stops.length - 1][1];
}

export function SpectrumWaterfall({ url, systemId, height = 420 }: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [status, setStatus] = useState<"connecting" | "live" | "idle" | "error">("connecting");
  const [frameInfo, setFrameInfo] = useState<{ centreMHz: number; spanMHz: number; min: number; max: number } | null>(null);

  // Track the canvas's CSS pixel width so we can resize it responsively.
  const [width, setWidth] = useState(1024);

  // Resize observer: reposition canvas to fill its wrapper.
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const w = Math.max(256, Math.floor(e.contentRect.width));
        setWidth(w);
      }
    });
    ro.observe(wrap);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    // Set canvas backing-store size to match CSS pixels × devicePixelRatio
    // for crisp rendering, but draw in CSS-pixel coordinates by scaling.
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    canvas.style.height = `${height}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    ctx.fillStyle = "#0a0a0a";
    ctx.fillRect(0, 0, width, height);

    let streamURL = url;
    if (!streamURL && systemId != null) {
      const token = localStorage.getItem("admin_token") ?? "";
      streamURL = `/api/v1/admin/systems/${systemId}/spectrum?token=${encodeURIComponent(token)}`;
    }
    if (!streamURL) return;
    const es = new EventSource(streamURL);

    let lastFrameAt = 0;

    es.onopen = () => setStatus("connecting");
    es.onerror = () => setStatus("error");
    es.onmessage = (ev) => {
      let env: ServerEnvelope;
      try {
        env = JSON.parse(ev.data);
      } catch {
        return;
      }
      lastFrameAt = Date.now();
      setStatus("live");
      if (env.type === "history") {
        const bundle = env.data as { frames: SpectrumFrame[] };
        // Draw history frames in arrival order so the oldest ends up at the
        // bottom of the canvas and the newest sits at the top.
        for (const f of bundle.frames) drawFrame(ctx, f, width, height);
        const last = bundle.frames[bundle.frames.length - 1];
        if (last) setFrameInfo(infoFromFrame(last));
      } else if (env.type === "frame") {
        const f = env.data as SpectrumFrame;
        drawFrame(ctx, f, width, height);
        setFrameInfo(infoFromFrame(f));
      }
    };

    const idleTimer = setInterval(() => {
      if (Date.now() - lastFrameAt > 3000) {
        setStatus((s) => (s === "error" ? s : "idle"));
      }
    }, 1000);

    return () => {
      es.close();
      clearInterval(idleTimer);
    };
  }, [url, systemId, width, height]);

  return (
    <div className="space-y-2">
      <div ref={wrapRef} className="w-full">
        <canvas
          ref={canvasRef}
          className="block w-full border rounded-md bg-black"
          style={{ height }}
        />
      </div>
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>
          {status === "live" && frameInfo ? (
            <>centre {frameInfo.centreMHz.toFixed(4)} MHz · span {frameInfo.spanMHz.toFixed(3)} MHz · {frameInfo.min.toFixed(1)} … {frameInfo.max.toFixed(1)} dB</>
          ) : status === "connecting" ? (
            "Waiting for spectrum…"
          ) : status === "idle" ? (
            "No spectrum frames (receiver offline or dongle silent)"
          ) : (
            "Spectrum stream failed"
          )}
        </span>
      </div>
    </div>
  );
}

function infoFromFrame(f: SpectrumFrame) {
  return {
    centreMHz: f.centre_hz / 1e6,
    spanMHz: f.sample_rate / 1e6,
    min: Math.min(...f.bins),
    max: Math.max(...f.bins),
  };
}

function drawFrame(
  ctx: CanvasRenderingContext2D,
  frame: SpectrumFrame,
  width: number,
  height: number,
) {
  // Scroll: copy the canvas down by 1px.
  const existing = ctx.getImageData(0, 0, width, height - 1);
  ctx.putImageData(existing, 0, 1);

  const min = Math.min(...frame.bins, -80);
  const max = Math.max(...frame.bins, min + 30);

  const row = ctx.createImageData(width, 1);
  for (let x = 0; x < width; x++) {
    const binIdx = Math.floor((x / width) * frame.bins.length);
    const [r, g, b] = colourMap(frame.bins[binIdx], min, max);
    const i = x * 4;
    row.data[i] = r;
    row.data[i + 1] = g;
    row.data[i + 2] = b;
    row.data[i + 3] = 255;
  }
  ctx.putImageData(row, 0, 0);
}
