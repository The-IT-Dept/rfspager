import { memo } from "react";

import { clockTime } from "./time";
import type { Message } from "./types";

const heading = "font-[family-name:var(--font-heading)]";

/**
 * One moment on the timeline. The clock sits in a fixed gutter on desktop so
 * the eye can run straight down the times; on phones it moves onto the meta
 * line so the message text gets the whole width.
 */
export const MessageRow = memo(function MessageRow({
  msg,
  arrivedLive,
  showReceiver = true,
}: {
  msg: Message;
  arrivedLive: boolean;
  /** Receiver name on the meta line (desktop only). Off in narrow embeds. */
  showReceiver?: boolean;
}) {
  const colour = msg.capcode_colour || "#d1d5db";
  const who = msg.capcode_alias || msg.address;
  const time = clockTime(msg.created_at);
  const text = msg.message.replace(/\r\n?/g, "\n");

  return (
    <li
      className={`relative flex gap-4 md:gap-5 border-b border-gray-100 bg-white px-4 py-3 sm:px-6 md:py-3.5 ${
        arrivedLive ? "animate-in fade-in-0 slide-in-from-top-1 duration-500 motion-reduce:animate-none" : ""
      }`}
    >
      <span aria-hidden className="absolute inset-y-0 left-0 w-[3px]" style={{ backgroundColor: colour }} />

      <time
        dateTime={msg.created_at}
        className="hidden w-[4.75rem] shrink-0 pt-px text-[13px] leading-6 tabular-nums text-gray-500 md:block"
      >
        {time}
      </time>

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-x-2.5 leading-6">
          <span className={`${heading} min-w-0 truncate text-[15px] font-bold text-gray-900`}>{who}</span>
          {msg.agency_name && (
            <span className="min-w-0 truncate text-[13px] text-gray-500">{msg.agency_name}</span>
          )}
          <time
            dateTime={msg.created_at}
            className="ml-auto shrink-0 pl-2 text-[13px] tabular-nums text-gray-500 md:hidden"
          >
            {time}
          </time>
          {showReceiver && msg.system_name && (
            <span
              className="ml-auto hidden max-w-[16rem] shrink-0 truncate text-xs text-gray-400 lg:block"
              title={`Decoded by ${msg.system_name}`}
            >
              {msg.system_name}
            </span>
          )}
        </div>
        <p className="mt-1 whitespace-pre-wrap break-words font-mono text-[14px] leading-[1.6] text-gray-800 md:text-[15px]">
          {text}
        </p>
      </div>
    </li>
  );
});

export function DayDivider({ label }: { label: string }) {
  return (
    <li className="z-10 border-b border-gray-200 bg-gray-50 px-4 py-1.5 sm:px-6 md:sticky md:top-16">
      <p className="text-xs font-semibold text-gray-600" suppressHydrationWarning>
        {label}
      </p>
    </li>
  );
}

export function SkeletonRows({ count = 3 }: { count?: number }) {
  return (
    <ul aria-hidden className="motion-reduce:animate-none animate-pulse">
      {Array.from({ length: count }).map((_, i) => (
        <li key={i} className="flex gap-4 border-b border-gray-100 bg-white px-4 py-3 sm:px-6 md:gap-5 md:py-3.5">
          <span className="hidden h-4 w-[4.75rem] shrink-0 rounded bg-gray-100 md:block" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2.5">
              <span className="h-4 w-36 rounded bg-gray-200/80" />
              <span className="h-3 w-16 rounded bg-gray-100" />
            </div>
            <span className="mt-2 block h-4 w-[70%] rounded bg-gray-100" />
          </div>
        </li>
      ))}
    </ul>
  );
}
