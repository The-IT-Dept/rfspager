import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Activity, RefreshCw } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import {
  captureDownloadPath,
  downloadUrl,
  errorMessage,
  fetchCaptureJSON,
  getCaptures,
  getCommands,
  getSystemStats,
  normalizeSpectrum,
  postCommand,
  uncorrectableShare,
  type CaptureRow,
  type ChannelConfig,
  type SpectrumCapture,
  type StatsRange,
  type StatsSample,
} from "~/lib/receiver-api";
import { CapturesPanel } from "./captures-panel";
import { SpectrumChart } from "./charts/area-chart";
import { MAX_SERIES } from "./charts/chart-utils";
import { LineChart, type LineSeries } from "./charts/line-chart";
import { fmtDate, fmtMHz } from "./format";

const RANGE_STEP: Record<StatsRange, string> = { "6h": "1m", "24h": "5m" };
const RANGE_LABEL: Record<StatsRange, string> = { "6h": "Last 6 hours", "24h": "Last 24 hours" };
const REFRESH_MS = 60_000;
const SNAPSHOT_TIMEOUT_MS = 120_000;
const SNAPSHOT_POLL_MS = 2_000;
const SNR_WARN_DB = 10;
const UNCORRECTABLE_WARN_PCT = 20;

type Snapshot =
  | { phase: "idle" }
  | { phase: "requesting" | "waiting" | "loading"; message: string }
  | { phase: "error"; message: string }
  | { phase: "ready"; capture: CaptureRow; data: SpectrumCapture };

interface Props {
  systemId: string;
  channels: ChannelConfig[];
  online: boolean;
}

export function DiagnosticsTab({ systemId, channels, online }: Props) {
  const [range, setRange] = useState<StatsRange>("6h");
  const [samples, setSamples] = useState<StatsSample[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const reqRef = useRef(0);

  const load = useCallback(
    async (r: StatsRange) => {
      const req = ++reqRef.current;
      setLoading(true);
      try {
        const rows = await getSystemStats(systemId, r, RANGE_STEP[r]);
        if (req !== reqRef.current) return; // a newer request superseded this one
        setSamples(rows);
        setError(null);
      } catch (e: unknown) {
        if (req === reqRef.current) setError(errorMessage(e));
      } finally {
        if (req === reqRef.current) setLoading(false);
      }
    },
    [systemId],
  );

  useEffect(() => {
    void load(range);
    const t = setInterval(() => void load(range), REFRESH_MS);
    return () => clearInterval(t);
  }, [load, range]);

  const { times, snr, unc } = useMemo(() => buildSeries(samples ?? [], channels), [samples, channels]);

  // Spectrum snapshot: post the command, watch the captures list until the
  // file is ready, then fetch and chart it.
  const [snap, setSnap] = useState<Snapshot>({ phase: "idle" });
  const [capturesKey, setCapturesKey] = useState(0);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const snapBusy = snap.phase === "requesting" || snap.phase === "waiting" || snap.phase === "loading";

  const requestSnapshot = async () => {
    setSnap({ phase: "requesting", message: "Sending the capture command…" });
    try {
      const cmd = await postCommand(systemId, { type: "capture", params: { kind: "spectrum" } });
      setCapturesKey((k) => k + 1);
      // The POST reply embeds the capture row; the id is also stored in the command params.
      const captureId = cmd.capture?.id ?? (typeof cmd.params?.capture_id === "string" ? cmd.params.capture_id : null);
      const requestedAt = Date.parse(cmd.created_at) || Date.now();
      const deadline = Date.now() + SNAPSHOT_TIMEOUT_MS;
      setSnap({ phase: "waiting", message: "Waiting for the receiver…" });
      while (Date.now() < deadline) {
        await sleep(SNAPSHOT_POLL_MS);
        if (!alive.current) return;
        const [caps, cmds] = await Promise.all([getCaptures(systemId), getCommands(systemId, 20)]);
        if (!alive.current) return;
        const mine = cmds.find((c) => c.id === cmd.id);
        if (mine && (mine.status === "error" || mine.status === "expired")) {
          setSnap({ phase: "error", message: mine.message || `The command ${mine.status}.` });
          setCapturesKey((k) => k + 1);
          return;
        }
        const cap = captureId
          ? caps.find((c) => c.id === captureId)
          : caps.find(
              (c) => c.kind === "spectrum" && (c.command_id === cmd.id || Date.parse(c.created_at) >= requestedAt - 5_000),
            );
        if (!cap) continue;
        if (cap.status === "error") {
          setSnap({ phase: "error", message: cap.error || "The capture failed on the receiver." });
          setCapturesKey((k) => k + 1);
          return;
        }
        if (cap.status === "ready") {
          setSnap({ phase: "loading", message: "Downloading the spectrum…" });
          const data = normalizeSpectrum(await fetchCaptureJSON<unknown>(systemId, cap.id));
          if (!alive.current) return;
          if (!data) {
            setSnap({ phase: "error", message: "The capture file is not a spectrum JSON." });
            return;
          }
          setSnap({ phase: "ready", capture: cap, data });
          setCapturesKey((k) => k + 1);
          return;
        }
        setSnap({
          phase: "waiting",
          message: cap.status === "recording" ? "Recording on the receiver…" : "Queued on the receiver…",
        });
      }
      setSnap({ phase: "error", message: "Timed out waiting for the capture. Check the captures table below." });
    } catch (e: unknown) {
      if (alive.current) setSnap({ phase: "error", message: errorMessage(e) });
    }
  };

  const xs = useMemo(() => (snap.phase === "ready" ? binFrequencies(snap.data) : []), [snap]);
  const markers = channels.map((c) => ({ freq_hz: c.freq_hz, label: c.label, enabled: c.enabled }));

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">Range</span>
        {(Object.keys(RANGE_STEP) as StatsRange[]).map((r) => (
          <Button key={r} type="button" size="sm" variant={range === r ? "default" : "outline"} onClick={() => setRange(r)}>
            {RANGE_LABEL[r]}
          </Button>
        ))}
        <Button type="button" variant="ghost" size="sm" onClick={() => void load(range)} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : undefined} /> Refresh
        </Button>
        {samples && (
          <span className="text-xs text-muted-foreground">
            {samples.length} samples at {RANGE_STEP[range]} steps, refreshed every minute
          </span>
        )}
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}

      <Card>
        <CardHeader>
          <CardTitle>SNR per channel</CardTitle>
          <CardDescription>
            p90 minus p10 block power after the channel filter. Decoding gets unreliable below {SNR_WARN_DB} dB.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {samples === null && !error ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (
            <LineChart
              times={times}
              series={snr}
              yLabel="SNR (dB)"
              yFormat={(v) => `${Math.round(v * 10) / 10} dB`}
              thresholds={[{ value: SNR_WARN_DB, label: `warn below ${SNR_WARN_DB} dB` }]}
              dimmed={loading}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Uncorrectable codewords per channel</CardTitle>
          <CardDescription>
            Share of BCH codewords the decoder could not repair. Gaps are intervals with no codewords at all.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {samples === null && !error ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (
            <LineChart
              times={times}
              series={unc}
              yLabel="Uncorrectable (%)"
              yFormat={(v) => `${Math.round(v * 10) / 10}%`}
              thresholds={[{ value: UNCORRECTABLE_WARN_PCT, label: `warn above ${UNCORRECTABLE_WARN_PCT}%` }]}
              dimmed={loading}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center justify-between gap-3">
            <span>Spectrum snapshot</span>
            <Button type="button" size="sm" onClick={() => void requestSnapshot()} disabled={!online || snapBusy}>
              <Activity /> {snapBusy ? "Capturing…" : "Spectrum snapshot"}
            </Button>
          </CardTitle>
          <CardDescription>
            A 1024-bin averaged power spectrum of one second of raw IQ around the dongle's current centre, with the configured channels marked.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {!online && <p className="text-sm text-muted-foreground">The receiver is offline; snapshots need a live receiver.</p>}
          {snap.phase === "idle" && online && <p className="text-sm text-muted-foreground">No snapshot yet.</p>}
          {(snap.phase === "requesting" || snap.phase === "waiting" || snap.phase === "loading") && (
            <p className="text-sm text-muted-foreground">{snap.message}</p>
          )}
          {snap.phase === "error" && <p className="text-sm text-destructive">{snap.message}</p>}
          {snap.phase === "ready" && (
            <>
              <p className="text-xs text-muted-foreground">
                Captured {fmtDate(snap.capture.ready_at ?? snap.capture.created_at)} · centre {fmtMHz(snap.data.centre_hz)} ·{" "}
                {(snap.data.sample_rate / 1e6).toFixed(3)} MS/s · {snap.data.bins_db.length} bins ·{" "}
                <a
                  className="underline hover:text-foreground"
                  href={downloadUrl(captureDownloadPath(systemId, snap.capture.id))}
                  target="_blank"
                  rel="noreferrer"
                >
                  download JSON
                </a>
              </p>
              <SpectrumChart xs={xs} ys={snap.data.bins_db} markers={markers} />
            </>
          )}
        </CardContent>
      </Card>

      <CapturesPanel systemId={systemId} channels={channels} online={online} refreshKey={capturesKey} />
    </div>
  );
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function binFrequencies(s: SpectrumCapture): number[] {
  const n = s.bins_db.length;
  const start = s.centre_hz - s.sample_rate / 2;
  return s.bins_db.map((_, i) => start + ((i + 0.5) * s.sample_rate) / n);
}

/**
 * One series per channel label. Slots (hence colours) follow the configured
 * channel order so a channel keeps its hue whatever else is on the chart.
 */
function buildSeries(samples: StatsSample[], channels: ChannelConfig[]): { times: number[]; snr: LineSeries[]; unc: LineSeries[] } {
  const order: string[] = [];
  for (const c of channels) if (c.label && !order.includes(c.label)) order.push(c.label);
  for (const s of samples) for (const ch of s.stats.channels) if (ch.label && !order.includes(ch.label)) order.push(ch.label);
  const labels = order.slice(0, MAX_SERIES);
  const times = samples.map((s) => Date.parse(s.ts));

  const hasValues = (series: LineSeries) => series.values.some((v) => v != null);
  const snr = labels
    .map((label, slot): LineSeries => ({
      key: label,
      label,
      slot,
      values: samples.map((s) => {
        const ch = s.stats.channels.find((c) => c.label === label);
        return ch && Number.isFinite(ch.snr_db) ? ch.snr_db : null;
      }),
    }))
    .filter(hasValues);
  const unc = labels
    .map((label, slot): LineSeries => ({
      key: label,
      label,
      slot,
      values: samples.map((s) => {
        const ch = s.stats.channels.find((c) => c.label === label);
        if (!ch) return null;
        const share = uncorrectableShare(ch);
        return share == null ? null : share * 100;
      }),
    }))
    .filter(hasValues);
  return { times, snr, unc };
}
