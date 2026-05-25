import { useEffect, useState, useRef, useCallback } from "react";
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
    { name: "description", content: "Live feed of NSW PSN Emergency Services pager messages. Updated in real-time via SSE." },
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
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString("en-AU", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  return d.toLocaleDateString("en-AU", { day: "2-digit", month: "short" }) + " " + d.toLocaleTimeString("en-AU", { hour: "2-digit", minute: "2-digit" });
}

function ago(dateStr: string) {
  const mins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h`;
  return `${Math.floor(hrs / 24)}d`;
}

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
  const debounce = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (e) => {
      try {
        const msg: Message = JSON.parse(e.data);
        if (paused) { setBuffered((n) => n + 1); return; }
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
    } catch {} finally {
      setLoadingMore(false);
    }
  }, [page, loadingMore, hasMore, search, agencyId]);

  // Infinite scroll via IntersectionObserver
  const sentinelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      ([entry]) => { if (entry.isIntersecting) loadMore(); },
      { rootMargin: "200px" },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [loadMore]);

  const updateParam = useCallback((key: string, val: string) => {
    const p = new URLSearchParams(searchParams);
    if (val) p.set(key, val); else p.delete(key);
    setSearchParams(p, { replace: true });
  }, [searchParams, setSearchParams]);

  const onSearch = (v: string) => {
    setSearch(v);
    clearTimeout(debounce.current);
    debounce.current = setTimeout(() => updateParam("search", v), 300);
  };

  const onAgency = (v: string) => {
    setAgencyId(v);
    updateParam("agency_id", v);
  };

  return (
    <div className="flex flex-col flex-1">
      {/* Toolbar */}
      <div className="bg-white border-b border-gray-200 px-4 py-2.5 flex items-center gap-3 shrink-0">
        {/* Status pill */}
        <div className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-bold tracking-wide ${connected ? "bg-green-50 text-green-700" : "bg-red-50 text-red-600"}`}>
          <div className={`w-1.5 h-1.5 rounded-full ${connected ? "bg-green-500 animate-pulse" : "bg-red-400"}`} />
          {connected ? "LIVE" : "OFFLINE"}
        </div>

        {/* Search */}
        <div className="relative flex-1 max-w-md">
          <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            placeholder="Search messages..."
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            className="w-full pl-9 pr-8 py-1.5 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
          />
          {search && (
            <button onClick={() => onSearch("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
          )}
        </div>

        {/* Agency */}
        <select
          value={agencyId}
          onChange={(e) => onAgency(e.target.value)}
          className="text-sm bg-gray-50 border border-gray-200 rounded-lg py-1.5 px-3 focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none max-w-[220px] transition-all"
        >
          <option value="">All Agencies</option>
          {agencies.map((a) => (
            <option key={a.id} value={a.id}>{a.name} ({a.capcode_count})</option>
          ))}
        </select>

        <div className="flex-1" />

        {/* Pause */}
        <button
          onClick={() => { setPaused(!paused); setBuffered(0); }}
          className={`text-xs font-bold px-3.5 py-1.5 rounded-lg border transition-all ${
            paused
              ? "bg-[#F7241D] text-white border-[#F7241D] shadow-sm"
              : "bg-white text-gray-500 border-gray-200 hover:border-gray-300"
          }`}
        >
          {paused ? `▶ Resume${buffered ? ` (${buffered})` : ""}` : "⏸ Pause"}
        </button>

        <span className="text-xs text-gray-400 tabular-nums font-mono">{messages.length}</span>
      </div>

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        <table className="w-full text-[13px]">
          <thead className="sticky top-0 z-10">
            <tr className="bg-gray-50 border-b border-gray-200 text-left">
              <th className="px-3 py-2 w-[130px] text-xs font-semibold text-gray-400 uppercase tracking-wider">Time</th>
              <th className="px-3 py-2 w-[150px] text-xs font-semibold text-gray-400 uppercase tracking-wider">Capcode</th>
              <th className="px-3 py-2 w-[130px] text-xs font-semibold text-gray-400 uppercase tracking-wider hidden lg:table-cell">Agency</th>
              <th className="px-3 py-2 text-xs font-semibold text-gray-400 uppercase tracking-wider">Message</th>
              <th className="px-3 py-2 w-[50px] text-xs font-semibold text-gray-400 uppercase tracking-wider text-right hidden md:table-cell">Age</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-50">
            {messages.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-3 py-20 text-center text-gray-400 text-sm">
                  {search || agencyId ? "No messages match your filters" : "Waiting for messages..."}
                </td>
              </tr>
            ) : (
              messages.map((msg, i) => (
                <tr key={msg.id} className={`hover:bg-red-50/30 transition-colors ${i === 0 && connected && !paused ? "bg-green-50/30" : "bg-white"}`}>
                  <td className="px-3 py-2 text-gray-400 tabular-nums whitespace-nowrap font-mono text-xs">
                    {fmtTime(msg.created_at)}
                  </td>
                  <td className="px-3 py-2">
                    <span
                      className="inline-block px-2 py-0.5 rounded text-[11px] font-bold text-white truncate max-w-[140px]"
                      style={{ backgroundColor: msg.capcode_colour || "#6b7280" }}
                    >
                      {msg.capcode_alias || msg.address}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-gray-400 text-xs truncate max-w-[130px] hidden lg:table-cell">
                    {msg.agency_name || "\u2014"}
                  </td>
                  <td className="px-3 py-2 text-gray-700 font-mono text-xs leading-relaxed whitespace-pre-wrap">
                    {msg.message}
                  </td>
                  <td className="px-3 py-2 text-gray-300 text-xs text-right tabular-nums whitespace-nowrap hidden md:table-cell">
                    {ago(msg.created_at)}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
        <div ref={sentinelRef} />
        {loadingMore && (
          <div className="py-4 text-center text-xs text-gray-400">Loading more...</div>
        )}
        {!hasMore && messages.length > 0 && (
          <div className="py-4 text-center text-xs text-gray-300">End of messages</div>
        )}
      </div>
    </div>
  );
}
