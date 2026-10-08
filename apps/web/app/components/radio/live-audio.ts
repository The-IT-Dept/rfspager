import { useCallback, useEffect, useRef, useState } from "react";

import type { LiveChunk } from "./types";

const SAMPLE_RATE = 8_000;
// Chunks arrive every ~180 ms; play this far behind so network jitter
// doesn't open gaps in the audio.
const JITTER_S = 0.35;
// One meter bar per 20 ms vocoder frame; keep a few seconds of history.
const LEVEL_BLOCK = 160;
const MAX_LEVELS = 160;
// A call that has waited this long behind others is dropped rather than
// played late: live listening shouldn't drift far behind the air.
const MAX_WAIT_MS = 20_000;
// No audio for this long and no end event: treat the call as over.
const STALL_MS = 3_000;
// Keep a finished call on the strip briefly so it doesn't vanish mid-word.
const LINGER_MS = 2_500;

export type LiveState = "playing" | "queued" | "done";

export interface LiveStream {
  callId: string;
  call: number;
  tgid: number;
  state: LiveState;
  /** Loudness per 20 ms of what has been played, 0–1, oldest first. */
  levels: number[];
  /** Seconds of audio waiting to be played (queued calls). */
  bufferedS: number;
}

interface Track {
  callId: string;
  call: number;
  tgid: number;
  firstAt: number;
  lastChunkAt: number;
  chunks: Float32Array<ArrayBuffer>[];
  played: number; // chunks scheduled so far
  ended: boolean;
  doneAt: number;
  levels: number[];
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
 * Plays /radio/live through Web Audio the way a scanner does: one call at a
 * time. Calls that start while another is playing wait their turn (briefly);
 * the same transmission patched onto a second talkgroup is heard once.
 */
class LiveAudio {
  private ctx: AudioContext;
  private gain: GainNode;
  private tracks = new Map<string, Track>();
  private order: string[] = []; // arrival order
  private current: string | null = null;
  private nextStart = 0;

  constructor(private isDuplicate: (call: number, of: number) => boolean) {
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
    let t = this.tracks.get(chunk.call_id);
    if (t?.doneAt) return; // dropped: a duplicate, or waited too long
    if (!t) {
      t = {
        callId: chunk.call_id, call: chunk.call, tgid: chunk.tgid, firstAt: Date.now(), lastChunkAt: 0,
        chunks: [], played: 0, ended: false, doneAt: 0, levels: [],
      };
      this.tracks.set(chunk.call_id, t);
      this.order.push(chunk.call_id);
    }
    t.lastChunkAt = Date.now();
    // Call details can reach the page after the first audio, so keep asking
    // until the call is playing.
    if (this.current !== t.callId && this.duplicate(t)) {
      t.doneAt = Date.now();
      return;
    }
    t.chunks.push(decodePCM(chunk.pcm));
    if (!this.current) this.advance();
    if (this.current === t.callId) this.schedule(t);
  }

  end(callId: string) {
    const t = this.tracks.get(callId);
    if (t) t.ended = true;
  }

  /** Schedules every buffered chunk of the current call, back to back. */
  private schedule(t: Track) {
    for (; t.played < t.chunks.length; t.played++) {
      const pcm = t.chunks[t.played];
      if (!pcm.length) continue;
      const buf = this.ctx.createBuffer(1, pcm.length, SAMPLE_RATE);
      buf.copyToChannel(pcm, 0);
      const src = this.ctx.createBufferSource();
      src.buffer = buf;
      src.connect(this.gain);
      const start = Math.max(this.ctx.currentTime + JITTER_S, this.nextStart);
      src.start(start);
      this.nextStart = start + buf.duration;
      for (let i = 0; i + LEVEL_BLOCK <= pcm.length; i += LEVEL_BLOCK) {
        let sum = 0;
        for (let j = i; j < i + LEVEL_BLOCK; j++) sum += pcm[j] * pcm[j];
        // Speech RMS sits roughly between 0.01 and 0.3; spread that over 0–1.
        const db = 20 * Math.log10(Math.sqrt(sum / LEVEL_BLOCK) + 1e-6);
        t.levels.push(Math.min(1, Math.max(0, (db + 50) / 40)));
      }
      if (t.levels.length > MAX_LEVELS) t.levels.splice(0, t.levels.length - MAX_LEVELS);
      t.chunks[t.played] = new Float32Array(0); // free it; it's scheduled
    }
  }

  /** A patch carries one keyup on several talkgroups: same radio, same moment. */
  private duplicate(t: Track): boolean {
    for (const other of this.tracks.values()) {
      if (other !== t && other.played > 0 && this.isDuplicate(t.call, other.call)) return true;
    }
    return false;
  }

  /** Moves to the oldest waiting call that's still worth playing. */
  private advance() {
    this.current = null;
    const now = Date.now();
    for (const id of this.order) {
      const t = this.tracks.get(id)!;
      if (t.doneAt) continue;
      if (this.duplicate(t)) {
        t.doneAt = now;
        continue;
      }
      if (now - t.firstAt > MAX_WAIT_MS && t.played === 0) {
        t.doneAt = now; // waited too long behind other calls
        continue;
      }
      this.current = id;
      this.schedule(t);
      return;
    }
  }

  /** Housekeeping, called a few times a second. */
  tick(now: number) {
    const cur = this.current ? this.tracks.get(this.current) : null;
    if (cur) {
      const drained = cur.played >= cur.chunks.length && this.ctx.currentTime >= this.nextStart - 0.05;
      if (drained && (cur.ended || now - cur.lastChunkAt > STALL_MS)) {
        cur.doneAt = now;
        this.advance();
      }
    } else if (this.order.length) {
      this.advance();
    }
    for (const [id, t] of this.tracks) {
      if (t.doneAt && now - t.doneAt > LINGER_MS) {
        this.tracks.delete(id);
        this.order = this.order.filter((o) => o !== id);
      }
    }
  }

  snapshot(): LiveStream[] {
    return this.order.map((id) => {
      const t = this.tracks.get(id)!;
      const state: LiveState = t.doneAt ? "done" : id === this.current ? "playing" : "queued";
      const bufferedS = t.chunks.slice(t.played).reduce((s, c) => s + c.length, 0) / SAMPLE_RATE;
      return { callId: id, call: t.call, tgid: t.tgid, state, levels: t.levels.slice(), bufferedS };
    });
  }

  close() {
    this.ctx.close();
  }
}

/**
 * Live listening. `start` must be called from a click: browsers only let a
 * page make sound after the user asks it to. `isDuplicate(a, b)` says
 * whether radio_calls a and b are the same transmission (a patch).
 */
export function useLiveAudio({
  tgid,
  freshToken,
  isDuplicate,
}: {
  tgid: number | null;
  freshToken: () => Promise<string | null>;
  isDuplicate: (call: number, of: number) => boolean;
}) {
  const engine = useRef<LiveAudio | null>(null);
  const dupRef = useRef(isDuplicate);
  dupRef.current = isDuplicate;
  const [listening, setListening] = useState(false);
  const [muted, setMuted] = useState(false);
  const [streams, setStreams] = useState<LiveStream[]>([]);
  const [error, setError] = useState<string | null>(null);

  const start = useCallback(() => {
    if (!engine.current) engine.current = new LiveAudio((a, b) => dupRef.current(a, b));
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

  // Advance the queue and repaint the meters ~12 times a second.
  useEffect(() => {
    if (!listening) return;
    const t = setInterval(() => {
      const e = engine.current;
      if (!e) return;
      e.tick(Date.now());
      setStreams(e.snapshot());
    }, 80);
    return () => clearInterval(t);
  }, [listening]);

  useEffect(() => () => engine.current?.close(), []);

  return { listening, start, stop, muted, setMuted, streams, error };
}
