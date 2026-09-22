/**
 * All times on the feed are Sydney time, on the server and in the browser
 * alike. The feed is NSW traffic, so it is the right clock for every reader,
 * and it keeps the server-rendered markup identical to what the client
 * hydrates — browser-local time would move day dividers around and break
 * hydration for anyone outside AEST.
 */
const TZ = "Australia/Sydney";

const clock = new Intl.DateTimeFormat("en-AU", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
  timeZone: TZ,
});

// en-CA yields ISO-style yyyy-mm-dd, which sorts and compares as a plain string.
const dayKeyFmt = new Intl.DateTimeFormat("en-CA", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  timeZone: TZ,
});

const dayLong = new Intl.DateTimeFormat("en-AU", {
  weekday: "long",
  day: "numeric",
  month: "long",
  timeZone: TZ,
});

export function clockTime(iso: string): string {
  return clock.format(new Date(iso));
}

export function dayKey(iso: string): string {
  return dayKeyFmt.format(new Date(iso));
}

export function dayLabel(key: string, now: Date = new Date()): string {
  if (key === dayKeyFmt.format(now)) return "Today";
  if (key === dayKeyFmt.format(new Date(now.getTime() - 86_400_000))) return "Yesterday";
  // Noon avoids any DST edge when turning the key back into a date.
  return dayLong.format(new Date(`${key}T12:00:00+10:00`));
}

export function ago(iso: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000));
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return h === 1 ? "an hour ago" : `${h} hours ago`;
  const d = Math.round(h / 24);
  return d === 1 ? "a day ago" : `${d} days ago`;
}
