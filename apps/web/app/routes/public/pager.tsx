import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import type { Route } from "./+types/pager";

const API_BASE = process.env.API_URL || "http://localhost:8080";

export async function loader({ request }: Route.LoaderArgs) {
  try {
    const url = new URL(request.url);
    const params = new URLSearchParams();
    params.set("page", url.searchParams.get("page") || "1");
    params.set("per_page", "100");
    if (url.searchParams.get("search")) params.set("search", url.searchParams.get("search")!);
    if (url.searchParams.get("agency_id")) params.set("agency_id", url.searchParams.get("agency_id")!);

    const [msgRes, agencyRes] = await Promise.all([
      fetch(`${API_BASE}/api/public/messages?${params}`),
      fetch(`${API_BASE}/api/public/agencies`),
    ]);

    return {
      messages: msgRes.ok ? await msgRes.json() : { data: [], has_more: false },
      agencies: agencyRes.ok ? await agencyRes.json() : [],
    };
  } catch {
    return { messages: { data: [], has_more: false }, agencies: [] };
  }
}

export function meta() {
  return [
    { title: "Live Pager Feed — RFSPager" },
    { name: "description", content: "Live feed of NSW PSN Emergency Services pager messages. Streaming in real time." },
  ];
}

interface Message {
  id: number;
  address: string;
  message: string;
  created_at: string;
  capcode_alias: string | null;
  capcode_icon: string | null;
  capcode_colour: string | null;
  agency_name: string | null;
}

interface Agency {
  id: number;
  name: string;
  capcode_count: number;
}

function fmtTime(dateStr: string) {
  const d = new Date(dateStr);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString("en-AU", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
  }
  return d.toLocaleDateString("en-AU", { day: "2-digit", month: "short" }) + " " + d.toLocaleTimeString("en-AU", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function ago(dateStr: string) {
  const mins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h`;
  return `${Math.floor(hrs / 24)}d`;
}

const heading = "font-[family-name:var(--font-heading)]";

export default function Pager({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [messages, setMessages] = useState<Message[]>(loaderData?.messages?.data || []);
  const agencies: Agency[] = loaderData?.agencies || [];
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const [buffered, setBuffered] = useState(0);
  const [search, setSearch] = useState(searchParams.get("search") || "");
  const [agencyId, setAgencyId] = useState(searchParams.get("agency_id") || "");
  const [page, setPage] = useState(1);
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(loaderData?.messages?.has_more ?? true);
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Rolling rate (messages per minute, last 60 sec)
  const arrivalsRef = useRef<number[]>([]);
  const [rate, setRate] = useState(0);

  useEffect(() => {
    const t = setInterval(() => {
      const cutoff = Date.now() - 60_000;
      arrivalsRef.current = arrivalsRef.current.filter((ts) => ts > cutoff);
      setRate(arrivalsRef.current.length);
    }, 1000);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (e) => {
      try {
        const msg: Message = JSON.parse(e.data);
        arrivalsRef.current.push(Date.now());
        if (paused) {
          setBuffered((n) => n + 1);
          return;
        }
        setMessages((prev) => [msg, ...prev].slice(0, 500));
      } catch {}
    };
    return () => es.close();
  }, [paused]);

  useEffect(() => {
    if (loaderData?.messages?.data) {
      setMessages(loaderData.messages.data);
      setHasMore(loaderData.messages.has_more ?? false);
      setPage(1);
    }
  }, [loaderData?.messages?.data]);

  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    const nextPage = page + 1;
    const params = new URLSearchParams();
    params.set("page", String(nextPage));
    params.set("per_page", "100");
    if (search) params.set("search", search);
    if (agencyId) params.set("agency_id", agencyId);

    try {
      const res = await fetch(`/api/public/messages?${params}`);
      if (res.ok) {
        const data = await res.json();
        setMessages((prev) => [...prev, ...(data.data || [])]);
        setHasMore(data.has_more ?? false);
        setPage(nextPage);
      }
    } catch {
      // ignore
    } finally {
      setLoadingMore(false);
    }
  }, [page, loadingMore, hasMore, search, agencyId]);

  const sentinelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) loadMore();
      },
      { rootMargin: "300px" },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [loadMore]);

  const updateParam = useCallback(
    (key: string, val: string) => {
      const p = new URLSearchParams(searchParams);
      if (val) p.set(key, val);
      else p.delete(key);
      setSearchParams(p, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const onSearch = (v: string) => {
    setSearch(v);
    if (debounce.current) clearTimeout(debounce.current);
    debounce.current = setTimeout(() => updateParam("search", v), 300);
  };

  const onAgency = (v: string) => {
    setAgencyId(v);
    updateParam("agency_id", v);
  };

  const visibleAgencies = useMemo(
    () => [...agencies].sort((a, b) => b.capcode_count - a.capcode_count),
    [agencies],
  );

  const activeAgency = agencies.find((a) => String(a.id) === agencyId) || null;

  return (
    <div className="flex flex-col flex-1 bg-gray-50">
      {/* ——— Big header ——— */}
      <header className="bg-white border-b border-gray-200 shrink-0">
        <div className="max-w-[1600px] mx-auto px-4 md:px-8 py-4 md:py-5 flex flex-col md:flex-row md:items-end gap-4 md:gap-8">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-3 mb-1.5">
              <div
                className={`flex items-center gap-2 px-2.5 py-1 rounded-full text-[11px] font-bold tracking-[0.12em] ${
                  connected ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
                }`}
              >
                <span className="relative flex w-2 h-2">
                  {connected && <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />}
                  <span className={`relative inline-flex w-2 h-2 rounded-full ${connected ? "bg-emerald-500" : "bg-gray-400"}`} />
                </span>
                {connected ? "LIVE" : "OFFLINE"}
              </div>
              <span className="text-xs text-gray-400 font-medium uppercase tracking-wider">
                NSW Public Safety Network
              </span>
            </div>
            <h1 className={`${heading} text-2xl md:text-3xl font-extrabold text-gray-900 leading-tight tracking-tight`}>
              {activeAgency ? activeAgency.name : "Live pager feed"}
              {search && (
                <span className="text-gray-400 font-bold"> · "{search}"</span>
              )}
            </h1>
          </div>

          {/* Stats */}
          <div className="flex items-center gap-6 md:gap-10 shrink-0">
            <div>
              <p className={`${heading} text-2xl md:text-3xl font-extrabold text-gray-900 leading-none tabular-nums`}>
                {messages.length}
              </p>
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mt-1">Loaded</p>
            </div>
            <div>
              <p
                className={`${heading} text-2xl md:text-3xl font-extrabold leading-none tabular-nums ${
                  rate > 0 ? "text-[#F7241D]" : "text-gray-300"
                }`}
              >
                {rate}
              </p>
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mt-1">Last 60s</p>
            </div>
          </div>
        </div>

        {/* Toolbar row */}
        <div className="border-t border-gray-100 max-w-[1600px] mx-auto px-4 md:px-8 py-3 flex items-center gap-3 flex-wrap">
          {/* Search */}
          <div className="relative flex-1 min-w-[220px] max-w-md">
            <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              placeholder="Search messages…"
              value={search}
              onChange={(e) => onSearch(e.target.value)}
              className="w-full pl-9 pr-9 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none transition-all"
            />
            {search && (
              <button onClick={() => onSearch("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            )}
          </div>

          <div className="flex-1" />

          {/* Pause */}
          <button
            onClick={() => {
              setPaused(!paused);
              setBuffered(0);
            }}
            className={`text-xs font-bold px-3.5 py-2 rounded-lg border transition-all flex items-center gap-1.5 ${
              paused
                ? "bg-[#F7241D] text-white border-[#F7241D] shadow-sm hover:bg-[#d91f19]"
                : "bg-white text-gray-600 border-gray-200 hover:border-gray-300"
            }`}
          >
            {paused ? (
              <>
                <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                Resume{buffered ? ` (${buffered})` : ""}
              </>
            ) : (
              <>
                <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M6 4h4v16H6V4zm8 0h4v16h-4V4z" /></svg>
                Pause
              </>
            )}
          </button>
        </div>

        {/* Agency chips */}
        {visibleAgencies.length > 0 && (
          <div className="border-t border-gray-100">
            <div className="max-w-[1600px] mx-auto px-4 md:px-8 py-2.5 overflow-x-auto scrollbar-none">
              <div className="flex items-center gap-2 min-w-max">
                <button
                  onClick={() => onAgency("")}
                  className={`text-xs font-semibold px-3 py-1.5 rounded-full transition-all shrink-0 ${
                    !agencyId
                      ? "bg-gray-900 text-white"
                      : "bg-gray-100 text-gray-600 hover:bg-gray-200"
                  }`}
                >
                  All agencies
                </button>
                {visibleAgencies.map((a) => {
                  const active = String(a.id) === agencyId;
                  return (
                    <button
                      key={a.id}
                      onClick={() => onAgency(active ? "" : String(a.id))}
                      className={`text-xs font-semibold px-3 py-1.5 rounded-full transition-all shrink-0 inline-flex items-center gap-2 ${
                        active
                          ? "bg-[#F7241D] text-white"
                          : "bg-gray-100 text-gray-600 hover:bg-gray-200"
                      }`}
                    >
                      {a.name}
                      <span className={`text-[10px] tabular-nums ${active ? "text-white/80" : "text-gray-400"}`}>
                        {a.capcode_count}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
        )}
      </header>

      {/* ——— Feed ——— */}
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-[1600px] mx-auto">
          {messages.length === 0 ? (
            <div className="px-6 py-32 text-center">
              <p className="text-gray-400 text-sm">
                {search || agencyId ? "No messages match your filters." : "Waiting for messages…"}
              </p>
            </div>
          ) : (
            <ul className="divide-y divide-gray-100">
              {messages.map((msg, i) => {
                const isNewest = i === 0 && connected && !paused;
                return (
                  <li
                    key={msg.id}
                    className={`group relative px-4 md:px-8 py-3 hover:bg-white transition-colors ${
                      isNewest ? "bg-emerald-50/40 animate-in slide-in-from-top-2 fade-in-0 duration-300" : ""
                    }`}
                  >
                    {/* Active hover bar */}
                    <span
                      className="absolute left-0 top-0 bottom-0 w-0.5 opacity-0 group-hover:opacity-100 transition-opacity"
                      style={{ backgroundColor: msg.capcode_colour || "#F7241D" }}
                    />

                    <div className="flex gap-3 md:gap-5">
                      {/* Time */}
                      <div className="shrink-0 w-[64px] md:w-[92px] pt-0.5">
                        <p className="text-[11px] md:text-xs font-mono text-gray-400 tabular-nums leading-tight">
                          {fmtTime(msg.created_at)}
                        </p>
                        <p className="text-[10px] text-gray-300 tabular-nums mt-0.5 hidden md:block">
                          {ago(msg.created_at)} ago
                        </p>
                      </div>

                      {/* Capcode */}
                      <div className="shrink-0 w-[120px] md:w-[160px] pt-0.5">
                        <span
                          className="inline-block px-2 py-0.5 rounded text-[11px] font-bold text-white uppercase tracking-wide truncate max-w-full"
                          style={{ backgroundColor: msg.capcode_colour || "#6b7280" }}
                        >
                          {msg.capcode_alias || msg.address}
                        </span>
                        {msg.agency_name && (
                          <p className="text-[10px] text-gray-400 mt-1 truncate hidden md:block">
                            {msg.agency_name}
                          </p>
                        )}
                      </div>

                      {/* Message */}
                      <div className="flex-1 min-w-0">
                        <p className="text-[13px] md:text-[13.5px] font-mono text-gray-800 leading-relaxed whitespace-pre-wrap break-words">
                          {msg.message}
                        </p>
                      </div>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}

          <div ref={sentinelRef} />
          {loadingMore && (
            <div className="py-6 text-center text-xs text-gray-400 font-medium">Loading more…</div>
          )}
          {!hasMore && messages.length > 0 && (
            <div className="py-6 text-center text-[10px] uppercase tracking-widest text-gray-300 font-bold">End of feed</div>
          )}
        </div>
      </div>
    </div>
  );
}
