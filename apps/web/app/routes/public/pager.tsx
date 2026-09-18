import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import type { Route } from "./+types/pager";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

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
    { name: "description", content: "Live feed of NSW PSN Emergency Services pager messages, decoded from our own SDR receivers and streamed in real time." },
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
  system_uuid: string | null;
  system_name: string | null;
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

export default function Pager({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [messages, setMessages] = useState<Message[]>(loaderData?.messages?.data || []);
  const agencies: Agency[] = loaderData?.agencies || [];
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const [buffered, setBuffered] = useState(0);
  const [search, setSearch] = useState(searchParams.get("search") || "");
  const [agencyId, setAgencyId] = useState(searchParams.get("agency_id") || "");
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState<boolean>(loaderData?.messages?.has_more ?? true);
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);

  // How many rows the list may hold. Grows as the reader pages back, so a live
  // message arriving never truncates history they have already scrolled
  // through. Live arrivals alone stay bounded by the headroom.
  const LIVE_HEADROOM = 300;
  const capRef = useRef(500);

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
        setMessages((prev) => {
          if (prev.some((m) => m.id === msg.id)) return prev;
          return [msg, ...prev].slice(0, capRef.current);
        });
      } catch {}
    };
    return () => es.close();
  }, [paused]);

  useEffect(() => {
    if (loaderData?.messages?.data) {
      setMessages(loaderData.messages.data);
      setHasMore(loaderData.messages.has_more ?? false);
      capRef.current = 500;
    }
  }, [loaderData?.messages?.data]);

  // A ref mirror of the in-flight flag: the IntersectionObserver callback and
  // the scroll fallback can both fire before a setState has landed, which would
  // otherwise fire two identical requests for the same cursor.
  const loadingRef = useRef(false);

  // Keep a ref copy so loadMore can read the newest list without being
  // re-created (and re-registering the observer) on every incoming message.
  const messagesRef = useRef<Message[]>(messages);
  useEffect(() => {
    messagesRef.current = messages;
  }, [messages]);

  const loadMore = useCallback(async () => {
    if (loadingRef.current || !hasMore) return;

    // Page from the oldest row we hold, not an offset. The feed prepends live
    // messages, so an OFFSET would shift between requests and skip rows.
    const oldest = messagesRef.current[messagesRef.current.length - 1];
    if (!oldest) return;

    loadingRef.current = true;
    setLoadingMore(true);

    const params = new URLSearchParams();
    params.set("per_page", "100");
    params.set("before_created_at", new Date(oldest.created_at).toISOString());
    params.set("before_id", String(oldest.id));
    if (search) params.set("search", search);
    if (agencyId) params.set("agency_id", agencyId);

    try {
      const res = await fetch(`/api/public/messages?${params}`);
      if (res.ok) {
        const data = await res.json();
        const incoming: Message[] = data.data || [];
        setMessages((prev) => {
          const seen = new Set(prev.map((m) => m.id));
          const merged = [...prev, ...incoming.filter((m) => !seen.has(m.id))];
          capRef.current = merged.length + LIVE_HEADROOM;
          return merged;
        });
        setHasMore(data.has_more ?? false);
      }
    } catch {
      // Network blip — leave hasMore alone so the reader can retry by scrolling.
    } finally {
      loadingRef.current = false;
      setLoadingMore(false);
    }
  }, [hasMore, search, agencyId]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = sentinelRef.current;
    const root = scrollRef.current;
    if (!el || !root) return;

    // `root` must be the scrolling element. With the default (viewport) the
    // sentinel sits inside an overflow-y-auto container and is clipped, so it
    // never reports as intersecting and the feed never pages.
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
      },
      { root, rootMargin: "600px 0px" },
    );
    obs.observe(el);

    // Fallback for cases IntersectionObserver alone handles poorly: momentum
    // scrolling on iOS can outrun observer callbacks, and if the first page is
    // shorter than the container nothing ever scrolls to trigger it.
    const onScroll = () => {
      if (root.scrollHeight - root.scrollTop - root.clientHeight < 600) loadMore();
    };
    root.addEventListener("scroll", onScroll, { passive: true });

    return () => {
      obs.disconnect();
      root.removeEventListener("scroll", onScroll);
    };
  }, [loadMore]);

  // If the loaded rows do not fill the container there is nothing to scroll,
  // so keep pulling until they do (or the feed runs out).
  useEffect(() => {
    const root = scrollRef.current;
    if (!root || loadingMore || !hasMore) return;
    if (root.scrollHeight <= root.clientHeight + 1) loadMore();
  }, [messages, loadingMore, hasMore, loadMore]);

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

  const sortedAgencies = useMemo(
    () => [...agencies].sort((a, b) => b.capcode_count - a.capcode_count),
    [agencies],
  );
  const activeAgency = agencies.find((a) => String(a.id) === agencyId) || null;

  return (
    <div className="flex flex-1 overflow-hidden bg-gray-50 h-[calc(100dvh-4rem)]">
      {/* ——— Sidebar ——— */}
      <aside className="w-72 lg:w-80 bg-white border-r border-gray-200 flex flex-col shrink-0 hidden md:flex">
        {/* Status block */}
        <div className="px-5 py-4 border-b border-gray-100">
          <div className="flex items-center gap-2 mb-1">
            <div
              className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold tracking-[0.12em] ${
                connected ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
              }`}
            >
              <span className="relative flex w-1.5 h-1.5">
                {connected && (
                  <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />
                )}
                <span
                  className={`relative inline-flex w-1.5 h-1.5 rounded-full ${connected ? "bg-emerald-500" : "bg-gray-400"}`}
                />
              </span>
              {connected ? "LIVE" : "OFFLINE"}
            </div>
            <span className="text-[10px] text-gray-400 font-bold uppercase tracking-wider">NSW PSN</span>
          </div>
          <h1 className={`${heading} text-xl font-extrabold text-gray-900 tracking-tight`}>
            Pager feed
          </h1>

          <div className="grid grid-cols-2 gap-4 mt-4">
            <div>
              <p className={`${heading} text-xl font-extrabold text-gray-900 leading-none tabular-nums`}>
                {messages.length}
              </p>
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mt-1">Loaded</p>
            </div>
            <div>
              <p
                className={`${heading} text-xl font-extrabold leading-none tabular-nums ${
                  rate > 0 ? "text-[#F7241D]" : "text-gray-300"
                }`}
              >
                {rate}
              </p>
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mt-1">Last 60s</p>
            </div>
          </div>
        </div>

        {/* Search + pause */}
        <div className="px-3 py-3 border-b border-gray-100 space-y-2">
          <div className="relative">
            <svg
              className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
              />
            </svg>
            <input
              type="text"
              placeholder="Search messages…"
              value={search}
              onChange={(e) => onSearch(e.target.value)}
              className="w-full pl-9 pr-9 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none transition-all"
            />
            {search && (
              <button
                onClick={() => onSearch("")}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            )}
          </div>

          <button
            onClick={() => {
              setPaused(!paused);
              setBuffered(0);
            }}
            className={`w-full text-xs font-bold px-3 py-2 rounded-lg border transition-all inline-flex items-center justify-center gap-1.5 ${
              paused
                ? "bg-[#F7241D] text-white border-[#F7241D] shadow-sm hover:bg-[#d91f19]"
                : "bg-white text-gray-600 border-gray-200 hover:border-gray-300"
            }`}
          >
            {paused ? (
              <>
                <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M8 5v14l11-7z" />
                </svg>
                Resume{buffered ? ` (${buffered})` : ""}
              </>
            ) : (
              <>
                <svg className="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M6 4h4v16H6V4zm8 0h4v16h-4V4z" />
                </svg>
                Pause stream
              </>
            )}
          </button>
        </div>

        {/* Agency filter list */}
        <div className="px-5 pt-3 pb-1.5">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider">Filter by agency</p>
        </div>
        <div className="flex-1 overflow-y-auto px-3 pb-4 space-y-px">
          <FilterRow label="All agencies" count={null} active={!agencyId} onClick={() => onAgency("")} tint="#111827" />
          {sortedAgencies.map((a) => (
            <FilterRow
              key={a.id}
              label={a.name}
              count={a.capcode_count}
              active={String(a.id) === agencyId}
              onClick={() => onAgency(String(a.id) === agencyId ? "" : String(a.id))}
            />
          ))}
        </div>
      </aside>

      {/* ——— Main feed ——— */}
      <main className="flex-1 flex flex-col min-w-0">
        <header className="bg-white border-b border-gray-200 px-4 md:px-6 py-4">
          <div className="flex items-end gap-6">
            <div className="flex-1 min-w-0">
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-1">
                Live stream
              </p>
              <h2
                className={`${heading} text-2xl md:text-3xl font-extrabold text-gray-900 leading-tight tracking-tight truncate`}
              >
                {activeAgency ? activeAgency.name : "All NSW pages"}
                {search && <span className="text-gray-400 font-bold"> · "{search}"</span>}
              </h2>
            </div>
            {/* Mobile-only LIVE pill (sidebar is hidden on small screens) */}
            <div className="md:hidden">
              <div
                className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold tracking-[0.12em] ${
                  connected ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
                }`}
              >
                <span className={`w-1.5 h-1.5 rounded-full ${connected ? "bg-emerald-500 animate-pulse" : "bg-gray-400"}`} />
                {connected ? "LIVE" : "OFFLINE"}
              </div>
            </div>
          </div>
        </header>

        <div ref={scrollRef} className="flex-1 overflow-y-auto overscroll-contain">
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
                    className={`group relative px-4 md:px-6 py-3 hover:bg-white transition-colors ${
                      isNewest ? "bg-emerald-50/40 animate-in slide-in-from-top-2 fade-in-0 duration-300" : ""
                    }`}
                  >
                    <span
                      className="absolute left-0 top-0 bottom-0 w-0.5 opacity-0 group-hover:opacity-100 transition-opacity"
                      style={{ backgroundColor: msg.capcode_colour || "#F7241D" }}
                    />
                    <div className="flex flex-col gap-1.5 md:flex-row md:gap-5">
                      {/* Time and capcode share one line on mobile so the message
                          gets the full column width. `md:contents` dissolves this
                          wrapper at md, restoring the three-column desktop row. */}
                      <div className="flex items-center gap-2.5 md:contents">
                        <div className="shrink-0 md:w-[92px] md:pt-0.5">
                          <p className="text-[11px] md:text-xs font-mono text-gray-400 tabular-nums leading-tight">
                            {fmtTime(msg.created_at)}
                          </p>
                          <p className="text-[10px] text-gray-300 tabular-nums mt-0.5 hidden md:block">
                            {ago(msg.created_at)} ago
                          </p>
                        </div>
                        <div className="min-w-0 flex-1 md:flex-none md:shrink-0 md:w-[160px] md:pt-0.5">
                          <span
                            className="inline-block px-2 py-0.5 rounded text-[11px] font-bold text-white uppercase tracking-wide truncate max-w-full align-middle"
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
                      </div>
                      <div className="flex-1 min-w-0">
                        <p className="text-[13px] md:text-[13.5px] font-mono text-gray-800 leading-relaxed whitespace-pre-wrap break-words">
                          {msg.message}
                        </p>
                        {msg.system_name && (
                          <p className="text-[10px] text-gray-400 mt-1.5 flex items-center gap-1.5 min-w-0">
                            <svg className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2 12h2m16 0h2M12 2v2m0 16v2M5.6 5.6l1.4 1.4m10 10l1.4 1.4M5.6 18.4l1.4-1.4m10-10l1.4-1.4M12 8a4 4 0 100 8 4 4 0 000-8z" />
                            </svg>
                            <span className="text-gray-500 whitespace-nowrap">decoded by</span>
                            {/* Systems page gated off for now — render as plain
                                text rather than a link to a redirect. */}
                            <span className="font-semibold text-gray-500 truncate">
                              {msg.system_name}
                            </span>
                          </p>
                        )}
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
            <div className="py-6 text-center text-[10px] uppercase tracking-widest text-gray-300 font-bold">
              End of feed
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function FilterRow({
  label,
  count,
  active,
  onClick,
  tint,
}: {
  label: string;
  count: number | null;
  active: boolean;
  onClick: () => void;
  tint?: string;
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 rounded-md flex items-center gap-3 transition-colors group ${
        active ? "bg-gray-900 text-white" : "hover:bg-gray-50 text-gray-700"
      }`}
    >
      {tint && (
        <span
          className={`w-1.5 h-1.5 rounded-full shrink-0 ${active ? "opacity-100" : "opacity-70"}`}
          style={{ backgroundColor: tint }}
        />
      )}
      <span className="text-sm font-medium flex-1 truncate">{label}</span>
      {count !== null && (
        <span
          className={`text-[11px] tabular-nums ${
            active ? "text-white/70" : "text-gray-400 group-hover:text-gray-500"
          }`}
        >
          {count.toLocaleString()}
        </span>
      )}
    </button>
  );
}
