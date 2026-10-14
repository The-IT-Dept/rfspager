import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";

import { PublicCoverageMap } from "~/components/coverage/public-coverage-map";
import { ago } from "~/components/pager/time";
import { HEAT_RAMP } from "~/lib/coverage";
import {
  COVERAGE_DAYS,
  DEFAULT_COVERAGE_DAYS,
  HEARD_COLOR,
  NOT_HEARD_COLOR,
  RECEIVER_COLOR,
  fetchCoverage,
  fetchCoverageSSR,
  summarise,
  type CoverageDays,
  type PublicCoverage,
  type PublicCoverageDistrict,
} from "~/lib/public-coverage";
import type { Route } from "./+types/coverage";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

export async function loader() {
  return { coverage: await fetchCoverageSSR(API_BASE, DEFAULT_COVERAGE_DAYS) };
}

export function meta() {
  const description =
    "Where RFSPager's own radio receivers can hear the NSW RFS paging network, which districts we hear, and where we still need a receiver.";
  return [
    { title: "Coverage — RFSPager" },
    { name: "description", content: description },
    { property: "og:title", content: "RFSPager coverage" },
    { property: "og:description", content: description },
  ];
}

function fmt(n: number | null | undefined): string {
  if (n == null) return "—";
  return n.toLocaleString("en-AU");
}

export default function Coverage({ loaderData }: Route.ComponentProps) {
  const [days, setDays] = useState<CoverageDays>(DEFAULT_COVERAGE_DAYS);
  const cache = useRef(new Map<CoverageDays, PublicCoverage>());
  if (loaderData.coverage && !cache.current.has(DEFAULT_COVERAGE_DAYS)) {
    cache.current.set(DEFAULT_COVERAGE_DAYS, loaderData.coverage);
  }
  const [data, setData] = useState<PublicCoverage | null>(loaderData.coverage);
  const [loading, setLoading] = useState(false);
  const [toggleError, setToggleError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const choose = (d: CoverageDays) => {
    if (d === days && data && !toggleError) return;
    abortRef.current?.abort();
    setToggleError(null);
    const cached = cache.current.get(d);
    if (cached) {
      setDays(d);
      setData(cached);
      setLoading(false);
      return;
    }
    const ac = new AbortController();
    abortRef.current = ac;
    setLoading(true);
    fetchCoverage(d, ac.signal)
      .then((res) => {
        cache.current.set(d, res);
        setDays(d);
        setData(res);
      })
      .catch(() => {
        if (ac.signal.aborted) return;
        setToggleError(`Couldn't load the ${d}-day view. Try again shortly.`);
      })
      .finally(() => {
        if (!ac.signal.aborted) setLoading(false);
      });
  };
  useEffect(() => () => abortRef.current?.abort(), []);

  // Relative times depend on the reader's clock, so render them after mount.
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(t);
  }, []);

  const summary = summarise(data);

  return (
    <div className="flex-1 bg-gray-50 md:grid md:h-[calc(100dvh-4rem)] md:flex-none md:grid-cols-[22rem_minmax(0,1fr)] md:grid-rows-[auto_minmax(0,1fr)] md:overflow-hidden lg:grid-cols-[26rem_minmax(0,1fr)]">
      {/* Title + window toggle. Top of the rail on desktop, above the map on a phone. */}
      <header className="border-gray-200 bg-white px-5 pb-5 pt-7 sm:px-6 md:col-start-1 md:row-start-1 md:border-r">
        <h1 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Coverage</h1>
        <p className="mt-2 text-sm leading-relaxed text-gray-500">
          Where our receivers hear the RFS paging network. We don't scrape websites: every page in RFSPager came off
          the air through a receiver someone hosts, so this map is the coverage.
        </p>
        <div className="mt-5 flex items-center justify-between gap-3">
          <WindowToggle days={days} loading={loading} onChange={choose} />
          {data?.generated_at && now != null && (
            <p className="text-xs text-gray-400" suppressHydrationWarning>
              Updated {ago(data.generated_at, now)}
            </p>
          )}
        </div>
        {toggleError && (
          <p role="alert" className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-800">
            {toggleError}
          </p>
        )}
      </header>

      <div className="border-t border-gray-200 md:col-start-2 md:row-span-2 md:row-start-1 md:border-t-0">
        <div className="relative h-[58dvh] min-h-[20rem] md:h-full md:min-h-0">
          <div className={`h-full transition-opacity duration-200 ${loading ? "opacity-60" : ""}`}>
            <PublicCoverageMap data={data} />
          </div>
          <div className="pointer-events-none absolute bottom-4 left-4 z-10 hidden rounded-lg border border-gray-200 bg-white/95 px-3 py-2.5 shadow-sm backdrop-blur md:block">
            <Legend sitesHeard={summary?.sitesHeard} sitesTotal={summary?.sitesTotal} />
          </div>
        </div>
        {/* On a phone the legend sits under the map rather than over it. */}
        <div className="border-y border-gray-200 bg-white px-5 py-3 md:hidden">
          <Legend sitesHeard={summary?.sitesHeard} sitesTotal={summary?.sitesTotal} inline />
        </div>
      </div>

      <div className="border-gray-200 bg-white md:col-start-1 md:row-start-2 md:overflow-y-auto md:border-r">
        <dl className="grid grid-cols-3 divide-x divide-gray-200 border-b border-gray-200 md:border-t">
          <Stat value={fmt(summary?.receivers)} label="Receivers" />
          <Stat
            value={summary ? `${summary.districtsHeard}/${summary.districtsTotal}` : "—"}
            label="Districts heard"
          />
          <Stat value={fmt(summary?.pages)} label={`Pages, ${days} days`} />
        </dl>

        <div className="px-5 py-5 sm:px-6">
          <div className="rounded-xl bg-gray-950 p-5 text-white">
            <p className={`${heading} text-base font-bold leading-snug`}>Help us cover more of NSW.</p>
            <p className="mt-2 text-sm leading-relaxed text-white/65">
              Grey dots are RFS paging sites we can't hear yet. Live near one? Host a receiver.
            </p>
            <Link
              to="/host"
              className="mt-4 inline-flex h-10 items-center rounded-lg bg-[#F7241D] px-4 text-sm font-semibold text-white transition-colors hover:bg-[#d91f19]"
            >
              Host a receiver
            </Link>
          </div>
        </div>

        <DistrictList districts={data?.districts ?? []} days={days} now={now} hasData={data != null} />
      </div>
    </div>
  );
}

function WindowToggle({
  days,
  loading,
  onChange,
}: {
  days: CoverageDays;
  loading: boolean;
  onChange: (d: CoverageDays) => void;
}) {
  return (
    <div role="group" aria-label="Time window" className="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-0.5" aria-busy={loading || undefined}>
      {COVERAGE_DAYS.map((d) => {
        const active = d === days;
        return (
          <button
            key={d}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(d)}
            className={`h-8 rounded-md px-3 text-sm font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
              active ? "bg-white text-gray-900 shadow-sm ring-1 ring-gray-200" : "text-gray-500 hover:text-gray-900"
            }`}
          >
            {d} days
          </button>
        );
      })}
    </div>
  );
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div className="px-4 py-4 sm:px-5">
      <dd className={`${heading} text-2xl font-extrabold tabular-nums leading-none text-gray-900`}>{value}</dd>
      <dt className="mt-1.5 text-xs text-gray-500">{label}</dt>
    </div>
  );
}

function Legend({ sitesHeard, sitesTotal, inline = false }: { sitesHeard?: number; sitesTotal?: number; inline?: boolean }) {
  const gradient = `linear-gradient(to right, ${HEAT_RAMP.slice(1)
    .map(([, c]) => c)
    .join(", ")})`;
  return (
    <div className="text-xs text-gray-700">
      <ul className={inline ? "grid grid-cols-2 gap-x-4 gap-y-1.5" : "space-y-1.5"}>
        <li className="flex items-center gap-2">
          <span aria-hidden className="ml-0.5 h-2.5 w-2.5 rotate-45 rounded-[2px] border border-white shadow" style={{ background: RECEIVER_COLOR }} />
          Receiver
        </li>
        <li className="flex items-center gap-2">
          <span aria-hidden className="h-2.5 w-2.5 rounded-full" style={{ background: HEARD_COLOR }} />
          Paging site we hear{sitesHeard != null ? ` (${sitesHeard})` : ""}
        </li>
        <li className="flex items-center gap-2">
          <span aria-hidden className="h-2.5 w-2.5 rounded-full" style={{ background: NOT_HEARD_COLOR }} />
          Not heard yet{sitesTotal != null && sitesHeard != null ? ` (${sitesTotal - sitesHeard})` : ""}
        </li>
        <li className="flex items-center gap-2">
          <span aria-hidden className="h-2 w-6 rounded-full" style={{ background: gradient }} />
          Where incidents were paged
        </li>
      </ul>
    </div>
  );
}

function DistrictList({
  districts,
  days,
  now,
  hasData,
}: {
  districts: PublicCoverageDistrict[];
  days: CoverageDays;
  now: number | null;
  hasData: boolean;
}) {
  const [heard, silent] = useMemo(() => {
    const h = districts.filter((d) => d.heard).sort((a, b) => b.pages - a.pages || a.name.localeCompare(b.name));
    const s = districts.filter((d) => !d.heard).sort((a, b) => a.name.localeCompare(b.name));
    return [h, s];
  }, [districts]);

  if (!hasData) {
    return <p className="px-6 pb-10 pt-2 text-sm text-gray-500">The district list will be back when the coverage data is.</p>;
  }

  return (
    <div className="pb-8">
      <DistrictGroup title="Heard" count={heard.length} note={`Pages decoded in the last ${days} days`}>
        {heard.length === 0 ? (
          <p className="px-6 py-4 text-sm text-gray-500">No districts heard in this window.</p>
        ) : (
          heard.map((d) => (
            <li key={d.name} className="flex items-center gap-3 px-6 py-2">
              <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ background: HEARD_COLOR }} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium text-gray-900">{d.name}</span>
                {d.last_heard_at && (
                  <span className="block text-xs text-gray-500" suppressHydrationWarning>
                    {now != null ? `Last heard ${ago(d.last_heard_at, now)}` : " "}
                  </span>
                )}
              </span>
              <span className="shrink-0 text-sm tabular-nums text-gray-600">{d.pages.toLocaleString("en-AU")}</span>
            </li>
          ))
        )}
      </DistrictGroup>
      <DistrictGroup title="Not heard yet" count={silent.length} note="No pages in this window">
        {silent.length === 0 ? (
          <p className="px-6 py-4 text-sm text-gray-500">Every RFS district was heard in this window.</p>
        ) : (
          silent.map((d) => (
            <li key={d.name} className="flex items-center gap-3 px-6 py-2">
              <span aria-hidden className="h-2 w-2 shrink-0 rounded-full" style={{ background: NOT_HEARD_COLOR }} />
              <span className="min-w-0 flex-1 truncate text-sm text-gray-700">{d.name}</span>
              <span className="shrink-0 text-xs text-gray-400" suppressHydrationWarning>
                {d.last_heard_at && now != null ? `Last heard ${ago(d.last_heard_at, now)}` : ""}
              </span>
            </li>
          ))
        )}
      </DistrictGroup>
    </div>
  );
}

function DistrictGroup({ title, count, note, children }: { title: string; count: number; note: string; children: React.ReactNode }) {
  return (
    <section className="pt-4">
      <div className="flex items-baseline justify-between gap-3 px-6 pb-1">
        <h2 className={`${heading} text-sm font-bold text-gray-900`}>
          {title} <span className="font-semibold text-gray-400">{count}</span>
        </h2>
        <p className="text-xs text-gray-400">{note}</p>
      </div>
      <ul>{children}</ul>
    </section>
  );
}
