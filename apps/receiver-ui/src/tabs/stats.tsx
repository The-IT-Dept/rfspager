import { useEffect, useMemo, useRef, useState } from "react";
import { LineChart, type LineSeries, type Threshold } from "../components/charts/line-chart";
import { ErrorNote, Panel, Segmented } from "../components/ui";
import { api, errorMessage, type StatsWindow } from "../lib/api";
import { useEvent, useStreamEpoch } from "../lib/events";
import { fmtDuration } from "../lib/format";
import { WINDOW_MS, channelOrder, channelValue, mergePoints } from "../lib/stats";
import type { ChannelStats, StatsPoint } from "../lib/types";

const WINDOWS: { value: StatsWindow; label: string }[] = [
  { value: "1h", label: "1 h" },
  { value: "6h", label: "6 h" },
  { value: "24h", label: "24 h" },
];

const fmt0 = (v: number) => String(Math.round(v));
const fmt1 = (v: number) => (Math.round(v * 10) / 10).toString();
const fmtPctV = (v: number) => `${Math.round(v)}%`;

export function StatsTab() {
  const [win, setWin] = useState<StatsWindow>("1h");
  const [points, setPoints] = useState<StatsPoint[]>([]);
  const [bucketS, setBucketS] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const epoch = useStreamEpoch();
  const pending = useRef<StatsPoint[]>([]);
  const reqRef = useRef(0);

  useEffect(() => {
    const req = ++reqRef.current;
    setLoading(true);
    pending.current = [];
    api
      .stats(win)
      .then((r) => {
        if (req !== reqRef.current) return;
        const pts = r.points ?? [];
        setPoints(pts);
        // The newest bucket may be partial; the one before it has the full length.
        setBucketS(pts.length >= 2 ? pts[pts.length - 2].interval_s : r.interval_s);
        setError(null);
      })
      .catch((e: unknown) => req === reqRef.current && setError(errorMessage(e)))
      .finally(() => req === reqRef.current && setLoading(false));
  }, [win, epoch]);

  // Live points arrive at the raw interval. Longer windows are bucketed by the
  // server, so fold arrivals into a bucket of the same length before appending;
  // otherwise counter series (pages, codewords) would dip at the live edge.
  useEvent("stats", (pt) => {
    if (loading) return;
    let next: StatsPoint | null = pt;
    if (bucketS > 0 && pt.interval_s > 0 && pt.interval_s < bucketS) {
      pending.current.push(pt);
      const span = pending.current.reduce((a, p) => a + p.interval_s, 0);
      if (span + pt.interval_s / 2 < bucketS) return;
      next = mergePoints(pending.current);
      pending.current = [];
    }
    const add = next;
    setPoints((prev) => {
      const cutoff = add.ts - WINDOW_MS[win];
      return [...prev.filter((p) => p.ts >= cutoff && p.ts < add.ts), add];
    });
  });

  const labels = useMemo(() => channelOrder(points), [points]);
  const times = useMemo(() => points.map((p) => p.ts), [points]);

  const perChannel = (f: (c: ChannelStats) => number, opts?: { dashed?: boolean; suffix?: string }): LineSeries[] =>
    labels.map((label, slot) => ({
      key: `${label}${opts?.suffix ?? ""}`,
      label: opts?.suffix ? `${label} ${opts.suffix}` : label,
      slot,
      dashed: opts?.dashed,
      values: points.map((p) => channelValue(p, label, f)),
    }));

  const sumChannels = (f: (c: ChannelStats) => number) =>
    points.map((p) => (p.channels?.length ? p.channels.reduce((a, c) => a + f(c), 0) : null));

  const charts: {
    title: string;
    note?: string;
    yLabel: string;
    series: LineSeries[];
    yFormat?: (v: number) => string;
    yDomain?: [number, number];
    yFloor?: number;
    integer?: boolean;
    thresholds?: Threshold[];
  }[] = [
    {
      title: "SNR per channel",
      note: "p90 minus p10 block power",
      yLabel: "dB",
      series: perChannel((c) => c.snr_db),
      yFormat: fmt0,
      yFloor: 0,
      thresholds: [{ value: 10, label: "weak" }],
    },
    {
      title: "Signal and noise floor",
      note: "solid: signal (p90) · dashed: noise floor (p10)",
      yLabel: "dBFS",
      series: [
        ...perChannel((c) => c.power_dbfs, { suffix: "signal" }),
        ...perChannel((c) => c.noise_floor_dbfs, { suffix: "floor", dashed: true }),
      ],
      yFormat: fmt0,
    },
    {
      title: "Carrier",
      note: "share of blocks more than 6 dB above the floor",
      yLabel: "%",
      series: perChannel((c) => c.carrier_pct),
      yFormat: fmtPctV,
      yFloor: 0,
    },
    {
      title: "Pages per interval",
      yLabel: "pages",
      series: perChannel((c) => c.pages),
      yFormat: fmt0,
      yFloor: 0,
      integer: true,
    },
    {
      title: "Codewords",
      note: "all channels, per interval",
      yLabel: "codewords",
      series: [
        { key: "clean", label: "clean", slot: 2, values: sumChannels((c) => c.cw_clean) },
        { key: "corrected", label: "corrected", slot: 0, values: sumChannels((c) => c.cw_corrected) },
        { key: "lost", label: "uncorrectable", slot: 1, values: sumChannels((c) => c.cw_uncorrectable) },
      ],
      yFormat: fmt0,
      yFloor: 0,
      integer: true,
    },
    {
      title: "DSP load",
      note: "100% means the Pi is only just keeping up",
      yLabel: "%",
      series: [{ key: "dsp", label: "DSP load", slot: 0, values: points.map((p) => p.dongle.dsp_load * 100) }],
      yFormat: fmtPctV,
      yFloor: 0,
      thresholds: [{ value: 80, label: "80%" }],
    },
    {
      title: "CPU temperature",
      yLabel: "°C",
      series: [{ key: "temp", label: "CPU", slot: 1, values: points.map((p) => (p.host.cpu_temp_c ? p.host.cpu_temp_c : null)) }],
      yFormat: fmt1,
      thresholds: [{ value: 80, label: "throttles near 80 °C" }],
    },
    {
      title: "Link RTT",
      note: "ping to the primary upstream",
      yLabel: "ms",
      series: [{ key: "rtt", label: "RTT", slot: 2, values: points.map((p) => (p.link.rtt_ms ? p.link.rtt_ms : null)) }],
      yFormat: fmt0,
      yFloor: 0,
    },
  ];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented value={win} onChange={setWin} options={WINDOWS} ariaLabel="Time window" />
        <p className="text-[11px] text-muted-foreground">
          {points.length > 0 && bucketS > 0 && <>{fmtDuration(bucketS)} intervals · </>}
          Kept in memory for 24 h; starts empty after a restart.
        </p>
      </div>
      {error && <ErrorNote>Could not load stats: {error}</ErrorNote>}
      <div className="grid gap-3 lg:grid-cols-2">
        {charts.map((c) => (
          <Panel key={c.title} title={c.title} aside={c.note}>
            <LineChart
              times={times}
              series={c.series}
              yLabel={c.yLabel}
              yFormat={c.yFormat}
              yDomain={c.yDomain}
              yFloor={c.yFloor}
              integer={c.integer}
              thresholds={c.thresholds}
              height={200}
              dimmed={loading && points.length > 0}
              emptyText={loading ? "Loading…" : "No stats yet in this window."}
            />
          </Panel>
        ))}
      </div>
    </div>
  );
}
