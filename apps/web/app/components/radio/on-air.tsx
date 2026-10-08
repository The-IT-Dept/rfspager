import { Headphones, Volume2, VolumeX } from "lucide-react";

import { agencyColour } from "./agency";
import { callTitle } from "./call-row";
import type { LiveStream } from "./live-audio";
import type { Connection } from "./use-radio-feed";
import type { RadioCall, Talkgroup } from "./types";

const heading = "font-[family-name:var(--font-heading)]";

/**
 * The head of the timeline: what's on air right now. While listening, each
 * call carries a trace of its own audio level, drawn from the decoded voice
 * as it plays — the one place the page moves on its own.
 */
export function OnAir({
  connection,
  active,
  live,
  talkgroups,
  filterLabel,
}: {
  connection: Connection;
  active: RadioCall[];
  live: {
    listening: boolean;
    start: () => void;
    stop: () => void;
    muted: boolean;
    setMuted: (m: boolean) => void;
    streams: LiveStream[];
    error: string | null;
  };
  talkgroups: Talkgroup[];
  filterLabel: string;
}) {
  const tgByID = new Map(talkgroups.map((t) => [t.tgid, t]));
  const status =
    connection === "live"
      ? active.length
        ? `${active.length} on air`
        : "Quiet right now"
      : connection === "reconnecting"
        ? "Reconnecting"
        : "Connecting";

  return (
    <div className="border-b border-gray-200 bg-white">
      <div className="flex items-center gap-3 px-4 py-3 sm:px-6">
        <span className="relative flex size-2.5 shrink-0" aria-hidden>
          {connection === "live" && active.length > 0 && (
            <span className="absolute inline-flex size-full rounded-full bg-[#F7241D] opacity-60 motion-safe:animate-ping" />
          )}
          <span
            className={`relative inline-flex size-2.5 rounded-full ${
              connection === "live" ? (active.length ? "bg-[#F7241D]" : "bg-gray-400") : "bg-amber-500"
            }`}
          />
        </span>
        <p className="min-w-0 flex-1 truncate text-sm text-gray-500">
          <span className="font-semibold text-gray-900">{status}</span>{" "}
          {live.listening ? `Listening to ${filterLabel}.` : `Listen to hear ${filterLabel} as it's spoken.`}
        </p>
        {live.listening && (
          <button
            type="button"
            onClick={() => live.setMuted(!live.muted)}
            aria-label={live.muted ? "Unmute" : "Mute"}
            className="grid size-8 place-items-center rounded-lg border border-gray-200 text-gray-600 outline-none hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
          >
            {live.muted ? <VolumeX className="size-4" /> : <Volume2 className="size-4" />}
          </button>
        )}
        <button
          type="button"
          onClick={live.listening ? live.stop : live.start}
          className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
            live.listening
              ? "border-gray-200 bg-white text-gray-700 hover:bg-gray-50"
              : "border-[#F7241D] bg-[#F7241D] text-white hover:bg-[#d91f19]"
          }`}
        >
          <Headphones className="size-3.5" />
          {live.listening ? "Stop listening" : "Listen live"}
        </button>
      </div>

      {live.error && <p className="px-4 pb-3 text-sm text-amber-700 sm:px-6">{live.error}</p>}

      {live.listening && live.streams.length > 0 && (
        <ul className="space-y-2 px-4 pb-4 sm:px-6">
          {visibleStreams(live.streams).shown.map((s) => {
            const tg = tgByID.get(s.tgid);
            return (
              <li
                key={s.callId}
                className={`flex items-center gap-3 transition-opacity duration-700 ${s.state === "playing" ? "" : "opacity-40"}`}
              >
                <span className={`${heading} w-36 shrink-0 truncate text-sm font-bold text-gray-900 sm:w-44`}>
                  {tg?.alpha_tag ?? `Talkgroup ${s.tgid}`}
                </span>
                {s.state === "queued" ? (
                  <span className="text-sm text-gray-500">Waiting, {s.bufferedS.toFixed(1)} s held</span>
                ) : (
                  <Trace levels={s.levels} colour={agencyColour(tg?.agency)} />
                )}
              </li>
            );
          })}
          {visibleStreams(live.streams).more > 0 && (
            <li className="pl-[9.75rem] text-sm text-gray-400 sm:pl-[11.75rem]">
              {visibleStreams(live.streams).more} more waiting
            </li>
          )}
        </ul>
      )}

      {!live.listening && active.length > 0 && (
        <ul className="flex flex-wrap gap-2 px-4 pb-4 sm:px-6">
          {active.map((c) => (
            <li key={c.id} className="flex items-center gap-2 rounded-lg bg-gray-50 py-1 pl-2 pr-3 text-sm">
              <span aria-hidden className="h-4 w-[3px] rounded-full" style={{ backgroundColor: agencyColour(c.tg_agency) }} />
              <span className="font-semibold text-gray-900">{callTitle(c)}</span>
              {c.encrypted && <span className="text-gray-400">encrypted</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** A scrolling bar trace of loudness: newest on the right, 20 ms per bar. */
function Trace({ levels, colour }: { levels: number[]; colour: string }) {
  const BARS = 96;
  const shown = levels.slice(-BARS);
  const pad = BARS - shown.length;
  return (
    <svg viewBox={`0 0 ${BARS * 3} 28`} preserveAspectRatio="none" className="h-7 min-w-0 flex-1" aria-hidden>
      {shown.map((v, i) => {
        const h = Math.max(1.5, v * 26);
        return <rect key={i} x={(pad + i) * 3} y={14 - h / 2} width={2} height={h} rx={1} fill={colour} />;
      })}
    </svg>
  );
}

// The playing call (and the one that just finished, fading), then the next
// two in line; the rest of the queue is just a count.
const SHOW_WAITING = 2;
function visibleStreams(streams: LiveStream[]) {
  const finished = streams.filter((s) => s.state === "done" && s.levels.length > 0);
  const playing = streams.filter((s) => s.state === "playing");
  const waiting = streams.filter((s) => s.state === "queued");
  return { shown: [...finished, ...playing, ...waiting.slice(0, SHOW_WAITING)], more: Math.max(0, waiting.length - SHOW_WAITING) };
}
