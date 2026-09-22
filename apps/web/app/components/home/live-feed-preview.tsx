import { useEffect, useState } from "react";
import { Link } from "react-router";

import { MessageRow } from "~/components/pager/message-row";
import { ago } from "~/components/pager/time";
import type { Message } from "~/components/pager/types";

const KEEP = 7;

/**
 * The real feed, on the front page. Same rows as /pager, same SSE stream;
 * nothing here is staged.
 */
export function LiveFeedPreview({ initial }: { initial: Message[] }) {
  const [messages, setMessages] = useState<Message[]>(initial.slice(0, KEEP));
  const [live, setLive] = useState(false);
  const [now, setNow] = useState<number | null>(null);
  const [liveIds] = useState(() => new Set<number>());

  useEffect(() => {
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.onmessage = (e) => {
      try {
        const msg: Message = JSON.parse(e.data);
        if (typeof msg?.id !== "number") return;
        liveIds.add(msg.id);
        setMessages((prev) => (prev.some((m) => m.id === msg.id) ? prev : [msg, ...prev].slice(0, KEEP)));
      } catch {}
    };
    return () => es.close();
  }, [liveIds]);

  const newest = messages[0];
  const newestAgo = now && newest ? ago(newest.created_at, now) : null;

  return (
    <div className="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm">
      <div className="flex items-center gap-3 border-b border-gray-200 px-4 py-3 sm:px-5">
        <span className="relative flex h-2.5 w-2.5" aria-hidden>
          {live && <span className="absolute inline-flex h-full w-full rounded-full bg-[#F7241D] opacity-60 motion-safe:animate-ping" />}
          <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${live ? "bg-[#F7241D]" : "bg-gray-300"}`} />
        </span>
        <p className="min-w-0 flex-1 truncate text-sm text-gray-500">
          <span className="font-semibold text-gray-900">{live ? "Live" : "Connecting"}</span>{" "}
          <span suppressHydrationWarning>{newestAgo ? `Newest page ${newestAgo}.` : ""}</span>
        </p>
        <Link to="/pager" className="shrink-0 text-sm font-semibold text-[#F7241D] hover:underline">
          Open the feed
        </Link>
      </div>
      {messages.length === 0 ? (
        <p className="px-5 py-16 text-center text-sm text-gray-500">Waiting for the first page.</p>
      ) : (
        <ul className="[&>li:last-child]:border-b-0">
          {messages.map((m) => (
            <MessageRow key={m.id} msg={m} arrivedLive={liveIds.has(m.id)} showReceiver={false} />
          ))}
        </ul>
      )}
    </div>
  );
}
