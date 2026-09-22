import { ArrowUp, Pause, Play } from "lucide-react";

import type { Connection } from "./use-pager-feed";

/**
 * The head of the timeline: the point new pages grow from. It carries the
 * connection state, how fresh the newest page is, and the pause control.
 */
export function LiveStatus({
  connection,
  paused,
  pendingCount,
  newestAgo,
  onTogglePause,
}: {
  connection: Connection;
  paused: boolean;
  pendingCount: number;
  newestAgo: string | null;
  onTogglePause: () => void;
}) {
  let dot: string;
  let title: string;
  let detail: string;

  if (paused) {
    dot = "bg-gray-400";
    title = "Paused";
    detail =
      pendingCount === 0
        ? "New pages will wait here until you resume."
        : pendingCount === 1
          ? "1 new page is waiting."
          : `${pendingCount} new pages are waiting.`;
  } else if (connection === "live") {
    dot = "bg-[#F7241D]";
    title = "Live";
    detail = newestAgo ? `Newest page ${newestAgo}.` : "Listening for the next page.";
  } else if (connection === "reconnecting") {
    dot = "bg-amber-500";
    title = "Reconnecting";
    detail = "The feed will pick up where it left off.";
  } else {
    dot = "bg-gray-300";
    title = "Connecting";
    detail = "Opening the live stream.";
  }

  const pulsing = !paused && connection === "live";

  return (
    <div className="flex items-center gap-3 border-b border-gray-200 bg-white px-4 py-3 sm:px-6">
      <span className="relative flex h-2.5 w-2.5 shrink-0" aria-hidden>
        {pulsing && (
          <span className="absolute inline-flex h-full w-full rounded-full bg-[#F7241D] opacity-60 motion-safe:animate-ping" />
        )}
        <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${dot}`} />
      </span>
      <p className="min-w-0 flex-1 truncate text-sm text-gray-500">
        <span className="font-semibold text-gray-900">{title}</span>{" "}
        <span suppressHydrationWarning>{detail}</span>
      </p>
      <button
        type="button"
        onClick={onTogglePause}
        className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
          paused
            ? "border-[#F7241D] bg-[#F7241D] text-white hover:bg-[#d91f19]"
            : "border-gray-200 bg-white text-gray-700 hover:border-gray-300 hover:bg-gray-50"
        }`}
      >
        {paused ? <Play className="size-3.5" fill="currentColor" /> : <Pause className="size-3.5" fill="currentColor" />}
        {paused ? "Resume" : "Pause"}
      </button>
    </div>
  );
}

/** Floats while the reader is down in history and pages have arrived above. */
export function NewPagesPill({ count, onJump }: { count: number; onJump: () => void }) {
  if (count <= 0) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-6 z-30 flex justify-center px-4 md:pl-72 lg:pl-80">
      <button
        type="button"
        onClick={onJump}
        className="pointer-events-auto inline-flex items-center gap-2 rounded-full bg-gray-900 px-4 py-2 text-sm font-semibold text-white shadow-lg outline-none ring-1 ring-white/10 transition-colors hover:bg-gray-800 focus-visible:ring-2 focus-visible:ring-[#F7241D]/60 animate-in fade-in-0 slide-in-from-bottom-2 duration-200 motion-reduce:animate-none"
      >
        <ArrowUp className="size-4" aria-hidden />
        <span aria-live="polite">{count === 1 ? "1 new page" : `${count} new pages`}</span>
      </button>
    </div>
  );
}
