import { useCallback, useEffect, useRef, useState } from "react";

import { getCustomerToken } from "~/lib/customer-api";
import type { CallPage, RadioCall, Talkgroup } from "./types";

const PAGE_SIZE = 50;
const MAX_ROWS = 1000;
const RECONNECT_MS = 3_000;

export type Connection = "connecting" | "live" | "reconnecting";

async function api<T>(path: string): Promise<T | null> {
  const token = getCustomerToken();
  const res = await fetch(`/api/v2/mobile/radio${path}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  }).catch(() => null);
  if (!res?.ok) return null;
  return res.json();
}

/**
 * The call timeline: the newest page from the API, then every call the
 * server announces over /radio/events — once when it starts, again when it
 * ends, when its audio lands and when its transcript arrives. Each
 * announcement replaces the row with the same id.
 */
export function useRadioFeed({
  enabled,
  tgid,
  freshToken,
}: {
  enabled: boolean;
  tgid: number | null;
  freshToken: () => Promise<string | null>;
}) {
  const [calls, setCalls] = useState<RadioCall[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [talkgroups, setTalkgroups] = useState<Talkgroup[]>([]);
  const [connection, setConnection] = useState<Connection>("connecting");

  const tgRef = useRef(tgid);
  tgRef.current = tgid;
  const liveIds = useRef(new Set<number>());

  // First page, again whenever the talkgroup filter changes.
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setLoading(true);
    const q = new URLSearchParams({ limit: String(PAGE_SIZE) });
    if (tgid) q.set("tg", String(tgid));
    api<CallPage>(`/calls?${q}`).then((page) => {
      if (cancelled) return;
      setCalls(page?.data ?? []);
      setHasMore(Boolean(page?.has_more));
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [enabled, tgid]);

  const loadTalkgroups = useCallback(() => {
    api<{ data: Talkgroup[] } | Talkgroup[]>("/talkgroups").then((res) => {
      const list = Array.isArray(res) ? res : (res?.data ?? []);
      setTalkgroups(list.filter((t) => t.last_seen_at));
    });
  }, []);
  useEffect(() => {
    if (!enabled) return;
    loadTalkgroups();
    const t = setInterval(loadTalkgroups, 60_000);
    return () => clearInterval(t);
  }, [enabled, loadTalkgroups]);

  const upsert = useCallback((call: RadioCall) => {
    if (tgRef.current && call.tgid !== tgRef.current) return;
    setCalls((prev) => {
      const i = prev.findIndex((c) => c.id === call.id);
      if (i >= 0) {
        const next = prev.slice();
        next[i] = call;
        return next;
      }
      liveIds.current.add(call.id);
      return [call, ...prev].slice(0, MAX_ROWS);
    });
  }, []);

  useEffect(() => {
    if (!enabled) return;
    let es: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let closed = false;

    const connect = async () => {
      const token = await freshToken();
      if (closed) return;
      if (!token) {
        setConnection("reconnecting");
        retry = setTimeout(connect, RECONNECT_MS);
        return;
      }
      es = new EventSource(`/api/v2/mobile/radio/events?token=${encodeURIComponent(token)}`);
      es.onopen = () => setConnection("live");
      es.addEventListener("call", (e) => {
        try {
          upsert(JSON.parse((e as MessageEvent).data));
        } catch {
          // ignore a malformed event
        }
      });
      // The browser would retry with the same URL, and the token in it may
      // have expired; reconnect ourselves with a fresh one.
      es.onerror = () => {
        es?.close();
        setConnection("reconnecting");
        if (!closed) retry = setTimeout(connect, RECONNECT_MS);
      };
    };
    connect();
    return () => {
      closed = true;
      es?.close();
      clearTimeout(retry);
    };
  }, [enabled, freshToken, upsert]);

  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore || !calls.length) return;
    setLoadingMore(true);
    const q = new URLSearchParams({ limit: String(PAGE_SIZE), before_id: String(calls[calls.length - 1].id) });
    if (tgRef.current) q.set("tg", String(tgRef.current));
    const page = await api<CallPage>(`/calls?${q}`);
    setCalls((prev) => {
      const seen = new Set(prev.map((c) => c.id));
      return [...prev, ...(page?.data ?? []).filter((c) => !seen.has(c.id))];
    });
    setHasMore(Boolean(page?.has_more));
    setLoadingMore(false);
  }, [calls, hasMore, loadingMore]);

  const arrivedLive = useCallback((id: number) => liveIds.current.has(id), []);

  return { calls, hasMore, loading, loadingMore, loadMore, talkgroups, connection, arrivedLive };
}
