import { useEffect, useState } from "react";

import { ago } from "~/components/pager/time";

export interface ReceiverSummary {
  uuid: string;
  name: string;
  suburb: string | null;
  last_message_at: string | null;
  is_pending: boolean;
}

export const HEARD_RECENTLY_MS = 30 * 60_000;

const heading = "font-[family-name:var(--font-heading)]";

/**
 * Every approved receiver, with the one fact we can stand behind: when we
 * last decoded a page from it. Derived from the messages table, not from a
 * heartbeat, so a receiver that is powered on but hearing nothing does not
 * get to call itself online.
 */
export function ReceiverNetwork({ receivers }: { receivers: ReceiverSummary[] }) {
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);

  const sorted = [...receivers].sort((a, b) => {
    const ta = a.last_message_at ? new Date(a.last_message_at).getTime() : 0;
    const tb = b.last_message_at ? new Date(b.last_message_at).getTime() : 0;
    return tb - ta;
  });

  return (
    <div className="overflow-hidden rounded-xl border border-white/10 bg-white/[0.03]">
      <div className="flex items-baseline justify-between gap-4 border-b border-white/10 px-5 py-3">
        <p className={`${heading} text-sm font-bold text-white`}>Receivers</p>
        <p className="text-xs text-white/50">Last page decoded</p>
      </div>
      {sorted.length === 0 ? (
        <p className="px-5 py-10 text-center text-sm text-white/50">No receivers registered yet.</p>
      ) : (
        <ul>
          {sorted.map((r) => {
            const t = r.last_message_at ? new Date(r.last_message_at).getTime() : null;
            const recent = now != null && t != null && now - t <= HEARD_RECENTLY_MS;
            return (
              <li key={r.uuid} className="flex items-center gap-3 border-b border-white/[0.06] px-5 py-3 last:border-b-0">
                <span
                  aria-hidden
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    recent ? "bg-[#F7241D]" : t ? "bg-white/30" : "border border-white/30 bg-transparent"
                  }`}
                />
                <p className="min-w-0 flex-1 truncate text-sm font-medium text-white/90">
                  {r.name}
                  {r.suburb && <span className="font-normal text-white/45"> {r.suburb}</span>}
                </p>
                <p className="shrink-0 text-sm tabular-nums text-white/55" suppressHydrationWarning>
                  {t == null ? "Not heard yet" : now == null ? "" : ago(r.last_message_at!, now)}
                </p>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
