import { useEffect, useRef, useState, type RefObject } from "react";

/**
 * Categorical slots from the dataviz reference palette, stepped for the
 * light and dark surfaces and validated for colour-vision separation.
 * Applied as CSS variables on a wrapper so marks reference
 * `var(--series-n)` and the dark values swap in one place.
 */
export const SERIES_VARS_CLASS =
  "[--series-1:#2a78d6] [--series-2:#eb6834] [--series-3:#1baf7a] [--series-4:#eda100] " +
  "dark:[--series-1:#3987e5] dark:[--series-2:#d95926] dark:[--series-3:#199e70] dark:[--series-4:#c98500]";

/** A receiver has at most four channels, so hues are never cycled. */
export const MAX_SERIES = 4;

export function seriesColor(slot: number): string {
  const n = Math.min(Math.max(Math.trunc(slot), 0), MAX_SERIES - 1) + 1;
  return `var(--series-${n})`;
}

export function scaleLinear(domain: [number, number], range: [number, number]): (v: number) => number {
  const [d0, d1] = domain;
  const [r0, r1] = range;
  const span = d1 - d0;
  if (span === 0) return () => (r0 + r1) / 2;
  return (v) => r0 + ((v - d0) / span) * (r1 - r0);
}

/** Round tick values (1/2/5 steps) that cover [lo, hi]. */
export function niceTicks(lo: number, hi: number, count = 5): number[] {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return [];
  if (hi <= lo) return [lo];
  const raw = (hi - lo) / Math.max(1, count);
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  const step = (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10) * mag;
  const start = Math.floor(lo / step) * step;
  const end = Math.ceil(hi / step) * step;
  const out: number[] = [];
  for (let v = start; v <= end + step / 2; v += step) out.push(Number(v.toFixed(10)));
  return out;
}

const TIME_STEPS_MS = [
  60_000, 5 * 60_000, 10 * 60_000, 15 * 60_000, 30 * 60_000,
  3_600_000, 2 * 3_600_000, 3 * 3_600_000, 6 * 3_600_000, 12 * 3_600_000, 24 * 3_600_000,
];

/** Tick timestamps on clean minute/hour boundaries, at most `maxTicks` of them. */
export function timeTicks(t0: number, t1: number, maxTicks = 6): number[] {
  const span = t1 - t0;
  if (!(span > 0)) return [t0];
  const step = TIME_STEPS_MS.find((s) => span / s <= maxTicks) ?? TIME_STEPS_MS[TIME_STEPS_MS.length - 1];
  // Align to the local clock, not to UTC multiples, so hour ticks land on local hours.
  const offset = new Date(t0).getTimezoneOffset() * 60_000;
  const first = Math.ceil((t0 - offset) / step) * step + offset;
  const out: number[] = [];
  for (let t = first; t <= t1; t += step) out.push(t);
  return out;
}

export function fmtTimeTick(t: number): string {
  const d = new Date(t);
  const hm = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
  if (d.getHours() === 0 && d.getMinutes() === 0) {
    return `${d.toLocaleDateString([], { day: "numeric", month: "short" })} ${hm}`;
  }
  return hm;
}

export function fmtTimeFull(t: number): string {
  const d = new Date(t);
  return d.toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** Index of the element of an ascending array closest to v. */
export function nearestIndex(sorted: number[], v: number): number {
  const n = sorted.length;
  if (n === 0) return -1;
  let lo = 0;
  let hi = n - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] < v) lo = mid + 1;
    else hi = mid;
  }
  if (lo > 0 && Math.abs(sorted[lo - 1] - v) <= Math.abs(sorted[lo] - v)) return lo - 1;
  return lo;
}

/** Width of a container element, tracked with ResizeObserver. 0 until mounted. */
export function useMeasure<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setWidth(Math.floor(el.getBoundingClientRect().width));
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width];
}
