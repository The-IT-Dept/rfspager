import type { ChannelStats, StatsPoint } from "./types";

export const WINDOW_MS = { "1h": 3_600_000, "6h": 6 * 3_600_000, "24h": 24 * 3_600_000 } as const;

const avg = (xs: number[]) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0);
const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);

/**
 * Merges raw points into one bucket the way the server does for long windows:
 * counters summed, gauges averaged. Used to fold live 15 s points into the
 * bucket size of a 6 h / 24 h view so counter charts stay comparable.
 */
export function mergePoints(pts: StatsPoint[]): StatsPoint {
  const last = pts[pts.length - 1];
  const labels: string[] = [];
  for (const p of pts) for (const c of p.channels ?? []) if (!labels.includes(c.label)) labels.push(c.label);
  const channels: ChannelStats[] = labels.map((label) => {
    const cs = pts.flatMap((p) => (p.channels ?? []).filter((c) => c.label === label));
    const lc = cs[cs.length - 1];
    return {
      ...lc,
      power_dbfs: avg(cs.map((c) => c.power_dbfs)),
      noise_floor_dbfs: avg(cs.map((c) => c.noise_floor_dbfs)),
      snr_db: avg(cs.map((c) => c.snr_db)),
      carrier_pct: avg(cs.map((c) => c.carrier_pct)),
      sync_locks: sum(cs.map((c) => c.sync_locks)),
      sync_losses: sum(cs.map((c) => c.sync_losses)),
      cw_clean: sum(cs.map((c) => c.cw_clean)),
      cw_corrected: sum(cs.map((c) => c.cw_corrected)),
      cw_uncorrectable: sum(cs.map((c) => c.cw_uncorrectable)),
      cw_chased: sum(cs.map((c) => c.cw_chased ?? 0)),
      pages: sum(cs.map((c) => c.pages)),
      pages_truncated: sum(cs.map((c) => c.pages_truncated)),
    };
  });
  return {
    ...last,
    interval_s: sum(pts.map((p) => p.interval_s)),
    dongle: {
      ...last.dongle,
      iq_buffers: sum(pts.map((p) => p.dongle.iq_buffers)),
      iq_dropped: sum(pts.map((p) => p.dongle.iq_dropped)),
      gaps: sum(pts.map((p) => p.dongle.gaps)),
      dsp_load: avg(pts.map((p) => p.dongle.dsp_load)),
    },
    host: { ...last.host, cpu_temp_c: avg(pts.map((p) => p.host.cpu_temp_c)) },
    link: { ...last.link, rtt_ms: avg(pts.map((p) => p.link.rtt_ms)) },
    channels,
  };
}

/** Channel labels in first-seen order; the index is the chart colour slot, so a channel keeps its hue. */
export function channelOrder(points: StatsPoint[]): string[] {
  const out: string[] = [];
  for (const p of points) for (const c of p.channels ?? []) if (!out.includes(c.label)) out.push(c.label);
  return out;
}

export function channelValue(p: StatsPoint, label: string, f: (c: ChannelStats) => number): number | null {
  const c = p.channels?.find((x) => x.label === label);
  return c ? f(c) : null;
}

export interface CodewordTotals {
  clean: number;
  corrected: number;
  lost: number;
  total: number;
}

export function codewordTotals(points: StatsPoint[], label?: string): CodewordTotals {
  let clean = 0;
  let corrected = 0;
  let lost = 0;
  for (const p of points) {
    for (const c of p.channels ?? []) {
      if (label && c.label !== label) continue;
      clean += c.cw_clean;
      corrected += c.cw_corrected;
      lost += c.cw_uncorrectable;
    }
  }
  return { clean, corrected, lost, total: clean + corrected + lost };
}
