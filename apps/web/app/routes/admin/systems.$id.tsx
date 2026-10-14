import { useCallback, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "~/components/ui/tabs";
import { Pencil, ArrowLeft, Copy, Check } from "lucide-react";
import { api } from "~/lib/api";
import {
  getSystemConfig,
  normalizeStatsSample,
  parseDongles,
  parseHostInfo,
  type ChannelConfig,
  type RuntimeConfig,
} from "~/lib/receiver-api";
import { ConfigTab } from "~/components/receiver/config-tab";
import { DiagnosticsTab } from "~/components/receiver/diagnostics-tab";
import { EventsTable } from "~/components/receiver/events-table";
import { fmtDuration } from "~/components/receiver/format";
import { LiveHealthCard } from "~/components/receiver/live-health";
import { LogPanel } from "~/components/receiver/log-panel";
import { ToneBadge, configStatusTone } from "~/components/receiver/status-badge";
import { UpdateTab } from "~/components/receiver/update-tab";

interface SystemDetail {
  id: number;
  system_id: string;
  name: string;
  user_id: number;
  user_name?: string | null;
  suburb?: string | null;
  request_notes?: string | null;
  api_key?: string | null;
  install_command?: string | null;
  approved_at?: string | null;
  deleted_at?: string | null;
  created_at?: string;
  updated_at?: string;
  sync?: boolean;
  frequency?: number;
  lat?: number | string | null;
  lng?: number | string | null;
  located_since?: string | null;
  last_message_at?: string | null;

  // Receiver-agent connection fields (joined from system_connections).
  receiver_online?: boolean;
  receiver_pod_id?: string | null;
  receiver_connected_at?: string | null;
  receiver_last_ping_at?: string | null;
  receiver_version?: string | null;
  receiver_os?: string | null;
  receiver_arch?: string | null;
  receiver_hostname?: string | null;
  receiver_dongles?: string | null; // JSON string (cast from jsonb)

  // Remote-management fields; jsonb columns may arrive as objects or strings.
  receiver_proto?: number | null;
  receiver_config_version?: number | null;
  receiver_config_status?: string | null;
  receiver_host?: unknown;
  receiver_last_stats?: unknown;
  receiver_last_stats_at?: string | null;
}

const TABS = ["overview", "config", "logs", "diagnostics", "update"] as const;
type TabId = (typeof TABS)[number];

function isTab(v: unknown): v is TabId {
  return typeof v === "string" && (TABS as readonly string[]).includes(v);
}

function fmtDate(s?: string | null): string {
  if (!s) return "—";
  return new Date(s).toLocaleString();
}

function fmtLatLng(lat?: number | string | null, lng?: number | string | null): string {
  if (lat == null || lng == null || lat === "" || lng === "") return "—";
  const la = Number(lat);
  const ln = Number(lng);
  if (!Number.isFinite(la) || !Number.isFinite(ln)) return "—";
  return `${la.toFixed(2)}, ${ln.toFixed(2)}`;
}

function Field({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className={`mt-1 text-sm break-all ${mono ? "font-mono" : ""}`}>{children}</p>
    </div>
  );
}

function CopyValue({ value }: { value?: string | null }) {
  const [copied, setCopied] = useState(false);
  if (!value) return <span className="text-muted-foreground">—</span>;
  return (
    <span className="inline-flex items-center gap-2">
      <span className="font-mono text-xs break-all">{value}</span>
      <button
        type="button"
        onClick={() => {
          navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        }}
        className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
        title="Copy"
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </span>
  );
}

function StatusBadge({ s }: { s: SystemDetail }) {
  if (s.deleted_at) return <Badge variant="destructive">deleted</Badge>;
  if (!s.approved_at) {
    return (
      <Badge variant="outline" className="border-amber-500 text-amber-700 bg-amber-50">
        pending
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50">
      approved
    </Badge>
  );
}

function OnlineBadge({ online }: { online?: boolean }) {
  if (online) {
    return (
      <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50">
        online
      </Badge>
    );
  }
  return <Badge variant="outline" className="text-muted-foreground">offline</Badge>;
}

export default function SystemDetailPage() {
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const [system, setSystem] = useState<SystemDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [channels, setChannels] = useState<ChannelConfig[]>([]);

  const tabParam = searchParams.get("tab");
  const tab: TabId = isTab(tabParam) ? tabParam : "overview";
  const setTab = (next: TabId) =>
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        if (next === "overview") p.delete("tab");
        else p.set("tab", next);
        return p;
      },
      { replace: true },
    );

  const loadSystem = useCallback(() => {
    if (!id) return;
    api
      .get<SystemDetail>(`/systems/${id}`)
      .then(setSystem)
      .catch((e: Error) => setError(e.message));
  }, [id]);

  useEffect(() => {
    loadSystem();
  }, [loadSystem]);

  // Channel list for the Diagnostics tab (capture channel select, spectrum
  // markers, series order). The Config tab owns the full editable copy and
  // reports saves back here.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    getSystemConfig(id)
      .then((r) => {
        if (!cancelled) setChannels((r.current?.config ?? r.defaults).channels);
      })
      .catch(() => {
        // The Config tab surfaces its own load errors.
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const onConfigSaved = useCallback((cfg: RuntimeConfig) => setChannels(cfg.channels), []);

  if (error) return <p className="text-destructive">{error}</p>;
  if (!system || !id) return <p className="text-muted-foreground">Loading...</p>;

  const dongles = parseDongles(system.receiver_dongles);
  const host = parseHostInfo(system.receiver_host);
  const lastStats = system.receiver_last_stats
    ? normalizeStatsSample({ ts: system.receiver_last_stats_at ?? "", stats: system.receiver_last_stats })
    : null;
  const proto = typeof system.receiver_proto === "number" ? system.receiver_proto : null;
  const online = Boolean(system.receiver_online);
  const freqDisplay = system.frequency && system.frequency > 1000
    ? `${(system.frequency / 1_000_000).toFixed(4)} MHz`
    : system.frequency ?? "—";

  return (
    <div className="space-y-6 min-w-0">
      <div className="flex items-center gap-3">
        <Link to="/admin/systems">
          <Button variant="ghost" size="sm"><ArrowLeft className="size-4" /> Back to Systems</Button>
        </Link>
      </div>

      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-2">
            {system.name}
            <StatusBadge s={system} />
            <OnlineBadge online={system.receiver_online} />
          </h1>
          <p className="text-sm text-muted-foreground">{system.suburb || "—"}</p>
        </div>
        <Link to={`/admin/systems/${system.id}/edit`}>
          <Button variant="outline"><Pencil className="size-4" /> Edit</Button>
        </Link>
      </div>

      <Tabs
        value={tab}
        onValueChange={(v) => {
          if (isTab(v)) setTab(v);
        }}
        className="gap-4"
      >
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="config">Config</TabsTrigger>
          <TabsTrigger value="logs">Logs &amp; Events</TabsTrigger>
          <TabsTrigger value="diagnostics">Diagnostics</TabsTrigger>
          <TabsTrigger value="update">Update</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-3">
                Receiver
                <OnlineBadge online={system.receiver_online} />
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              {system.receiver_last_ping_at ? (
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                  <Field label="Hostname">{system.receiver_hostname ?? "—"}</Field>
                  <Field label="Version" mono>{system.receiver_version ?? "—"}</Field>
                  <Field label="OS / arch">
                    {(system.receiver_os ?? "—") + " / " + (system.receiver_arch ?? "—")}
                  </Field>
                  <Field label="Pod"><CopyValue value={system.receiver_pod_id} /></Field>
                  <Field label="Connected">{fmtDate(system.receiver_connected_at)}</Field>
                  <Field label="Last Ping">{fmtDate(system.receiver_last_ping_at)}</Field>
                  <Field label="Protocol">
                    {proto == null ? "—" : proto >= 2 ? `v${proto} (managed)` : `v${proto} (legacy, ignores commands)`}
                  </Field>
                  <Field label="Config">
                    {system.receiver_config_version == null ? (
                      "—"
                    ) : (
                      <span className="inline-flex items-center gap-2">
                        revision {system.receiver_config_version}
                        {system.receiver_config_status && (
                          <ToneBadge tone={configStatusTone(system.receiver_config_status)}>{system.receiver_config_status}</ToneBadge>
                        )}
                      </span>
                    )}
                  </Field>
                  <Field label="Last Stats">{fmtDate(system.receiver_last_stats_at)}</Field>
                  {host && (
                    <>
                      <Field label="Host">{host.model ?? "—"}</Field>
                      <Field label="Kernel" mono>{host.kernel ?? "—"}</Field>
                      <Field label="librtlsdr">{host.rtlsdr ?? "—"}</Field>
                      {host.uptime_s != null && <Field label="Host uptime">{fmtDuration(host.uptime_s)} (at connect)</Field>}
                    </>
                  )}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">No receiver has connected yet.</p>
              )}

              {dongles.length > 0 && (
                <div>
                  <p className="text-xs uppercase tracking-wide text-muted-foreground mb-2">Dongles</p>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-12">#</TableHead>
                        <TableHead>Name</TableHead>
                        <TableHead>Manufacturer</TableHead>
                        <TableHead>Product</TableHead>
                        <TableHead>Serial</TableHead>
                        <TableHead>Tuner</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {dongles.map((d) => (
                        <TableRow key={d.index}>
                          <TableCell>{d.index}</TableCell>
                          <TableCell>{d.name ?? "—"}</TableCell>
                          <TableCell>{d.manufacturer ?? "—"}</TableCell>
                          <TableCell>{d.product ?? "—"}</TableCell>
                          <TableCell className="font-mono text-xs">{d.serial ?? "—"}</TableCell>
                          <TableCell>{d.tuner ?? "—"}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>

          <LiveHealthCard systemId={id} online={online} lastKnown={lastStats} />

          <Card>
            <CardHeader><CardTitle>Identity</CardTitle></CardHeader>
            <CardContent className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <Field label="Internal ID"><CopyValue value={String(system.id)} /></Field>
              <Field label="UUID"><CopyValue value={system.system_id} /></Field>
              <Field label="Owner">
                {system.user_name ? (
                  <Link to={`/admin/users/${system.user_id}`} className="hover:underline">
                    {system.user_name}
                  </Link>
                ) : "—"}
              </Field>
              <Field label="Frequency">{freqDisplay}</Field>
              <Field label="Suburb">{system.suburb || "—"}</Field>
              <Field label="Location">{fmtLatLng(system.lat, system.lng)}</Field>
              <Field label="Located Since">{system.located_since ? fmtDate(system.located_since) : "always"}</Field>
              <Field label="Ingress API Key"><CopyValue value={system.api_key} /></Field>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Install on a Pi</CardTitle></CardHeader>
            <CardContent>
              {system.install_command ? (
                <>
                  <CopyValue value={system.install_command} />
                  <p className="mt-3 text-sm text-muted-foreground">
                    Run this on the receiver over SSH. It installs the rtl-sdr dependencies, the binary and the
                    systemd unit, then waits until the receiver has reached this server. Re-running it is how you
                    upgrade a legacy receiver; managed receivers update from the Update tab.
                  </p>
                </>
              ) : (
                <p className="text-sm text-muted-foreground">
                  No API key yet — approve the system to generate one.
                </p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Lifecycle</CardTitle></CardHeader>
            <CardContent className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
              <Field label="Submitted">{fmtDate(system.created_at)}</Field>
              <Field label="Approved">{fmtDate(system.approved_at)}</Field>
              <Field label="Updated">{fmtDate(system.updated_at)}</Field>
              <Field label="Last Message">{fmtDate(system.last_message_at)}</Field>
              {system.deleted_at && (
                <Field label="Deleted">
                  <Badge variant="destructive">{fmtDate(system.deleted_at)}</Badge>
                </Field>
              )}
            </CardContent>
          </Card>

          {system.request_notes && (
            <Card>
              <CardHeader><CardTitle>Request Notes</CardTitle></CardHeader>
              <CardContent>
                <p className="text-sm whitespace-pre-wrap">{system.request_notes}</p>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="config">
          <ConfigTab systemId={id} dongles={dongles} onSaved={onConfigSaved} />
        </TabsContent>

        <TabsContent value="logs" className="space-y-6">
          <LogPanel systemId={id} />
          <EventsTable systemId={id} />
        </TabsContent>

        <TabsContent value="diagnostics">
          <DiagnosticsTab systemId={id} channels={channels} online={online} />
        </TabsContent>

        <TabsContent value="update">
          <UpdateTab
            systemId={id}
            currentVersion={system.receiver_version ?? null}
            online={online}
            proto={proto}
            onSystemChanged={loadSystem}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}
