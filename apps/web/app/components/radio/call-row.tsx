import { Lock, Pause, Play, Siren } from "lucide-react";
import { memo } from "react";

import { clockTime } from "~/components/pager/time";
import { agencyColour, agencyName } from "./agency";
import type { RadioCall } from "./types";

const heading = "font-[family-name:var(--font-heading)]";

export function callTitle(c: {
  tg_alpha_tag: string | null;
  tgid: number | null;
  target_rid: number | null;
  patched_with?: string[] | null;
}) {
  let title: string;
  if (c.tg_alpha_tag) title = c.tg_alpha_tag;
  else if (c.tgid) title = `Talkgroup ${c.tgid}`;
  else return c.target_rid ? `Private call to ${c.target_rid}` : "Private call";
  // A patched keyup is one call heard on several talkgroups.
  return [title, ...(c.patched_with ?? [])].join(" + ");
}

function seconds(c: RadioCall): number | null {
  if (c.audio_ms) return c.audio_ms / 1000;
  if (c.ended_at) return (Date.parse(c.ended_at) - Date.parse(c.started_at)) / 1000;
  return null;
}

/**
 * One transmission. Built like the pager feed's rows: time in a gutter,
 * whose channel it was, who keyed up, and what was said.
 */
export const CallRow = memo(function CallRow({
  call,
  arrivedLive,
  active,
  playing,
  progress,
  failed,
  onToggle,
}: {
  call: RadioCall;
  arrivedLive: boolean;
  active: boolean;
  playing: boolean;
  progress: number;
  failed: boolean;
  onToggle: (c: RadioCall) => void;
}) {
  const time = clockTime(call.started_at);
  const secs = seconds(call);
  const agency = agencyName(call.tg_agency);
  const talker = call.source_alias || (call.source_rid ? `Radio ${call.source_rid}` : null);
  const inProgress = !call.ended_at;

  return (
    <li
      className={`relative flex gap-4 border-b border-gray-100 bg-white px-4 py-3 sm:px-6 md:gap-5 md:py-3.5 ${
        arrivedLive ? "animate-in fade-in-0 slide-in-from-top-1 duration-500 motion-reduce:animate-none" : ""
      }`}
    >
      <span aria-hidden className="absolute inset-y-0 left-0 w-[3px]" style={{ backgroundColor: agencyColour(call.tg_agency) }} />

      <time dateTime={call.started_at} className="hidden w-[4.75rem] shrink-0 pt-1.5 text-[13px] leading-6 tabular-nums text-gray-500 md:block">
        {time}
      </time>

      <PlayButton call={call} active={active} playing={playing} progress={progress} onToggle={onToggle} />

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-x-2.5 leading-6">
          <span className={`${heading} min-w-0 truncate text-[15px] font-bold text-gray-900`}>{callTitle(call)}</span>
          {agency && <span className="hidden min-w-0 truncate text-[13px] text-gray-500 sm:inline">{agency}</span>}
          <time dateTime={call.started_at} className="ml-auto shrink-0 pl-2 text-[13px] tabular-nums text-gray-500 md:hidden">
            {time}
          </time>
        </div>
        <p className="text-[13px] leading-5 text-gray-500">
          {[talker, secs != null && secs > 0 ? `${secs.toFixed(secs < 10 ? 1 : 0)} s` : null].filter(Boolean).join(", ")}
          {call.emergency && (
            <span className="ml-2 inline-flex items-center gap-1 rounded bg-red-50 px-1.5 text-[12px] font-semibold text-[#F7241D]">
              <Siren className="size-3" aria-hidden /> Emergency
            </span>
          )}
        </p>
        <Transcript call={call} inProgress={inProgress} failed={failed} />
      </div>
    </li>
  );
});

function Transcript({ call, inProgress, failed }: { call: RadioCall; inProgress: boolean; failed: boolean }) {
  if (call.encrypted) {
    return (
      <p className="mt-1.5 flex items-center gap-1.5 text-[14px] text-gray-400">
        <Lock className="size-3.5" aria-hidden /> Encrypted. This transmission can't be heard.
      </p>
    );
  }
  if (failed) return <p className="mt-1.5 text-[14px] text-gray-400">Couldn't load this recording. Sign in again and retry.</p>;
  if (call.transcript) {
    return <p className="mt-1.5 max-w-[68ch] text-[15px] leading-[1.6] text-gray-800">“{call.transcript}”</p>;
  }
  if (call.transcript_error === "poor_audio") {
    return <p className="mt-1.5 text-[14px] text-gray-400">Too broken up to transcribe.</p>;
  }
  if (call.transcribed_at) return <p className="mt-1.5 text-[14px] text-gray-400">No speech recognised.</p>;
  if (inProgress) return <p className="mt-1.5 text-[14px] text-gray-400">On air now.</p>;
  if (call.audio_url) return <p className="mt-1.5 text-[14px] text-gray-400">Transcribing…</p>;
  return <p className="mt-1.5 text-[14px] text-gray-400">Not recorded.</p>;
}

function PlayButton({
  call,
  active,
  playing,
  progress,
  onToggle,
}: {
  call: RadioCall;
  active: boolean;
  playing: boolean;
  progress: number;
  onToggle: (c: RadioCall) => void;
}) {
  if (!call.audio_url) {
    return <span aria-hidden className="mt-0.5 hidden size-9 shrink-0 md:block" />;
  }
  const r = 16;
  const circ = 2 * Math.PI * r;
  return (
    <button
      type="button"
      onClick={() => onToggle(call)}
      aria-label={active && playing ? `Pause ${callTitle(call)}` : `Play ${callTitle(call)}`}
      className={`relative mt-0.5 grid size-9 shrink-0 place-items-center rounded-full outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
        active ? "bg-gray-900 text-white" : "bg-gray-100 text-gray-700 hover:bg-gray-200"
      }`}
    >
      {active && (
        <svg aria-hidden viewBox="0 0 36 36" className="absolute inset-0 -rotate-90">
          <circle cx="18" cy="18" r={r} fill="none" stroke="#F7241D" strokeWidth="2.5" strokeDasharray={circ} strokeDashoffset={circ * (1 - progress)} strokeLinecap="round" />
        </svg>
      )}
      {active && playing ? <Pause className="size-3.5" fill="currentColor" /> : <Play className="ml-0.5 size-3.5" fill="currentColor" />}
    </button>
  );
}
