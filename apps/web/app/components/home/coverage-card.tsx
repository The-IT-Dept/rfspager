import { Link } from "react-router";

import { PublicCoverageMap } from "~/components/coverage/public-coverage-map";
import { HEARD_COLOR, NOT_HEARD_COLOR, RECEIVER_COLOR, summarise, type PublicCoverage } from "~/lib/public-coverage";

const heading = "font-[family-name:var(--font-heading)]";

/**
 * Home page: a compact, non-interactive coverage map that links through to
 * /coverage. Built for the dark "We listen to the radio" section.
 */
export function CoverageCard({ data }: { data: PublicCoverage | null }) {
  const s = summarise(data);
  const days = data?.window.days ?? 90;

  return (
    <Link
      to="/coverage"
      aria-label="Open the coverage map"
      className="group block overflow-hidden rounded-xl border border-white/10 bg-white/[0.03] outline-none transition-colors hover:border-white/25 focus-visible:ring-2 focus-visible:ring-[#F7241D]/60"
    >
      <div className="flex items-baseline justify-between gap-4 border-b border-white/10 px-5 py-3">
        <p className={`${heading} text-sm font-bold text-white`}>Coverage</p>
        <p className="text-xs text-white/50">Last {days} days</p>
      </div>

      <div className="relative h-[20rem] bg-gray-100 sm:h-[24rem]">
        <PublicCoverageMap data={data} compact />
      </div>

      <dl className="grid grid-cols-3 divide-x divide-white/10 border-t border-white/10">
        {[
          { v: s ? s.receivers.toLocaleString("en-AU") : "—", k: "Receivers" },
          { v: s ? `${s.districtsHeard} of ${s.districtsTotal}` : "—", k: "RFS districts heard" },
          { v: s ? s.pages.toLocaleString("en-AU") : "—", k: "Pages decoded" },
        ].map((x) => (
          <div key={x.k} className="px-4 py-3 sm:px-5">
            <dd className={`${heading} text-lg font-extrabold tabular-nums text-white sm:text-xl`}>{x.v}</dd>
            <dt className="mt-0.5 text-xs text-white/50">{x.k}</dt>
          </div>
        ))}
      </dl>

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-white/10 px-5 py-3 text-xs text-white/55">
        <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <Key color={RECEIVER_COLOR} diamond>
            Receiver
          </Key>
          <Key color={HEARD_COLOR}>Site we hear</Key>
          <Key color={NOT_HEARD_COLOR}>Not yet</Key>
        </span>
        <span className="font-semibold text-white/80 transition-colors group-hover:text-white">
          Open the coverage map <span aria-hidden>→</span>
        </span>
      </div>
    </Link>
  );
}

function Key({ color, diamond = false, children }: { color: string; diamond?: boolean; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        aria-hidden
        className={diamond ? "h-2 w-2 rotate-45 rounded-[1px]" : "h-2 w-2 rounded-full"}
        style={{ background: color }}
      />
      {children}
    </span>
  );
}
