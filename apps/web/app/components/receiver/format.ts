/** Display helpers shared by the receiver management tabs. */

export function fmtDate(s?: string | null): string {
  if (!s) return "—";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleString();
}

export function fmtTime(s?: string | null): string {
  if (!s) return "—";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

export function fmtAgo(s: string | null | undefined, now: number = Date.now()): string {
  if (!s) return "—";
  const t = new Date(s).getTime();
  if (Number.isNaN(t)) return "—";
  const sec = Math.max(0, Math.round((now - t) / 1000));
  if (sec < 60) return `${sec} s ago`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min} min ago`;
  const h = Math.round(min / 60);
  if (h < 48) return `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}

/** 93784 -> "1d 2h", 7384 -> "2h 3m", 125 -> "2m 5s", 45 -> "45 s". */
export function fmtDuration(totalSeconds: number | null | undefined): string {
  if (totalSeconds == null || !Number.isFinite(totalSeconds) || totalSeconds < 0) return "—";
  const s = Math.round(totalSeconds);
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec} s`;
}

export function fmtMHz(hz: number | null | undefined, digits = 4): string {
  if (hz == null || !Number.isFinite(hz)) return "—";
  return `${(hz / 1e6).toFixed(digits)} MHz`;
}

export function fmtMSps(rate: number | null | undefined): string {
  if (rate == null || !Number.isFinite(rate)) return "—";
  return `${(rate / 1e6).toFixed(3)} MS/s`;
}

export function fmtBytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function fmtFixed(v: number | null | undefined, digits = 1): string {
  if (v == null || !Number.isFinite(v)) return "—";
  return v.toFixed(digits);
}

export function fmtInt(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return "—";
  return Math.round(v).toLocaleString();
}

/** 0 is AGC; anything else is tenths of a dB. */
export function gainLabel(tenths: number): string {
  if (tenths === 0) return "AGC";
  return `${(tenths / 10).toFixed(1)} dB`;
}

/** Compact "key=value, key=value" rendering for metadata and raw log payloads. */
export function compactObject(obj: Record<string, unknown> | null | undefined, max = 6): string {
  if (!obj) return "";
  return Object.entries(obj)
    .slice(0, max)
    .map(([k, v]) => `${k}=${typeof v === "object" && v !== null ? JSON.stringify(v) : String(v)}`)
    .join(", ");
}
