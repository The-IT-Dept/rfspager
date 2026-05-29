import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import type { Route } from "./+types/systems";
import { SpectrumWaterfall } from "~/components/spectrum-waterfall";

const API_BASE = process.env.API_URL || "http://localhost:8080";
// The receiver agent pings every 30s. Two missed pings = stale. The
// admin UI uses 90s; we match that.
const PING_STALE_SECONDS = 90;
const heading = "font-[family-name:var(--font-heading)]";

interface System {
  uuid: string;
  name: string;
  suburb: string | null;
  is_pending: boolean;
  last_message_at: string | null;
  last_ping_at: string | null;
  connected_at: string | null;
}

export async function loader() {
  try {
    const res = await fetch(`${API_BASE}/api/public/systems`);
    return { systems: res.ok ? ((await res.json()) as System[]) : [] };
  } catch {
    return { systems: [] };
  }
}

export function meta() {
  return [
    { title: "Receiver Network — RFSPager" },
    {
      name: "description",
      content:
        "Live status of the SDR receivers feeding RFSPager. Pick a receiver to watch its real-time RF spectrum.",
    },
  ];
}

/**
 * A receiver is "online" when its WebSocket is currently pinging. We
 * deliberately do NOT fall back to last_message_at — a receiver that's
 * up but sitting on a quiet channel is still online, and a receiver
 * whose backlog happens to include a recent message but whose socket
 * has dropped is offline.
 */
function isOnline(sys: System): boolean {
  if (!sys.last_ping_at) return false;
  const diffMs = Date.now() - new Date(sys.last_ping_at).getTime();
  return diffMs <= PING_STALE_SECONDS * 1000;
}

function timeAgo(dateStr: string | null): string {
  if (!dateStr) return "No messages yet";
  const mins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export default function SystemsPage({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const systems: System[] = loaderData?.systems || [];
  const onlineCount = systems.filter((s) => isOnline(s)).length;

  // Filter to only approved (non-pending) systems for the public list; we
  // still surface a small "pending" badge under the title.
  const visibleSystems = useMemo(
    () => systems.filter((s) => !s.is_pending),
    [systems],
  );

  const [selectedUuid, setSelectedUuid] = useState<string | null>(
    () => searchParams.get("uuid") || visibleSystems[0]?.uuid || null,
  );
  const [search, setSearch] = useState("");

  // Pick a default selection once data is in.
  useEffect(() => {
    if (!selectedUuid && visibleSystems.length > 0) {
      setSelectedUuid(visibleSystems[0].uuid);
    }
  }, [selectedUuid, visibleSystems]);

  // Sync selected → URL so refresh + share keep state.
  useEffect(() => {
    const p = new URLSearchParams(searchParams);
    if (selectedUuid) p.set("uuid", selectedUuid);
    else p.delete("uuid");
    setSearchParams(p, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedUuid]);

  const filteredSystems = useMemo(() => {
    if (!search.trim()) return visibleSystems;
    const s = search.trim().toLowerCase();
    return visibleSystems.filter(
      (sys) =>
        sys.name.toLowerCase().includes(s) ||
        (sys.suburb ?? "").toLowerCase().includes(s) ||
        sys.uuid.toLowerCase().includes(s),
    );
  }, [visibleSystems, search]);

  const selected = systems.find((s) => s.uuid === selectedUuid) || null;
  const pendingCount = systems.length - visibleSystems.length;

  return (
    <div className="flex flex-1 overflow-hidden bg-gray-50 h-[calc(100dvh-4rem)]">
      {/* ——— Sidebar ——— */}
      <aside className="w-72 lg:w-80 bg-white border-r border-gray-200 flex flex-col shrink-0 hidden md:flex">
        <div className="px-5 py-4 border-b border-gray-100">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-1">
            Receiver network
          </p>
          <h1 className={`${heading} text-xl font-extrabold text-gray-900 tracking-tight`}>
            Our receivers
          </h1>
          <div className="flex items-center gap-3 mt-3 text-xs">
            <span className="inline-flex items-center gap-1.5 text-emerald-700 font-semibold">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
              {onlineCount} online
            </span>
            <span className="text-gray-300">·</span>
            <span className="text-gray-500">{visibleSystems.length - onlineCount} offline</span>
            {pendingCount > 0 && (
              <>
                <span className="text-gray-300">·</span>
                <span className="text-gray-400">{pendingCount} pending</span>
              </>
            )}
          </div>
        </div>

        <div className="px-3 py-3 border-b border-gray-100">
          <div className="relative">
            <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              placeholder="Search receivers…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full pl-9 pr-3 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none transition-all"
            />
          </div>
        </div>

        <div className="flex-1 overflow-y-auto px-2 pb-4 pt-2 space-y-px">
          {filteredSystems.length === 0 ? (
            <div className="px-3 py-8 text-center text-sm text-gray-400">
              No receivers match.
            </div>
          ) : (
            filteredSystems.map((s) => {
              const online = isOnline(s);
              const active = s.uuid === selectedUuid;
              return (
                <button
                  key={s.uuid}
                  onClick={() => setSelectedUuid(s.uuid)}
                  className={`w-full text-left px-3 py-2.5 rounded-md flex items-start gap-3 transition-colors ${
                    active ? "bg-gray-900 text-white" : "hover:bg-gray-50"
                  }`}
                >
                  <span className="relative mt-1 flex w-2 h-2 shrink-0">
                    {online && !active && (
                      <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-50 animate-ping" />
                    )}
                    <span
                      className={`relative inline-flex w-2 h-2 rounded-full ${
                        online ? "bg-emerald-500" : active ? "bg-white/40" : "bg-gray-300"
                      }`}
                    />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className={`text-sm font-semibold truncate ${active ? "text-white" : "text-gray-900"}`}>
                      {s.name || "Unnamed system"}
                    </p>
                    <p
                      className={`text-[11px] truncate mt-0.5 ${
                        active ? "text-white/60" : "text-gray-400"
                      }`}
                    >
                      {s.suburb ? `${s.suburb} · ` : ""}
                      {timeAgo(s.last_message_at)}
                    </p>
                  </div>
                </button>
              );
            })
          )}
        </div>
      </aside>

      {/* ——— Detail ——— */}
      <main className="flex-1 min-w-0 overflow-y-auto">
        {selected ? (
          <SystemDetail key={selected.uuid} system={selected} />
        ) : (
          <div className="h-full flex items-center justify-center text-sm text-gray-400 px-6">
            Pick a receiver on the left.
          </div>
        )}
      </main>
    </div>
  );
}

function SystemDetail({ system }: { system: System }) {
  const online = isOnline(system);
  return (
    <div className="px-4 md:px-8 py-6 md:py-8 max-w-5xl mx-auto">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-end gap-4 md:gap-8 mb-6">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-2">
            <div
              className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold tracking-[0.12em] ${
                online ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
              }`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${online ? "bg-emerald-500 animate-pulse" : "bg-gray-400"}`} />
              {online ? "ONLINE" : "OFFLINE"}
            </div>
            {system.suburb && (
              <span className="text-[10px] text-gray-400 font-bold uppercase tracking-wider">
                {system.suburb}
              </span>
            )}
          </div>
          <h2 className={`${heading} text-3xl md:text-4xl font-extrabold text-gray-900 leading-tight tracking-tight`}>
            {system.name || "Unnamed receiver"}
          </h2>
          <p className="text-xs text-gray-400 mt-1.5 font-mono break-all">{system.uuid}</p>
        </div>
      </div>

      {/* Spectrum */}
      <div className="rounded-2xl bg-[#0a0a0c] ring-1 ring-white/5 overflow-hidden shadow-xl mb-6">
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/5 bg-black/40">
          <div className="flex items-center gap-2 text-[11px] font-mono text-white/60">
            <span className="text-white/40">RF spectrum</span>
            <span className="text-white/20">·</span>
            <span className={online ? "text-emerald-400" : "text-white/40"}>
              {online ? "live" : "no signal"}
            </span>
          </div>
          <span className="text-[10px] text-white/30 font-mono uppercase tracking-wider">
            real-time SDR
          </span>
        </div>
        <div className="p-4">
          <SpectrumWaterfall
            url={`/api/public/systems/${system.uuid}/spectrum`}
            height={360}
          />
        </div>
      </div>

      {/* Connection info */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mb-6">
        <InfoCard
          label="Status"
          value={online ? "Online" : "Offline"}
          accent={online ? "#10b981" : "#9ca3af"}
        />
        <InfoCard
          label="Last ping"
          value={system.last_ping_at ? timeAgo(system.last_ping_at) : "Never"}
          accent={online ? "#10b981" : undefined}
        />
        <InfoCard
          label="Last message"
          value={system.last_message_at ? timeAgo(system.last_message_at) : "—"}
        />
      </div>

      <UptimeRibbon uuid={system.uuid} />

      {/* Transparency footer */}
      <div className="rounded-xl bg-white border border-gray-100 px-5 py-4 text-sm text-gray-600 leading-relaxed">
        <p className="font-semibold text-gray-900 mb-1">What you're looking at</p>
        <p>
          This is a live RF spectrum from one of our software-defined-radio
          receivers tuned to the NSW Public Safety Network. Bright vertical
          streaks are radio transmissions — pages, voice channels, control
          bursts. Our decoder pulls the POCSAG pager messages out of this raw
          signal and pushes them to the feed.
        </p>
      </div>
    </div>
  );
}

interface UptimeData {
  uuid: string;
  window_days: number;
  window_start: string;
  uptime_percent: number;
  downtime_seconds: number;
  downtime_intervals: Array<{ from: string; to: string | null }>;
  currently_offline: boolean;
}

function UptimeRibbon({ uuid }: { uuid: string }) {
  const [data, setData] = useState<UptimeData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetch(`/api/public/systems/${uuid}/uptime`)
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => {
        if (cancelled) return;
        setData(j);
      })
      .catch(() => !cancelled && setData(null))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [uuid]);

  if (loading) {
    return (
      <div className="rounded-xl bg-white border border-gray-100 px-5 py-4 mb-6 text-xs text-gray-400">
        Loading uptime…
      </div>
    );
  }
  if (!data) return null;

  // Build a 168-segment array (one per hour over 7 days), each marked up/down.
  const SEGMENTS = 168; // 24 * 7
  const windowStart = new Date(data.window_start).getTime();
  const windowEnd = Date.now();
  const windowMs = windowEnd - windowStart;
  const segMs = windowMs / SEGMENTS;

  // Backend serves `null` when there are no intervals (Go nil slice). Treat as empty.
  const intervals = data.downtime_intervals ?? [];
  const downRanges = intervals.map((iv) => ({
    from: new Date(iv.from).getTime(),
    to: iv.to ? new Date(iv.to).getTime() : windowEnd,
  }));

  const segments: ("up" | "down" | "partial")[] = [];
  for (let i = 0; i < SEGMENTS; i++) {
    const segStart = windowStart + i * segMs;
    const segEnd = segStart + segMs;
    let downInSeg = 0;
    for (const r of downRanges) {
      const overlapStart = Math.max(segStart, r.from);
      const overlapEnd = Math.min(segEnd, r.to);
      if (overlapEnd > overlapStart) downInSeg += overlapEnd - overlapStart;
    }
    const ratio = downInSeg / segMs;
    segments.push(ratio === 0 ? "up" : ratio >= 0.9 ? "down" : "partial");
  }

  const pct = data.uptime_percent;
  const pctColor = pct >= 99 ? "text-emerald-600" : pct >= 95 ? "text-amber-600" : "text-[#F7241D]";

  return (
    <div className="rounded-xl bg-white border border-gray-100 px-5 py-4 mb-6">
      <div className="flex items-end justify-between mb-3">
        <div>
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-1">
            Uptime · last 7 days
          </p>
          <p className={`${heading} text-2xl font-extrabold tabular-nums ${pctColor}`}>
            {pct.toFixed(2)}%
          </p>
        </div>
        <div className="text-right">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-1">
            Downtime
          </p>
          <p className="text-sm font-mono text-gray-700 tabular-nums">
            {formatDuration(data.downtime_seconds)}
          </p>
        </div>
      </div>

      {/* Bar */}
      <div className="flex gap-[1px] h-7 rounded-md overflow-hidden bg-gray-100">
        {segments.map((s, i) => {
          const bg =
            s === "up"
              ? "bg-emerald-500"
              : s === "partial"
                ? "bg-amber-400"
                : "bg-[#F7241D]";
          const hr = Math.floor((i / SEGMENTS) * 168);
          const daysAgo = Math.floor(hr / 24);
          const title = `${daysAgo === 0 ? "today" : `${daysAgo}d ago`} · ${s === "up" ? "online" : s === "partial" ? "partial outage" : "offline"}`;
          return (
            <span key={i} title={title} className={`flex-1 ${bg}`} />
          );
        })}
      </div>
      <div className="flex justify-between mt-1.5 text-[10px] text-gray-400 tabular-nums">
        <span>7 days ago</span>
        <span>now</span>
      </div>
    </div>
  );
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h < 24) return `${h}h ${m}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

function InfoCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: string;
}) {
  return (
    <div className="rounded-xl bg-white border border-gray-100 px-4 py-3">
      <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-1">{label}</p>
      <p className="text-base font-semibold text-gray-900" style={accent ? { color: accent } : undefined}>
        {value}
      </p>
    </div>
  );
}
