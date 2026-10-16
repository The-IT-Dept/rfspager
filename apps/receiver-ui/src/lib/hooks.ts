import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { errorMessage } from "./api";

// One shared 1 s clock so every "14s ago" on screen ticks together.
let now = Date.now();
const nowListeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | null = null;

function subscribeNow(l: () => void) {
  nowListeners.add(l);
  if (!timer) {
    now = Date.now();
    timer = setInterval(() => {
      now = Date.now();
      for (const fn of nowListeners) fn();
    }, 1000);
  }
  return () => {
    nowListeners.delete(l);
    if (nowListeners.size === 0 && timer) {
      clearInterval(timer);
      timer = null;
    }
  };
}

/** Current time in ms, re-rendering once a second. */
export function useNow(): number {
  return useSyncExternalStore(subscribeNow, () => now);
}

/** The path part of the location hash: "#/pages" -> "/pages", "" -> "/". */
function readHash(): string {
  const h = window.location.hash.replace(/^#/, "");
  const path = h.split("?")[0];
  return path.startsWith("/") ? path : "/" + path;
}

function subscribeHash(l: () => void) {
  window.addEventListener("hashchange", l);
  return () => window.removeEventListener("hashchange", l);
}

export function useHashRoute(): string {
  return useSyncExternalStore(subscribeHash, readHash);
}

export interface Loaded<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
  setData: (fn: (prev: T | null) => T | null) => void;
}

/** Fetch on mount and whenever `deps` change; stale responses are discarded. */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]): Loaded<T> {
  const [data, setDataState] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fnRef
      .current()
      .then((d) => {
        if (cancelled) return;
        setDataState(d);
        setError(null);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(errorMessage(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const setData = useCallback((f: (prev: T | null) => T | null) => setDataState(f), []);
  return { data, error, loading, reload, setData };
}

/** Polls `fn` every `ms` while the tab is visible. */
export function useInterval(fn: () => void, ms: number) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    const t = setInterval(() => {
      if (document.visibilityState === "visible") ref.current();
    }, ms);
    return () => clearInterval(t);
  }, [ms]);
}

/** Debounced copy of a value. */
export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}
