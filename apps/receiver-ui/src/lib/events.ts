import { useEffect, useRef, useSyncExternalStore } from "react";
import type { LogEntry, Page, Status, StatsPoint } from "./types";

/** Payload of each named event on /api/events. */
export interface EventMap {
  page: Page;
  stats: StatsPoint;
  log: LogEntry;
  status: Status;
}
export type EventName = keyof EventMap;

/** `connecting` before the first open, `live` while open, `down` while the browser retries. */
export type StreamState = "connecting" | "live" | "down";

type Handler<K extends EventName> = (data: EventMap[K]) => void;

const NAMES: EventName[] = ["page", "stats", "log", "status"];
const RETRY_MS = 3000;

/**
 * One EventSource for the whole app. The browser reconnects by itself after a
 * network error; when it gives up (non-200, wrong content type) it is reopened
 * here after a pause. `epoch` goes up on every reopen after the first, so views
 * that keep history can refetch what they missed.
 */
class EventHub {
  private es: EventSource | null = null;
  private handlers = new Map<EventName, Set<(d: unknown) => void>>();
  private stateListeners = new Set<() => void>();
  private retry: ReturnType<typeof setTimeout> | null = null;
  private opened = false;
  state: StreamState = "connecting";
  epoch = 0;

  start() {
    if (this.es || typeof EventSource === "undefined") return;
    const es = new EventSource("/api/events");
    this.es = es;
    es.onopen = () => {
      if (this.opened) this.epoch++;
      this.opened = true;
      this.setState("live");
    };
    es.onerror = () => {
      if (this.state !== "connecting" || this.opened) this.setState("down");
      if (es.readyState === EventSource.CLOSED) {
        es.close();
        this.es = null;
        if (this.retry) clearTimeout(this.retry);
        this.retry = setTimeout(() => {
          this.retry = null;
          this.start();
        }, RETRY_MS);
      }
    };
    for (const name of NAMES) {
      es.addEventListener(name, (ev) => {
        const set = this.handlers.get(name);
        if (!set || set.size === 0) return;
        let data: unknown;
        try {
          data = JSON.parse((ev as MessageEvent<string>).data);
        } catch {
          return;
        }
        for (const h of set) h(data);
      });
    }
  }

  on<K extends EventName>(name: K, h: Handler<K>): () => void {
    let set = this.handlers.get(name);
    if (!set) {
      set = new Set();
      this.handlers.set(name, set);
    }
    const fn = h as (d: unknown) => void;
    set.add(fn);
    return () => set.delete(fn);
  }

  private setState(s: StreamState) {
    if (this.state === s) return;
    this.state = s;
    for (const l of this.stateListeners) l();
  }

  subscribeState = (l: () => void) => {
    this.stateListeners.add(l);
    return () => this.stateListeners.delete(l);
  };
}

export const hub = new EventHub();

/** Calls `handler` for every `name` event. The handler may change between renders. */
export function useEvent<K extends EventName>(name: K, handler: Handler<K>) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => hub.on(name, (d) => ref.current(d)), [name]);
}

export function useStreamState(): StreamState {
  return useSyncExternalStore(hub.subscribeState, () => hub.state);
}

/** Goes up each time the stream reopens after a drop. Use as an effect dependency to refetch. */
export function useStreamEpoch(): number {
  return useSyncExternalStore(hub.subscribeState, () => hub.epoch);
}
