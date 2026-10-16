import type { ReactNode } from "react";
import { Info } from "lucide-react";
import { EmptyRow, ErrorNote, Panel, Pill, Table, Tag } from "../components/ui";
import { api } from "../lib/api";
import { fmtMHz, fmtMSps, fmtVersion, gainLabel } from "../lib/format";
import { useLoad } from "../lib/hooks";
import type { RuntimeConfig, Settings } from "../lib/types";

export function SettingsTab() {
  const { data, error, loading } = useLoad(() => api.settings(), []);

  return (
    <div className="space-y-3">
      <div className="flex items-start gap-2.5 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2.5 text-[13px] text-sky-950">
        <Info className="mt-0.5 size-4 shrink-0 text-sky-700" />
        <p>
          These settings are managed by RFSPager. Contact us if something needs changing.
          <span className="block text-xs text-sky-900/70">
            Tuning is set centrally so every receiver in the network stays comparable. Webhooks, on the Connections
            tab, are yours to change.
          </span>
        </p>
      </div>

      {error && <ErrorNote>Could not load settings: {error}</ErrorNote>}
      {loading && !data && <p className="py-8 text-center text-muted-foreground">Loading…</p>}
      {data && <SettingsBody s={data} />}
    </div>
  );
}

function SettingsBody({ s }: { s: Settings }) {
  const c = s.config;
  const channels = c.channels ?? [];
  const dongles = s.dongles ?? [];
  const endpoints = s.endpoints ?? [];
  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <Panel title="Radio">
        <Rows>
          <Row k="Gain" v={gainLabel(c.tuner_gain_tenths_db)} />
          <Row k="Frequency correction" v={`${c.ppm} ppm`} />
          <Row k="Sample rate" v={fmtMSps(c.sample_rate)} mono />
          <Row k="Dongle" v={dongleSelection(c)} />
        </Rows>
      </Panel>

      <Panel title="Channels">
        <Table>
          <thead>
            <tr>
              <th>Label</th>
              <th className="text-right">MHz</th>
              <th>Enabled</th>
            </tr>
          </thead>
          <tbody>
            {channels.length === 0 && <EmptyRow colSpan={3}>No channels configured.</EmptyRow>}
            {channels.map((ch) => (
              <tr key={`${ch.label}-${ch.freq_hz}`}>
                <td className="font-semibold">{ch.label}</td>
                <td className="text-right font-mono tabular-nums">{fmtMHz(ch.freq_hz)}</td>
                <td>{ch.enabled ? <Pill tone="ok">On</Pill> : <Pill tone="muted">Off</Pill>}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Panel>

      <Panel title="Config">
        <Rows>
          <Row k="Revision" v={s.config_version ? `#${s.config_version}` : "0 (local defaults)"} />
          <Row k="Source" v={s.config_source || "—"} mono small />
          <Row k="Log level" v={c.log_level || "info"} />
          <Row k="Stats interval" v={`${c.stats_interval_s} s`} />
          <Row
            k="Forward all decodes"
            v={
              c.post_all ? (
                <span>
                  Yes <span className="text-muted-foreground">(content filter bypassed)</span>
                </span>
              ) : (
                <span>
                  No <span className="text-muted-foreground">(noise and numeric beacons filtered)</span>
                </span>
              )
            }
          />
        </Rows>
      </Panel>

      <Panel title="Build and host">
        <Rows>
          <Row k="Version" v={fmtVersion(s.build.version)} mono />
          <Row k="Platform" v={`${s.build.os}/${s.build.arch}`} mono />
          <Row k="Hostname" v={s.build.hostname} mono />
          <Row k="Model" v={s.host?.model || "—"} />
          <Row k="Kernel" v={s.host?.kernel || "—"} mono small />
          <Row k="librtlsdr" v={s.host?.rtlsdr || "—"} mono small />
          <Row
            k="Dongles"
            v={
              dongles.length === 0 ? (
                <span className="text-muted-foreground">none detected</span>
              ) : (
                <ul className="space-y-1">
                  {dongles.map((d) => (
                    <li key={`${d.index}-${d.serial ?? ""}`}>
                      <span className="font-mono text-muted-foreground">#{d.index}</span> {d.name}
                      {d.tuner && <span className="text-muted-foreground"> · {d.tuner}</span>}
                      {d.serial && <span className="font-mono text-[11.5px] text-muted-foreground"> · SN {d.serial}</span>}
                    </li>
                  ))}
                </ul>
              )
            }
          />
        </Rows>
      </Panel>

      <Panel title="Endpoints">
        <Table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Mode</th>
              <th>URL</th>
            </tr>
          </thead>
          <tbody>
            {endpoints.length === 0 && <EmptyRow colSpan={3}>No endpoints configured.</EmptyRow>}
            {endpoints.map((e) => (
              <tr key={e.name}>
                <td className="whitespace-nowrap">
                  <span className="font-semibold">{e.name}</span> {e.primary && <Tag>primary</Tag>}
                </td>
                <td className="font-mono">{e.mode}</td>
                <td className="font-mono text-[11.5px] break-all">{e.url}</td>
              </tr>
            ))}
          </tbody>
        </Table>
        <p className="mt-2 text-[11px] text-muted-foreground">API keys are never shown here.</p>
      </Panel>

      <Panel title="Local UI">
        <Rows>
          <Row k="Listening on" v={s.ui_listen || "—"} mono />
          <Row k="State directory" v={s.state_dir || "—"} mono small />
        </Rows>
        <p className="mt-2 text-[11px] text-muted-foreground">
          No login: anyone who can reach this port can see everything here and manage webhooks. Keep it on a trusted
          network.
        </p>
      </Panel>
    </div>
  );
}

function dongleSelection(c: RuntimeConfig): string {
  if (c.dongle_serial) return `serial ${c.dongle_serial}`;
  if (c.dongle_index < 0) return "first available";
  return `index ${c.dongle_index}`;
}

function Rows({ children }: { children: ReactNode }) {
  return <dl className="divide-y divide-hairline">{children}</dl>;
}

function Row({ k, v, mono, small }: { k: string; v: ReactNode; mono?: boolean; small?: boolean }) {
  return (
    <div className="grid grid-cols-[minmax(7rem,38%)_1fr] gap-3 py-1.5">
      <dt className="text-muted-foreground">{k}</dt>
      <dd className={[mono ? "font-mono" : "", small ? "text-[11.5px] break-all" : "", "min-w-0"].join(" ")}>{v}</dd>
    </div>
  );
}
