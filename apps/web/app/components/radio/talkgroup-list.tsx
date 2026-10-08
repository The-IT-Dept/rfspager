import { agencyColour, agencyName } from "./agency";
import type { Talkgroup } from "./types";

/** Talkgroups heard so far, most recently active first. */
export function TalkgroupList({
  talkgroups,
  activeId,
  onSelect,
  className,
}: {
  talkgroups: Talkgroup[];
  activeId: number | null;
  onSelect: (tgid: number | null) => void;
  className?: string;
}) {
  const item = (selected: boolean) =>
    `flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
      selected ? "bg-gray-900 text-white" : "text-gray-700 hover:bg-gray-100"
    }`;
  return (
    <ul className={className}>
      <li>
        <button type="button" onClick={() => onSelect(null)} className={item(activeId === null)}>
          <span className="flex-1 text-sm font-semibold">All talkgroups</span>
        </button>
      </li>
      {talkgroups.map((t) => {
        const selected = activeId === t.tgid;
        const total = t.calls_clear + t.calls_encrypted;
        return (
          <li key={t.id}>
            <button type="button" onClick={() => onSelect(t.tgid)} className={item(selected)} title={t.description ?? undefined}>
              <span aria-hidden className="h-7 w-[3px] shrink-0 rounded-full" style={{ backgroundColor: agencyColour(t.agency) }} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-semibold">{t.alpha_tag ?? `Talkgroup ${t.tgid}`}</span>
                <span className={`block truncate text-xs ${selected ? "text-gray-300" : "text-gray-500"}`}>
                  {[agencyName(t.agency), t.tgid].filter(Boolean).join(", ")}
                </span>
              </span>
              {total > 0 && (
                <span className={`shrink-0 text-xs tabular-nums ${selected ? "text-gray-300" : "text-gray-400"}`}>
                  {total}
                </span>
              )}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
