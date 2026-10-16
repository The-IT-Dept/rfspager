import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const pad2 = (n: number) => String(n).padStart(2, "0");

/** 14:02:31 in the browser's local time. */
export function fmtClock(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return "—";
  const d = new Date(ms);
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
}

function sameDay(a: Date, b: Date) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

/** Clock time today, "16 Oct 14:02:31" otherwise. */
export function fmtStamp(ms: number | null | undefined, now = Date.now()): string {
  if (ms == null || !Number.isFinite(ms)) return "—";
  const d = new Date(ms);
  if (sameDay(d, new Date(now))) return fmtClock(ms);
  return `${d.toLocaleDateString([], { day: "numeric", month: "short" })} ${fmtClock(ms)}`;
}

export function fmtDateTime(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return "—";
  return new Date(ms).toLocaleString([], {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** "14s ago", "3m ago", "2h ago", "4d ago". */
export function fmtAgo(ms: number | null | undefined, now: number): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return "never";
  const s = Math.max(0, Math.floor((now - ms) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

/** 273600 -> "3d 4h", 7384 -> "2h 3m", 125 -> "2m 5s", 45 -> "45s". */
export function fmtDuration(totalSeconds: number | null | undefined): string {
  if (totalSeconds == null || !Number.isFinite(totalSeconds) || totalSeconds < 0) return "—";
  const s = Math.floor(totalSeconds);
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec}s`;
}

export function fmtMHz(hz: number | null | undefined, digits = 4): string {
  if (hz == null || !Number.isFinite(hz) || hz === 0) return "—";
  return (hz / 1e6).toFixed(digits);
}

export function fmtMSps(rate: number | null | undefined): string {
  if (rate == null || !Number.isFinite(rate) || rate === 0) return "—";
  return `${(rate / 1e6).toFixed(3)} MS/s`;
}

export function fmtFixed(v: number | null | undefined, digits = 1): string {
  if (v == null || !Number.isFinite(v)) return "—";
  return v.toFixed(digits);
}

export function fmtInt(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return "—";
  return Math.round(v).toLocaleString();
}

export function fmtPct(share: number | null | undefined, digits = 1): string {
  if (share == null || !Number.isFinite(share)) return "—";
  return `${(share * 100).toFixed(digits)}%`;
}

/** 0 is AGC; anything else is tenths of a dB. */
export function gainLabel(tenths: number): string {
  if (!tenths) return "AGC";
  return `${(tenths / 10).toFixed(1)} dB`;
}

/** "v5.9.0-abc1234" from "5.9.0-abc1234"; leaves "dev" alone. */
export function fmtVersion(v: string | null | undefined): string {
  if (!v) return "—";
  return /^\d/.test(v) ? `v${v}` : v;
}

/** Hostname of a URL, for compact display; falls back to the input. */
export function urlHost(u: string): string {
  try {
    return new URL(u).host || u;
  } catch {
    return u;
  }
}

export function attrValue(v: unknown): string {
  if (v === null || v === undefined) return String(v);
  if (typeof v === "object") return JSON.stringify(v);
  const s = String(v);
  return /\s/.test(s) || s === "" ? JSON.stringify(s) : s;
}
