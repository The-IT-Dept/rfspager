import { useCallback, useEffect, useRef, useState } from "react";

import type { Agency, Message, MessagePage } from "./types";

const PAGE_SIZE = 50;
// Ceiling on rows held while the reader is at the head of the feed. Trimming
// only ever happens on a live prepend, and live prepends only happen at the
// top, so scrollback the reader has paged into is never cut from under them.
const MAX_ROWS = 1500;
// Live arrivals held back while the reader is paused or scrolled into history.
const MAX_PENDING = 200;
const TOP_THRESHOLD_PX = 160;
// How far below the viewport the sentinel may sit before we fetch the next page.
const NEAR_BOTTOM_PX = 900;

export type Connection = "connecting" | "live" | "reconnecting";

interface Options {
  initial: MessagePage;
  agencies: Agency[];
  /** Committed (URL) search term — what the loaded rows were filtered by. */
  search: string;
  /** Committed (URL) agency id. */
  agencyId: string;
}

export function usePagerFeed({ initial, agencies, search, agencyId }: Options) {
  const [messages, setMessages] = useState<Message[]>(initial.data);
  const [hasMore, setHasMore] = useState(initial.has_more);
  const [loadingMore, setLoadingMore] = useState(false);
  const [connection, setConnection] = useState<Connection>("connecting");
  const [paused, setPaused] = useState(false);
  const [pending, setPending] = useState<Message[]>([]);
  const [atTop, setAtTop] = useState(true);

  // The loader hands us a fresh first page whenever the filter changes.
  useEffect(() => {
    setMessages(initial.data);
    setHasMore(initial.has_more);
    setPending([]);
  }, [initial]);

  // Ref mirrors so the single EventSource and the observer callback can read
  // current values without being torn down and recreated on every change.
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const atTopRef = useRef(atTop);
  atTopRef.current = atTop;
  const filterRef = useRef({ search, agencyId, agencies });
  filterRef.current = { search, agencyId, agencies };
  const messagesRef = useRef(messages);
  messagesRef.current = messages;
  const hasMoreRef = useRef(hasMore);
  hasMoreRef.current = hasMore;

  // Ids that arrived over SSE this session, so only those rows animate in.
  const liveIdsRef = useRef(new Set<number>());
  const trimmedRef = useRef(false);

  // Mirrors the server's filter: agency by id, search as a case-insensitive
  // substring of the message text (the public endpoint uses ILIKE '%term%').
  const matchesFilter = useCallback((m: Message) => {
    const f = filterRef.current;
    if (f.agencyId) {
      const agency = f.agencies.find((a) => String(a.id) === f.agencyId);
      if (!agency || m.agency_name !== agency.name) return false;
    }
    if (f.search && !m.message.toLowerCase().includes(f.search.toLowerCase())) return false;
    return true;
  }, []);

  useEffect(() => {
    const onScroll = () => setAtTop(window.scrollY <= TOP_THRESHOLD_PX);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  const prepend = useCallback((incoming: Message[]) => {
    if (!incoming.length) return;
    setMessages((prev) => {
      const seen = new Set(prev.map((m) => m.id));
      const fresh = incoming.filter((m) => !seen.has(m.id));
      if (!fresh.length) return prev;
      const merged = [...fresh, ...prev];
      if (merged.length > MAX_ROWS) {
        trimmedRef.current = true;
        return merged.slice(0, MAX_ROWS);
      }
      return merged;
    });
  }, []);

  // Rows trimmed off the tail still exist on the server.
  useEffect(() => {
    if (trimmedRef.current) {
      trimmedRef.current = false;
      setHasMore(true);
    }
  }, [messages]);

  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => setConnection("live");
    es.onerror = () => setConnection("reconnecting");
    es.onmessage = (e) => {
      let msg: Message;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      if (!msg || typeof msg.id !== "number" || typeof msg.message !== "string") return;
      if (!matchesFilter(msg)) return;
      liveIdsRef.current.add(msg.id);
      if (pausedRef.current || !atTopRef.current) {
        setPending((prev) =>
          prev.some((p) => p.id === msg.id) ? prev : [msg, ...prev].slice(0, MAX_PENDING),
        );
      } else {
        prepend([msg]);
      }
    };
    return () => es.close();
  }, [matchesFilter, prepend]);

  // Back at the head and not paused: release anything held.
  useEffect(() => {
    if (atTop && !paused && pending.length) {
      prepend(pending);
      setPending([]);
    }
  }, [atTop, paused, pending, prepend]);

  const togglePause = useCallback(() => setPaused((p) => !p), []);
  const arrivedLive = useCallback((id: number) => liveIdsRef.current.has(id), []);

  const jumpToLatest = useCallback(() => {
    setPaused(false);
    window.scrollTo({ top: 0, behavior: window.scrollY > 3000 ? "auto" : "smooth" });
  }, []);

  const loadingRef = useRef(false);

  const loadMore = useCallback(async () => {
    if (loadingRef.current || !hasMoreRef.current) return;
    const oldest = messagesRef.current[messagesRef.current.length - 1];
    if (!oldest) return;

    loadingRef.current = true;
    setLoadingMore(true);

    // Keyset cursor from the oldest row we hold. The timestamp goes through
    // verbatim: re-serialising it would round microseconds to milliseconds
    // and the (created_at, id) comparison could then skip rows.
    const { search, agencyId } = filterRef.current;
    const params = new URLSearchParams({
      per_page: String(PAGE_SIZE),
      before_created_at: oldest.created_at,
      before_id: String(oldest.id),
    });
    if (search) params.set("search", search);
    if (agencyId) params.set("agency_id", agencyId);

    try {
      const res = await fetch(`/api/public/messages?${params}`);
      if (!res.ok) return;
      const page: MessagePage = await res.json();
      // The filter changed while this was in flight; the loader owns the reset.
      if (filterRef.current.search !== search || filterRef.current.agencyId !== agencyId) return;
      const incoming = page.data ?? [];
      setMessages((prev) => {
        const seen = new Set(prev.map((m) => m.id));
        const fresh = incoming.filter((m) => !seen.has(m.id));
        return fresh.length ? [...prev, ...fresh] : prev;
      });
      setHasMore(Boolean(page.has_more) && incoming.length > 0);
    } catch {
      // Network blip: leave hasMore alone so scrolling again retries.
    } finally {
      loadingRef.current = false;
      setLoadingMore(false);
    }
  }, []);

  const sentinelRef = useRef<HTMLDivElement>(null);
  const nearBottomRef = useRef(false);

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      (entries) => {
        nearBottomRef.current = entries.some((e) => e.isIntersecting);
        if (nearBottomRef.current) loadMore();
      },
      { rootMargin: `0px 0px ${NEAR_BOTTOM_PX}px 0px` },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [loadMore]);

  // IntersectionObserver only reports crossings. If a short page leaves the
  // sentinel inside the margin after a load, nothing crosses, so re-measure
  // once the new rows are laid out. Gated on the observer having seen the
  // reader near the bottom, so it cannot fire on a live prepend at the top.
  useEffect(() => {
    if (!nearBottomRef.current || loadingMore || !hasMore) return;
    const el = sentinelRef.current;
    if (!el) return;
    if (el.getBoundingClientRect().top <= window.innerHeight + NEAR_BOTTOM_PX) loadMore();
  }, [messages.length, loadingMore, hasMore, loadMore]);

  return {
    messages,
    hasMore,
    loadingMore,
    connection,
    paused,
    pendingCount: pending.length,
    atTop,
    togglePause,
    jumpToLatest,
    sentinelRef,
    arrivedLive,
  };
}
