import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { cn } from "~/lib/utils";
import { uncorrectableShare, type Stats, type StatsSample } from "~/lib/receiver-api";
import { fmtAgo, fmtDuration, fmtFixed, fmtInt, fmtMHz, gainLabel } from "./format";
import { StatTile, TileGroup } from "./stat-tile";
import { ToneBadge, type Tone } from "./status-badge";
import { useStatsStream, type StreamState } from "./use-stats-stream";

interface Props {
  systemId: string;
  online: boolean;
  /** system_connections.last_stats, shown dimmed until the stream delivers a frame. */
  lastKnown?: StatsSample | null;
}

const SNR_WARN_DB = 10;
const UNCORRECTABLE_WARN = 0.2;
const DSP_LOAD_WARN = 0.8;
const CPU_TEMP_WARN_C = 80;

export function LiveHealthCard({ systemId, online, lastKnown }: Props) {
  const { latest, state } = useStatsStream(systemId, online);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5_000);
    return () => clearInterval(t);
  }, []);

  const live = latest;
  const stale = !live && lastKnown ? lastKnown : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-3">
          Live health
          <StreamBadge state={state} online={online} />
          {live && (
            <span className="text-xs font-normal text-muted-foreground">
              Updated {fmtAgo(live.ts, now)} · every {live.stats.interval_s || "?"} s
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {!online && <p className="text-sm text-muted-foreground">Receiver offline. Health resumes when it reconnects.</p>}
        {online && !live && <p className="text-sm text-muted-foreground">Waiting for stats from the receiver…</p>}
        {live && <HealthTiles stats={live.stats} />}
        {stale && (
          <div className="space-y-3 opacity-70">
            <p className="text-xs text-muted-foreground">Last report {fmtAgo(stale.ts, now)}</p>
            <HealthTiles stats={stale.stats} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function StreamBadge({ state, online }: { state: StreamState; online: boolean }) {
  if (!online) return <ToneBadge tone="muted">offline</ToneBadge>;
  const map: Record<StreamState, { tone: Tone; label: string }> = {
    off: { tone: "muted", label: "idle" },
    connecting: { tone: "muted", label: "connecting" },
    live: { tone: "ok", label: "streaming" },
    reconnecting: { tone: "warn", label: "reconnecting" },
  };
  const { tone, label } = map[state];
  return <ToneBadge tone={tone}>{label}</ToneBadge>;
}

function HealthTiles({ stats }: { stats: Stats }) {
  const { dongle, host, link } = stats;
  const throttled = parseThrottled(host.throttled_hex);
  return (
    <div className="space-y-5">
      {stats.channels.length === 0 && <p className="text-sm text-muted-foreground">No channel stats in this report.</p>}
      {stats.channels.map((ch) => {
        const share = uncorrectableShare(ch);
        const neverPaged = ch.last_page_age_s == null || ch.last_page_age_s < 0;
        return (
          <TileGroup
            key={ch.label}
            title={ch.label}
            aside={`${fmtMHz(ch.freq_hz)} · signal ${fmtFixed(ch.power_dbfs)} dBFS · floor ${fmtFixed(ch.noise_floor_dbfs)} dBFS`}
          >
            <StatTile label="SNR" value={fmtFixed(ch.snr_db)} unit="dB" warn={ch.snr_db < SNR_WARN_DB} hint="p90 minus p10 block power" />
            <StatTile label="Carrier" value={fmtFixed(ch.carrier_pct, 0)} unit="%" hint="Share of blocks more than 6 dB above the floor" />
            <StatTile
              label="Uncorrectable"
              value={share == null ? "—" : fmtFixed(share * 100, 1)}
              unit={share == null ? undefined : "%"}
              warn={share != null && share > UNCORRECTABLE_WARN}
              hint={`${fmtInt(ch.cw_clean)} clean, ${fmtInt(ch.cw_corrected)} corrected, ${fmtInt(ch.cw_uncorrectable)} uncorrectable codewords`}
            />
            <StatTile label="Pages" value={fmtInt(ch.pages)} hint={`${fmtInt(ch.sync_locks)} sync locks, ${fmtInt(ch.sync_losses)} losses`} />
            <StatTile label="Truncated" value={fmtInt(ch.pages_truncated)} hint="Pages flushed by a resync or sync loss" />
            <StatTile
              label="Last page"
              value={neverPaged ? "none yet" : fmtDuration(ch.last_page_age_s)}
              unit={neverPaged ? undefined : "ago"}
              className={cn(neverPaged && "text-muted-foreground")}
            />
          </TileGroup>
        );
      })}

      <TileGroup title="Dongle" aside={`${fmtMHz(dongle.centre_hz)} centre · ${(dongle.sample_rate / 1e6).toFixed(3)} MS/s`}>
        <StatTile label="Device" value={dongle.open ? "open" : "closed"} warn={!dongle.open} />
        <StatTile label="Gain" value={dongle.agc ? "AGC" : gainLabel(dongle.gain_tenths_db)} />
        <StatTile label="PPM" value={fmtInt(dongle.ppm)} />
        <StatTile label="Overflows / gaps" value={`${fmtInt(dongle.iq_dropped)} / ${fmtInt(dongle.gaps)}`} hint={`${fmtInt(dongle.iq_buffers)} IQ buffers this interval`} />
        <StatTile
          label="DSP load"
          value={fmtFixed(dongle.dsp_load * 100, 0)}
          unit="%"
          warn={dongle.dsp_load > DSP_LOAD_WARN}
          hint="Processing time over buffer duration; 100% means falling behind"
        />
      </TileGroup>

      <TileGroup title="Host" aside={`up ${fmtDuration(host.uptime_s)}`}>
        <StatTile label="CPU temp" value={fmtFixed(host.cpu_temp_c)} unit="°C" warn={host.cpu_temp_c >= CPU_TEMP_WARN_C} />
        <StatTile
          label="Undervoltage"
          value={host.undervoltage_now ? "now" : host.undervoltage_ever ? "since boot" : "no"}
          warn={host.undervoltage_now || host.undervoltage_ever}
          hint="From the Pi firmware get_throttled flags"
        />
        <StatTile label="Throttled" value={host.throttled_hex || "—"} warn={throttled != null && throttled !== 0} hint="Raw get_throttled value" />
        <StatTile label="Load (1 min)" value={fmtFixed(host.load1, 2)} />
        <StatTile label="Free memory" value={fmtInt(host.mem_free_mb)} unit="MB" />
      </TileGroup>

      <TileGroup title="Link">
        <StatTile label="Reconnects" value={fmtInt(link.reconnects_total)} hint="Total since the receiver started" />
        <StatTile label="Queue" value={fmtInt(link.queue_depth)} hint="Frames waiting to be sent" />
        <StatTile label="RTT" value={fmtInt(link.rtt_ms)} unit="ms" />
        <StatTile label="Dropped frames" value={fmtInt(link.dropped_frames_total)} hint="Total since the receiver started" />
      </TileGroup>
    </div>
  );
}

function parseThrottled(hex: string | undefined): number | null {
  if (!hex) return null;
  const n = Number.parseInt(hex, 16);
  return Number.isNaN(n) ? null : n;
}
