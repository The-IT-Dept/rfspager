import { useCallback, useEffect, useRef, useState } from "react";

import type { LiveChunk } from "./types";

const SAMPLE_RATE = 8_000;
// Chunks arrive every ~180 ms; play this far behind so network jitter
// doesn't open gaps in the audio.
const JITTER_S = 0.35;
// One meter bar per 20 ms vocoder frame; keep a few seconds of history.
const LEVEL_BLOCK = 160;
const MAX_LEVELS = 160;
// Keep a finished call on the strip briefly so it doesn't vanish mid-word.
const LINGER_MS = 2_500;

export interface LiveStream {
  callId: string;
  call: number;
  tgid: number;
  /** Loudness per 20 ms, 0–1, oldest first. */
  levels: number[];
  ended: boolean;
  endedAt: number;
}

function decodePCM(b64: string): Float32Array<ArrayBuffer> {
  const bin = atob(b64);
  const out = new Float32Array(new ArrayBuffer((bin.length >> 1) * 4));
  for (let i = 0; i < out.length; i++) {
    let v = bin.charCodeAt(2 * i) | (bin.charCodeAt(2 * i + 1) << 8);
    if (v & 0x8000) v -= 0x10000;
    out[i] = v / 32768;
  }
  return out;
}

/**
 * Plays /radio/live through Web Audio. Each call is its own gapless stream;
 * overlapping calls simply mix, as they would on a scanner.
 */
class LiveAudio {
  private ctx: AudioContext;
  private gain: GainNode;
  private next = new Map<string, number>(); // call → when its next chunk starts
  streams = new Map<string, LiveStream>();

  constructor() {
    this.ctx = new AudioContext();
    this.gain = this.ctx.createGain();
    this.gain.connect(this.ctx.destination);
  }

  resume() {
    return this.ctx.resume();
  }

  setMuted(muted: boolean) {
    this.gain.gain.value = muted ? 0 : 1;
  }

  push(chunk: LiveChunk) {
    const pcm = decodePCM(chunk.pcm);
    if (!pcm.length) return;
    const buf = this.ctx.createBuffer(1, pcm.length, SAMPLE_RATE);
    buf.copyToChannel(pcm, 0);
    const src = this.ctx.createBufferSource();
    src.buffer = buf;
    src.connect(this.gain);
    const start = Math.max(this.ctx.currentTime + JITTER_S, this.next.get(chunk.call_id) ?? 0);
    src.start(start);
    this.next.set(chunk.call_id, start + buf.duration);

    let s = this.streams.get(chunk.call_id);
    if (!s) {
      s = { callId: chunk.call_id, call: chunk.call, tgid: chunk.tgid, levels: [], ended: false, endedAt: 0 };
      this.streams.set(chunk.call_id, s);
    }
    for (let i = 0; i + LEVEL_BLOCK <= pcm.length; i += LEVEL_BLOCK) {
      let sum = 0;
      for (let j = i; j < i + LEVEL_BLOCK; j++) sum += pcm[j] * pcm[j];
      // Speech RMS sits roughly between 0.01 and 0.3; spread that over 0–1.
      const db = 20 * Math.log10(Math.sqrt(sum / LEVEL_BLOCK) + 1e-6);
      s.levels.push(Math.min(1, Math.max(0, (db + 50) / 40)));
    }
    if (s.levels.length > MAX_LEVELS) s.levels.splice(0, s.levels.length - MAX_LEVELS);
  }

  end(callId: string) {
    const s = this.streams.get(callId);
    if (s) {
      s.ended = true;
      s.endedAt = Date.now();
    }
    this.next.delete(callId);
  }

  /** Drops streams that finished a while ago. Returns true if any went. */
  sweep(now: number) {
    let changed = false;
    for (const [id, s] of this.streams) {
      if (s.ended && now - s.endedAt > LINGER_MS) {
        this.streams.delete(id);
        changed = true;
      }
    }
    return changed;
  }

  close() {
    this.ctx.close();
  }
}

/**
 * Live listening. `start` must be called from a click: browsers only let a
 * page make sound after the user asks it to.
 */
export function useLiveAudio({
  tgid,
  freshToken,
}: {
  tgid: number | null;
  freshToken: () => Promise<string | null>;
}) {
  const engine = useRef<LiveAudio | null>(null);
  const [listening, setListening] = useState(false);
  const [muted, setMuted] = useState(false);
  const [streams, setStreams] = useState<LiveStream[]>([]);
  const [error, setError] = useState<string | null>(null);

  const start = useCallback(() => {
    if (!engine.current) engine.current = new LiveAudio();
    engine.current.resume();
    setError(null);
    setListening(true);
  }, []);

  const stop = useCallback(() => {
    setListening(false);
    engine.current?.close();
    engine.current = null;
    setStreams([]);
  }, []);

  useEffect(() => {
    engine.current?.setMuted(muted);
  }, [muted]);

  // The stream itself, reopened when the talkgroup filter changes.
  useEffect(() => {
    if (!listening) return;
    let es: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let closed = false;
    const connect = async () => {
      const token = await freshToken();
      if (closed) return;
      if (!token) {
        setError("Couldn't start live audio. Check you're still signed in.");
        retry = setTimeout(connect, 5_000);
        return;
      }
      const q = new URLSearchParams({ token });
      if (tgid) q.set("tg", String(tgid));
      es = new EventSource(`/api/v2/mobile/radio/live?${q}`);
      es.onopen = () => setError(null);
      es.addEventListener("audio", (e) => {
        try {
          engine.current?.push(JSON.parse((e as MessageEvent).data));
        } catch {
          // ignore a malformed chunk
        }
      });
      es.addEventListener("end", (e) => {
        try {
          engine.current?.end(JSON.parse((e as MessageEvent).data).call_id);
        } catch {
          // ignore
        }
      });
      es.onerror = () => {
        es?.close();
        if (!closed) retry = setTimeout(connect, 3_000);
      };
    };
    connect();
    return () => {
      closed = true;
      es?.close();
      clearTimeout(retry);
    };
  }, [listening, tgid, freshToken]);

  // Repaint the meters ~12 times a second while listening.
  useEffect(() => {
    if (!listening) return;
    const t = setInterval(() => {
      const e = engine.current;
      if (!e) return;
      e.sweep(Date.now());
      setStreams(Array.from(e.streams.values(), (s) => ({ ...s, levels: s.levels.slice() })));
    }, 80);
    return () => clearInterval(t);
  }, [listening]);

  useEffect(() => () => engine.current?.close(), []);

  return { listening, start, stop, muted, setMuted, streams, error };
}
