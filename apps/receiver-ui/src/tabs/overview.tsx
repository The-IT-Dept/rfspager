import { useEffect, useMemo, useState } from "react";
import { ArrowRight } from "lucide-react";
import { Capcode, PageMessage, QualityBadge, channelTitle } from "../components/page-bits";
import { ErrorNote, Panel, Pill, Tag, Tile } from "../components/ui";
import { api, errorMessage } from "../lib/api";
import { useEvent, useStreamEpoch, useStreamState } from "../lib/events";
import { cn, fmtAgo, fmtClock, fmtFixed, fmtInt, fmtMHz, fmtPct, gainLabel } from "../lib/format";
import { useNow } from "../lib/hooks";
import { codewordTotals } from "../lib/stats";
import { useStatus } from "../lib/status";
import type { ChannelStats, Page, StatsPoint } from "../lib/types";

const LIVE_PAGES = 15;
const CODEWORD_WINDOW_MS = 15 * 60_000;
const CPU_TEMP_WARN_C = 80;
/** SNR that fills the channel bar. */
const SNR_FULL_DB = 40;

export function OverviewTab() {
  const { status } = useStatus();
  const now = useNow();
  const epoch = useStreamEpoch();

  const [pages, setPages] = useState<Page[] | null>(null);
  const [fresh, setFresh] = useState<Set<number>>(() => new Set());
  const [pagesError, setPagesError] = useState<string | null>(null);
  const [points, setPoints] = useState<StatsPoint[]>([]);
  const [statsError, setStatsError] = useState<string | null>(null);
  const [livePageAt, setLivePageAt] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .pages({ limit: LIVE_PAGES })
      .then((r) => {
        if (cancelled) return;
        setPages(r.pages ?? []);
        setPagesError(null);
      })
      .catch((e: unknown) => !cancelled && setPagesError(errorMessage(e)));
    api
      .stats("1h")
      .then((r) => {
        if (cancelled) return;
        const pts = r.points ?? [];
        setPoints(pts.length ? pts : r.latest ? [r.latest] : []);
        setStatsError(null);
      })
      .catch((e: unknown) => !cancelled && setStatsError(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [epoch]);

  useEvent("page", (p) => {
    if (!p.posted) return;
    setLivePageAt((t) => Math.max(t ?? 0, p.ts));
    setPages((prev) => {
      if (!prev) return prev;
      if (prev.some((x) => x.id === p.id)) return prev;
      return [p, ...prev].slice(0, LIVE_PAGES);
    });
    setFresh((s) => new Set(s).add(p.id));
  });

  useEvent("stats", (pt) => {
    setPoints((prev) => {
      const cutoff = pt.ts - 3_600_000;
      return [...prev.filter((x) => x.ts >= cutoff && x.ts !== pt.ts), pt];
    });
  });

  const latest = points.length ? points[points.length - 1] : null;
  const recent = useMemo(
    () => (latest ? points.filter((p) => p.ts > latest.ts - CODEWORD_WINDOW_MS) : []),
    [points, latest],
  );

  const channels = latest?.channels ?? [];
  // Peak over the last 15 min: a single interval between pages has no
  // carrier and reads 2-3 dB, which looks like a fault when it isn't.
  const best = recent
    .flatMap((p) => p.channels ?? [])
    .reduce<ChannelStats | null>((b, c) => (!b || c.snr_db > b.snr_db ? c : b), null);
  const lastPageAt = Math.max(status?.last_page_at ?? 0, livePageAt ?? 0) || null;
  const temp = status?.cpu_temp_c || latest?.host.cpu_temp_c || 0;
  const uvNow = status?.undervoltage_now ?? latest?.host.undervoltage_now ?? false;
  const uvEver = status?.undervoltage_ever ?? latest?.host.undervoltage_ever ?? false;

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        <Tile label="Pages today" value={status ? fmtInt(status.pages_today) : "—"} hint="Pages forwarded since local midnight" />
        <Tile
          label="Last page"
          value={lastPageAt ? fmtAgo(lastPageAt, now) : "none yet"}
          sub={lastPageAt ? fmtClock(lastPageAt) : undefined}
        />
        <Tile
          label="Peak SNR · 15 min"
          value={best ? fmtFixed(best.snr_db, 0) : "—"}
          unit={best ? "dB" : undefined}
          sub={best ? best.label : statsError ? "stats unavailable" : "waiting for stats"}
          hint="Highest channel SNR in any stats interval over the last 15 minutes. SNR only means something while a transmitter is keyed; the Stats tab has the history."
        />
        <Tile
          label="CPU temp"
          value={temp ? fmtFixed(temp, 0) : "—"}
          unit={temp ? "°C" : undefined}
          warn={temp >= CPU_TEMP_WARN_C || uvNow || uvEver}
          sub={
            uvNow ? (
              <span className="font-semibold text-red-700">Undervoltage now: check the power supply</span>
            ) : uvEver ? (
              <span className="text-amber-800">Undervoltage since boot</span>
            ) : temp ? (
              "Power OK"
            ) : undefined
          }
          hint="From the Pi firmware; undervoltage causes dropped samples and missed pages"
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.35fr_1fr]">
        <LivePages pages={pages} fresh={fresh} error={pagesError} />
        <div className="space-y-3">
          <ChannelsPanel channels={channels} recent={recent} latest={latest} error={statsError} />
        </div>
      </div>
    </div>
  );
}

function LivePages({ pages, fresh, error }: { pages: Page[] | null; fresh: Set<number>; error: string | null }) {
  const stream = useStreamState();
  return (
    <Panel
      title="Live pages"
      aside={
        <>
          {stream === "live" ? (
            <Pill tone="ok" pulse>
              live
            </Pill>
          ) : (
            <Pill tone="muted">{stream === "down" ? "reconnecting" : "connecting"}</Pill>
          )}
          <a href="#/pages" className="inline-flex items-center gap-1 font-medium text-foreground hover:underline">
            All pages <ArrowRight className="size-3" />
          </a>
        </>
      }
      bodyClassName="px-0 pb-1"
    >
      {error && <ErrorNote className="mx-3 mb-2">Could not load pages: {error}</ErrorNote>}
      {pages && pages.length === 0 && (
        <p className="px-3 py-8 text-center text-muted-foreground">No pages yet. They appear here as they are decoded.</p>
      )}
      {!pages && !error && <p className="px-3 py-8 text-center text-muted-foreground">Loading…</p>}
      <ul>
        {pages?.map((p) => (
          <li
            key={p.id}
            className={cn(
              "grid grid-cols-[auto_auto_1fr] items-baseline gap-x-3 gap-y-0.5 border-b border-hairline px-3 py-2 last:border-b-0",
              fresh.has(p.id) && "animate-row-in",
            )}
          >
            <span className="font-mono text-[11.5px] text-muted-foreground tabular-nums">{fmtClock(p.ts)}</span>
            <Capcode address={p.address} className="text-[12px]" />
            <span className="min-w-0 max-sm:order-last max-sm:col-span-3">
              <PageMessage page={p} className="line-clamp-3 sm:line-clamp-2" />
            </span>
            <span className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground max-sm:justify-self-end sm:col-start-3">
              <span title={channelTitle(p)}>{p.channel}</span>
              <QualityBadge quality={p.quality} />
            </span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function ChannelsPanel({
  channels,
  recent,
  latest,
  error,
}: {
  channels: ChannelStats[];
  recent: StatsPoint[];
  latest: StatsPoint | null;
  error: string | null;
}) {
  const all = codewordTotals(recent);
  return (
    <Panel title="Channels" aside={<a href="#/stats" className="font-medium text-foreground hover:underline">Stats</a>}>
      {error && <ErrorNote className="mb-2">Could not load stats: {error}</ErrorNote>}
      {!latest && !error && <p className="py-6 text-center text-muted-foreground">Waiting for the first stats interval…</p>}
      <div className="divide-y divide-hairline">
        {channels.map((c) => {
          const cw = codewordTotals(recent, c.label);
          const pct = Math.max(0, Math.min(100, (c.snr_db / SNR_FULL_DB) * 100));
          return (
            <div key={c.label} className="py-2.5 first:pt-0.5">
              <div className="flex items-baseline justify-between gap-3">
                <span className="min-w-0 truncate">
                  <span className="font-semibold">{c.label}</span>{" "}
                  <span className="font-mono text-[11.5px] text-muted-foreground">{fmtMHz(c.freq_hz)}</span>
                </span>
                <span className="font-mono text-[13px] font-semibold tabular-nums">
                  {fmtFixed(c.snr_db, 0)} dB
                </span>
              </div>
              <div
                className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-zinc-200"
                role="meter"
                aria-label={`${c.label} SNR`}
                aria-valuemin={0}
                aria-valuemax={SNR_FULL_DB}
                aria-valuenow={Math.round(c.snr_db)}
              >
                <div className="h-full rounded-full bg-green-600 transition-[width] duration-500" style={{ width: `${pct}%` }} />
              </div>
              <dl className="mt-1.5 grid grid-cols-3 gap-x-2 text-[11px]">
                <Stat k="Floor" v={`${fmtFixed(c.noise_floor_dbfs, 0)} dBFS`} />
                <Stat k="Signal" v={`${fmtFixed(c.power_dbfs, 0)} dBFS`} />
                <Stat k="Carrier" v={`${fmtFixed(c.carrier_pct, 0)}%`} />
              </dl>
              <CodewordLine totals={cw} className="mt-1" />
            </div>
          );
        })}
      </div>
      {latest && (
        <div className="mt-2 border-t border-border pt-2 text-[11px] text-muted-foreground">
          <p>
            Codewords last 15 min, all channels:{" "}
            {all.total ? (
              <>
                <b className="text-foreground">{fmtPct(all.clean / all.total)}</b> clean ·{" "}
                <b className="text-foreground">{fmtPct(all.corrected / all.total)}</b> corrected ·{" "}
                <b className={cn("text-foreground", all.lost / all.total > 0.05 && "text-amber-700")}>{fmtPct(all.lost / all.total)}</b> lost
              </>
            ) : (
              "none decoded"
            )}
          </p>
          <p className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5">
            <span>
              DSP load{" "}
              <b className={cn("text-foreground tabular-nums", latest.dongle.dsp_load > 0.8 && "text-amber-700")}>
                {fmtFixed(latest.dongle.dsp_load * 100, 0)}%
              </b>
            </span>
            <span>
              Link RTT <b className="text-foreground tabular-nums">{latest.link.rtt_ms ? `${latest.link.rtt_ms} ms` : "—"}</b>
            </span>
            <span>
              Gain <b className="text-foreground">{latest.dongle.agc ? "AGC" : gainLabel(latest.dongle.gain_tenths_db)}</b>
            </span>
            {latest.dongle.iq_dropped > 0 && (
              <Tag className="bg-amber-100 text-amber-800" title="IQ buffers dropped in the latest interval: the Pi is falling behind">
                {latest.dongle.iq_dropped} dropped
              </Tag>
            )}
            {!latest.dongle.open && <Tag className="bg-red-100 text-red-800">dongle closed</Tag>}
          </p>
        </div>
      )}
    </Panel>
  );
}

function Stat({ k, v }: { k: string; v: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="truncate font-mono tabular-nums">{v}</dd>
    </div>
  );
}

function CodewordLine({ totals, className }: { totals: ReturnType<typeof codewordTotals>; className?: string }) {
  if (!totals.total) {
    return <p className={cn("text-[11px] text-muted-foreground", className)}>No codewords in the last 15 min</p>;
  }
  const t = totals.total;
  return (
    <div className={cn("text-[11px]", className)}>
      <div className="flex h-1 overflow-hidden rounded-full bg-zinc-200" aria-hidden>
        <div className="bg-green-600" style={{ width: `${(totals.clean / t) * 100}%` }} />
        <div className="bg-sky-500" style={{ width: `${(totals.corrected / t) * 100}%` }} />
        <div className="bg-amber-500" style={{ width: `${(totals.lost / t) * 100}%` }} />
      </div>
      <p className="mt-1 text-muted-foreground">
        {fmtPct(totals.clean / t)} clean · {fmtPct(totals.corrected / t)} corrected · {fmtPct(totals.lost / t)} lost
        <span className="text-zinc-400"> · {fmtInt(t)} codewords</span>
      </p>
    </div>
  );
}
