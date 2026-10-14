import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { Loader2, RefreshCw, TriangleAlert, X } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Label } from "~/components/ui/label";
import { Switch } from "~/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { StatTile } from "~/components/receiver/stat-tile";
import { fmtAgo, fmtDate } from "~/components/receiver/format";
import { CoverageMap, type CoverageLayers } from "~/components/coverage/coverage-map";
import { ApiError, api, type PaginatedResult } from "~/lib/api";
import {
  HEAT_RAMP,
  LINK_COLOR,
  RECEIVER_COLOR,
  STALE_DISTRICT_DAYS,
  STATUS_ORDER,
  STATUS_STYLES,
  fmtEirp,
  isStaleDistrict,
  type CoverageChannel,
  type CoverageDistrict,
  type CoverageReceiver,
  type CoverageResponse,
  type CoverageTransmitter,
  type TransmitterDistrictUpdate,
  type TransmitterStatus,
} from "~/lib/coverage";
import { cn } from "~/lib/utils";

export function meta() {
  return [{ title: "Coverage — RFSPager Admin" }];
}

const WINDOWS = [7, 30, 90, 120] as const;
const CHANNELS: CoverageChannel[] = ["RFS", "FRNSW"];
const ALL = "all";

interface AgencyOption {
  id: number;
  agency: string;
  coverage_district?: boolean | null;
}

interface Notice {
  tone: "ok" | "warn" | "error";
  text: string;
}

/** Agencies list is paginated (server caps per_page at 100), so walk all pages. */
async function fetchAllAgencies(): Promise<AgencyOption[]> {
  const out: AgencyOption[] = [];
  for (let page = 1; page <= 20; page++) {
    const res = await api.get<PaginatedResult<AgencyOption>>(`/agencies?per_page=100&page=${page}`);
    out.push(...res.data);
    if (page >= res.total_pages || res.data.length === 0) break;
  }
  return out.sort((a, b) => a.agency.localeCompare(b.agency));
}

function Segmented<T extends string | number>({
  options,
  value,
  onChange,
  label,
  render,
}: {
  options: readonly T[];
  value: T;
  onChange: (v: T) => void;
  label: string;
  render?: (v: T) => string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-lg border bg-background p-0.5">
      {options.map((o) => (
        <button
          key={String(o)}
          type="button"
          role="radio"
          aria-checked={o === value}
          onClick={() => onChange(o)}
          className={cn(
            "h-7 rounded-md px-2.5 text-[0.8rem] font-medium transition-colors",
            o === value ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          {render ? render(o) : String(o)}
        </button>
      ))}
    </div>
  );
}

function StatusSwatch({ status, size = 10 }: { status: TransmitterStatus; size?: number }) {
  const s = STATUS_STYLES[status];
  return (
    <span
      aria-hidden
      className="inline-block shrink-0 rounded-full"
      style={{ width: size, height: size, background: s.fill, border: `1.5px solid ${s.stroke === "#ffffff" ? s.fill : s.stroke}` }}
    />
  );
}

function Legend() {
  const gradient = `linear-gradient(to right, ${HEAT_RAMP.map(([stop, c]) => `${c} ${stop * 100}%`).join(", ")})`;
  return (
    <div className="pointer-events-auto max-w-[220px] rounded-lg border bg-background/95 p-2.5 text-xs shadow-sm backdrop-blur">
      <p className="mb-1.5 font-medium">Transmitters</p>
      <ul className="space-y-1">
        {STATUS_ORDER.map((s) => (
          <li key={s} className="flex items-center gap-2" title={STATUS_STYLES[s].description}>
            <StatusSwatch status={s} />
            <span>{STATUS_STYLES[s].label}</span>
          </li>
        ))}
      </ul>
      <p className="mt-1 text-[11px] text-muted-foreground">Size = EIRP</p>
      <div className="mt-2 space-y-1 border-t pt-2">
        <div className="flex items-center gap-2">
          <span
            aria-hidden
            className="ml-0.5 inline-block size-2.5 rotate-45 rounded-[2px] border border-white shadow-sm"
            style={{ background: RECEIVER_COLOR }}
          />
          <span>Receiver</span>
        </div>
        <div className="flex items-center gap-2">
          <svg width="18" height="6" aria-hidden>
            <line x1="0" y1="3" x2="18" y2="3" stroke={LINK_COLOR} strokeWidth="2" />
          </svg>
          <span>Probable link</span>
        </div>
        <div className="flex items-center gap-2">
          <svg width="18" height="6" aria-hidden>
            <line x1="0" y1="3" x2="18" y2="3" stroke={LINK_COLOR} strokeWidth="2" strokeDasharray="4 3" />
          </svg>
          <span>Possible link</span>
        </div>
      </div>
      <div className="mt-2 border-t pt-2">
        <p className="mb-1">Incidents heard</p>
        <div className="h-2 w-full rounded-full border" style={{ background: gradient }} />
        <div className="mt-0.5 flex justify-between text-[11px] text-muted-foreground">
          <span>fewer</span>
          <span>more</span>
        </div>
      </div>
    </div>
  );
}

function LayerToggles({ layers, onChange }: { layers: CoverageLayers; onChange: (l: CoverageLayers) => void }) {
  const items: Array<{ key: keyof CoverageLayers; label: string }> = [
    { key: "heat", label: "Heat" },
    { key: "transmitters", label: "Transmitters" },
    { key: "receivers", label: "Receivers" },
    { key: "links", label: "Links" },
  ];
  return (
    <div className="pointer-events-auto flex flex-wrap gap-x-3 gap-y-1.5 rounded-lg border bg-background/95 px-2.5 py-2 text-xs shadow-sm backdrop-blur">
      {items.map((i) => (
        <div key={i.key} className="flex items-center gap-1.5">
          <Switch
            id={`layer-${i.key}`}
            size="sm"
            checked={layers[i.key]}
            onCheckedChange={(v) => onChange({ ...layers, [i.key]: v })}
          />
          <Label htmlFor={`layer-${i.key}`} className="text-xs font-normal">
            {i.label}
          </Label>
        </div>
      ))}
    </div>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[96px_1fr] gap-2 py-1 text-sm">
      <span className="text-xs uppercase tracking-wide text-muted-foreground pt-0.5">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

function TransmitterDetail({
  tx,
  receiverName,
  agencies,
  agenciesError,
  onSaved,
  onClose,
}: {
  tx: CoverageTransmitter;
  receiverName: (id: number) => string;
  agencies: AgencyOption[] | null;
  agenciesError: string | null;
  onSaved: () => void;
  onClose: () => void;
}) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    setError(null);
    // Stacked layout: the panel sits below the map, so bring it into view.
    if (typeof window !== "undefined" && window.matchMedia("(max-width: 1023px)").matches) {
      ref.current?.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }, [tx.id]);

  const items = useMemo(() => {
    const list = (agencies ?? []).filter(
      (a) => tx.channel === "FRNSW" || a.coverage_district !== false || a.id === tx.agency_id,
    );
    if (tx.agency_id != null && !list.some((a) => a.id === tx.agency_id)) {
      list.unshift({ id: tx.agency_id, agency: tx.agency ?? `Agency #${tx.agency_id}` });
    }
    return [
      { value: "none", label: "Unmapped (no district)" },
      ...list.map((a) => ({ value: String(a.id), label: a.agency })),
    ];
  }, [agencies, tx.agency_id, tx.agency, tx.channel]);

  const current = tx.agency_id == null ? "none" : String(tx.agency_id);

  const save = async (body: TransmitterDistrictUpdate) => {
    setSaving(true);
    setError(null);
    try {
      await api.put(`/coverage/transmitters/${tx.id}`, body);
      onSaved();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const style = STATUS_STYLES[tx.status];
  const height = [
    tx.height_m != null ? `${Math.round(tx.height_m)} m mast` : null,
    tx.elevation_m != null ? `${Math.round(tx.elevation_m)} m ASL` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <section ref={ref} className="scroll-mt-4 rounded-lg border bg-card p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Transmitter #{tx.id}</p>
          <h3 className="font-semibold leading-snug break-words">{tx.name}</h3>
        </div>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close transmitter detail">
          <X />
        </Button>
      </div>
      <div className="mt-2 divide-y">
        <DetailRow label="Status">
          <span className="inline-flex items-center gap-1.5" title={style.description}>
            <StatusSwatch status={tx.status} />
            {style.label}
          </span>
        </DetailRow>
        <DetailRow label="Licensee">{tx.licensee || "—"}</DetailRow>
        <DetailRow label="Channel">{tx.channel}</DetailRow>
        <DetailRow label="EIRP">{fmtEirp(tx.eirp_w)}{tx.eirp_w == null && <span className="text-muted-foreground"> (50 W assumed)</span>}</DetailRow>
        <DetailRow label="Height">{height || "—"}</DetailRow>
        <DetailRow label="District">
          {tx.agency ?? <span className="text-muted-foreground">unmapped</span>}
          <span className="block text-xs text-muted-foreground">
            {tx.district_source === "manual"
              ? "set manually"
              : `auto${tx.district_confidence != null ? `, ${Math.round(tx.district_confidence * 100)}% confidence` : ""}`}
          </span>
        </DetailRow>
        <DetailRow label="Heard by">
          {tx.heard_by.length ? tx.heard_by.map(receiverName).join(", ") : <span className="text-muted-foreground">—</span>}
        </DetailRow>
        <DetailRow label="Pages">{tx.pages.toLocaleString()}</DetailRow>
      </div>

      <div className="mt-3 space-y-2 border-t pt-3">
        <Label htmlFor="tx-district" className="text-xs">
          District override
        </Label>
        <div className="flex gap-2">
          <Select
            items={items}
            value={current}
            disabled={saving || agencies == null}
            onValueChange={(v) => {
              if (v == null || v === current) return;
              save({ agency_id: v === "none" ? null : Number(v) });
            }}
          >
            <SelectTrigger id="tx-district" className="w-full min-w-0">
              <SelectValue placeholder={agencies == null ? "Loading agencies…" : "Select district"} />
            </SelectTrigger>
            <SelectContent>
              {items.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            disabled={saving || tx.district_source === "auto"}
            onClick={() => save({ auto: true })}
            title={tx.district_source === "auto" ? "Already automatic" : "Recompute the district from stations and incidents"}
          >
            {saving ? <Loader2 className="animate-spin" /> : null}
            Reset to auto
          </Button>
        </div>
        {agenciesError && <p className="text-xs text-destructive">Couldn't load agencies: {agenciesError}</p>}
        {error && <p className="text-xs text-destructive">{error}</p>}
      </div>
    </section>
  );
}

function DistrictsTable({
  districts,
  receiverName,
  selectedAgencyId,
  onSelect,
}: {
  districts: CoverageDistrict[];
  receiverName: (id: number) => string;
  selectedAgencyId: number | null;
  onSelect: (id: number | null) => void;
}) {
  const now = Date.now();
  if (districts.length === 0) {
    return <p className="text-sm text-muted-foreground">No districts for this channel.</p>;
  }
  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead className="h-8 px-2">District</TableHead>
          <TableHead className="h-8 px-2 text-right">Pages</TableHead>
          <TableHead className="h-8 px-2 text-right" title="Capcodes heard / known">Capcodes</TableHead>
          <TableHead className="h-8 px-2 text-right">Last heard</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {districts.map((d) => {
          const stale = isStaleDistrict(d, now);
          const selected = d.agency_id === selectedAgencyId;
          return (
            <TableRow
              key={d.agency_id}
              onClick={() => onSelect(selected ? null : d.agency_id)}
              aria-selected={selected}
              className={cn(
                "cursor-pointer",
                stale && "bg-amber-50 hover:bg-amber-100/70 dark:bg-amber-950/30 dark:hover:bg-amber-950/50",
                selected && "bg-muted ring-2 ring-inset ring-foreground/70",
              )}
            >
              <TableCell className="px-2 py-1.5 whitespace-normal">
                <div className="flex items-center gap-1 font-medium">
                  {stale && (
                    <TriangleAlert
                      className="size-3 shrink-0 text-amber-600 dark:text-amber-400"
                      aria-label={`not heard in ${STALE_DISTRICT_DAYS} days`}
                    />
                  )}
                  <span>{d.agency}</span>
                </div>
                <div className="text-[11px] text-muted-foreground">
                  {d.receivers.length
                    ? d.receivers.map((r) => `${receiverName(r.system_id)} ${r.pages.toLocaleString()}`).join(" · ")
                    : "no receiver"}
                  {` · ${d.transmitters} tx`}
                </div>
              </TableCell>
              <TableCell
                className="px-2 py-1.5 text-right tabular-nums"
                title={
                  d.unknown_capcode_pages
                    ? `${d.unknown_capcode_pages.toLocaleString()} more to unknown capcodes under this district's prefixes (not used for links: may be garbled decodes)`
                    : undefined
                }
              >
                {d.pages.toLocaleString()}
                {d.unknown_capcode_pages > 0 && (
                  <div className="text-[11px] text-muted-foreground">+{d.unknown_capcode_pages.toLocaleString()} unk</div>
                )}
              </TableCell>
              <TableCell className="px-2 py-1.5 text-right tabular-nums">
                {d.capcodes_heard}/{d.capcodes_known}
              </TableCell>
              <TableCell
                className={cn("px-2 py-1.5 text-right whitespace-nowrap", stale && "font-medium text-amber-800 dark:text-amber-300")}
                title={d.last_heard_at ? fmtDate(d.last_heard_at) : "never"}
              >
                {d.last_heard_at ? fmtAgo(d.last_heard_at, now) : "never"}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

export default function CoveragePage() {
  const [days, setDays] = useState<number>(30);
  const [systemId, setSystemId] = useState<string>(ALL);
  const [channel, setChannel] = useState<CoverageChannel>("RFS");
  const [layers, setLayers] = useState<CoverageLayers>({ heat: true, transmitters: true, receivers: true, links: true });

  const [data, setData] = useState<CoverageResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reloadNonce, setReloadNonce] = useState(0);
  // Every receiver we've seen, so the select keeps all options while the
  // response is filtered down to one.
  const [knownReceivers, setKnownReceivers] = useState<CoverageReceiver[]>([]);

  const [selectedAgencyId, setSelectedAgencyId] = useState<number | null>(null);
  const [selectedTxId, setSelectedTxId] = useState<number | null>(null);

  const [agencies, setAgencies] = useState<AgencyOption[] | null>(null);
  const [agenciesError, setAgenciesError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);

  const seq = useRef(0);

  useEffect(() => {
    const mine = ++seq.current;
    setLoading(true);
    setError(null);
    const q = new URLSearchParams({ days: String(days), channel });
    if (systemId !== ALL) q.set("system_id", systemId);
    api
      .get<CoverageResponse>(`/coverage?${q}`)
      .then((res) => {
        if (mine !== seq.current) return;
        setData(res);
        setKnownReceivers((prev) => {
          if (systemId === ALL) return [...res.receivers].sort((a, b) => a.name.localeCompare(b.name));
          const ids = new Set(prev.map((r) => r.id));
          const added = res.receivers.filter((r) => !ids.has(r.id));
          return added.length ? [...prev, ...added].sort((a, b) => a.name.localeCompare(b.name)) : prev;
        });
      })
      .catch((e: unknown) => {
        if (mine !== seq.current) return;
        setError(e instanceof Error ? e.message : "Failed to load coverage");
      })
      .finally(() => {
        if (mine === seq.current) setLoading(false);
      });
  }, [days, systemId, channel, reloadNonce]);

  useEffect(() => {
    fetchAllAgencies()
      .then(setAgencies)
      .catch((e: unknown) => setAgenciesError(e instanceof Error ? e.message : "failed"));
  }, []);

  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(null), 8000);
    return () => clearTimeout(t);
  }, [notice]);

  const reload = useCallback(() => setReloadNonce((n) => n + 1), []);

  const changeChannel = (c: CoverageChannel) => {
    setChannel(c);
    setSelectedAgencyId(null);
    setSelectedTxId(null);
  };

  const receiverNames = useMemo(() => {
    const m = new Map<number, string>();
    for (const r of knownReceivers) m.set(r.id, r.name);
    for (const r of data?.receivers ?? []) m.set(r.id, r.name);
    return m;
  }, [knownReceivers, data]);
  const receiverName = useCallback((id: number) => receiverNames.get(id) ?? `System #${id}`, [receiverNames]);

  const receiverItems = useMemo(
    () => [{ value: ALL, label: "All receivers" }, ...knownReceivers.map((r) => ({ value: String(r.id), label: r.name }))],
    [knownReceivers],
  );

  const summary = useMemo(() => {
    if (!data) return null;
    const now = Date.now();
    let probable = 0;
    let possible = 0;
    for (const t of data.transmitters) {
      if (t.status === "probable") probable++;
      else if (t.status === "possible") possible++;
    }
    const heard = data.districts.filter((d) => d.pages > 0).length;
    const stale = data.districts.filter((d) => isStaleDistrict(d, now)).length;
    const pages = data.total_pages;
    return { probable, possible, heard, stale, pages };
  }, [data]);

  const selectedTx = useMemo(
    () => (selectedTxId == null ? null : data?.transmitters.find((t) => t.id === selectedTxId) ?? null),
    [data, selectedTxId],
  );
  const selectedDistrict = useMemo(
    () => (selectedAgencyId == null ? null : data?.districts.find((d) => d.agency_id === selectedAgencyId) ?? null),
    [data, selectedAgencyId],
  );

  const refreshAcma = async () => {
    setRefreshing(true);
    try {
      await api.post("/coverage/transmitters/refresh");
      setNotice({ tone: "ok", text: "ACMA import started. Transmitters update when it finishes (a few minutes)." });
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setNotice({ tone: "warn", text: "An ACMA import is already running." });
      } else {
        setNotice({ tone: "error", text: e instanceof Error ? e.message : "Couldn't start the ACMA import." });
      }
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div className="flex min-w-0 flex-col gap-4 lg:h-[calc(100svh-7rem)] lg:min-h-[640px]">
      {/* Header + controls */}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Coverage</h1>
          <p className="text-sm text-muted-foreground">
            Receivers, the paging transmitters they hear, and incidents whose pages reached us.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Segmented label="Window" options={WINDOWS} value={days as (typeof WINDOWS)[number]} onChange={setDays} render={(d) => `${d}d`} />
          <Select items={receiverItems} value={systemId} onValueChange={(v) => v != null && setSystemId(v)}>
            <SelectTrigger className="w-44" aria-label="Receiver">
              <SelectValue placeholder="All receivers" />
            </SelectTrigger>
            <SelectContent>
              {receiverItems.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Segmented label="Channel" options={CHANNELS} value={channel} onChange={changeChannel} />
          <Button variant="outline" onClick={refreshAcma} disabled={refreshing} title="Re-import transmitters from the ACMA register">
            {refreshing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            Refresh ACMA data
          </Button>
        </div>
      </div>

      {notice && (
        <div
          role="status"
          className={cn(
            "flex shrink-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm",
            notice.tone === "ok" && "border-emerald-300 bg-emerald-50 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300",
            notice.tone === "warn" && "border-amber-300 bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300",
            notice.tone === "error" && "border-red-300 bg-red-50 text-red-800 dark:bg-red-950/40 dark:text-red-300",
          )}
        >
          <span>{notice.text}</span>
          <Button variant="ghost" size="icon-xs" onClick={() => setNotice(null)} aria-label="Dismiss">
            <X />
          </Button>
        </div>
      )}

      {error && data && (
        <div className="flex shrink-0 items-center justify-between gap-2 rounded-lg border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:bg-red-950/40 dark:text-red-300">
          <span>Couldn't refresh coverage: {error}. Showing the previous result.</span>
          <Button variant="outline" size="sm" onClick={reload}>
            Retry
          </Button>
        </div>
      )}

      {/* Summary tiles */}
      <div className="grid shrink-0 grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-5">
        <StatTile label="Receivers" value={data ? data.receivers.length : "—"} hint="Approved receivers with a location" />
        <StatTile
          label="Transmitters probable / possible"
          value={summary ? `${summary.probable} / ${summary.possible}` : "—"}
          hint="Modelled from distance and EIRP"
        />
        <StatTile
          label="Districts heard"
          value={summary && data ? `${summary.heard} / ${data.districts.length}` : "—"}
          hint="Districts with at least one page in the window"
        />
        <StatTile
          label={`Silent > ${STALE_DISTRICT_DAYS} days`}
          value={summary ? summary.stale : "—"}
          warn={!!summary && summary.stale > 0}
          hint="Districts never heard or not heard recently (all time, any receiver)"
        />
        <StatTile
          label={`Pages, last ${data?.window.days ?? days} days`}
          value={summary ? summary.pages.toLocaleString() : "—"}
          hint="Distinct pages (address + minute) on this channel, including unknown capcodes"
        />
      </div>

      {/* Map + side panel */}
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_380px]">
        <div className="relative h-[60vh] min-h-[420px] overflow-hidden rounded-lg border lg:h-full">
          <CoverageMap
            data={data}
            layers={layers}
            selectedAgencyId={selectedAgencyId}
            selectedTransmitterId={selectedTxId}
            fitToken={`${systemId}|${channel}`}
            onTransmitterClick={setSelectedTxId}
          />
          <div className="pointer-events-none absolute left-2 top-2 right-14 flex flex-col items-start gap-2">
            <LayerToggles layers={layers} onChange={setLayers} />
            {selectedDistrict && (
              <div className="pointer-events-auto flex items-center gap-2 rounded-lg border bg-background/95 px-2.5 py-1.5 text-xs shadow-sm">
                <span>
                  Showing <strong>{selectedDistrict.agency}</strong> ({selectedDistrict.transmitters} transmitters)
                </span>
                <Button variant="ghost" size="icon-xs" onClick={() => setSelectedAgencyId(null)} aria-label="Clear district">
                  <X />
                </Button>
              </div>
            )}
          </div>
          <div className="pointer-events-none absolute bottom-2 left-2 hidden sm:block">
            <Legend />
          </div>
          {loading && (
            <div className="pointer-events-none absolute inset-x-0 top-14 flex justify-center">
              <span className="inline-flex items-center gap-1.5 rounded-full border bg-background/95 px-3 py-1 text-xs shadow-sm">
                <Loader2 className="size-3 animate-spin" /> Loading coverage…
              </span>
            </div>
          )}
          {error && !data && !loading && (
            <div className="absolute inset-0 flex items-center justify-center bg-background/70">
              <div className="space-y-2 rounded-lg border bg-background p-4 text-center text-sm shadow-sm">
                <p className="text-destructive">Couldn't load coverage: {error}</p>
                <Button variant="outline" size="sm" onClick={reload}>
                  Retry
                </Button>
              </div>
            </div>
          )}
        </div>

        <aside className="min-w-0 space-y-4 lg:h-full lg:overflow-y-auto lg:pr-1">
          {selectedTx && (
            <TransmitterDetail
              tx={selectedTx}
              receiverName={receiverName}
              agencies={agencies}
              agenciesError={agenciesError}
              onSaved={reload}
              onClose={() => setSelectedTxId(null)}
            />
          )}

          <section>
            <div className="mb-1 flex items-baseline justify-between gap-2">
              <h2 className="text-sm font-medium">Districts</h2>
              <span className="text-[11px] text-muted-foreground">click to highlight</span>
            </div>
            {data ? (
              <DistrictsTable
                districts={data.districts}
                receiverName={receiverName}
                selectedAgencyId={selectedAgencyId}
                onSelect={setSelectedAgencyId}
              />
            ) : (
              <p className="text-sm text-muted-foreground">{loading ? "Loading…" : "—"}</p>
            )}
          </section>

          {data && data.unmapped_prefixes.length > 0 && (
            <section>
              <h2 className="mb-1 text-sm font-medium">Unmapped capcode prefixes</h2>
              <p className="mb-2 text-[11px] text-muted-foreground">
                Pages to unknown capcodes whose 4-digit prefix doesn't resolve to a district.{" "}
                <Link to="/admin/capcodes" className="underline underline-offset-2">
                  Capcodes
                </Link>
              </p>
              <ul className="grid grid-cols-2 gap-x-4 gap-y-0.5 text-xs">
                {data.unmapped_prefixes.map((p) => (
                  <li key={p.prefix} className="flex justify-between gap-2 border-b border-dashed py-0.5">
                    <span className="font-mono">{p.prefix}xxx</span>
                    <span className="tabular-nums text-muted-foreground">{p.pages.toLocaleString()}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          <p className="text-[11px] leading-relaxed text-muted-foreground">
            Transmitter attribution is modelled from distance and EIRP, not decoded: POCSAG doesn't identify the
            sending site. Receiver pins are suburb-level. Per-receiver detail before{" "}
            {data?.receptions_since ? fmtDate(data.receptions_since) : "the reception log started"} only credits the
            receiver whose copy was stored.
            {data && <> Generated {fmtAgo(data.generated_at)}.</>}
          </p>
        </aside>
      </div>
    </div>
  );
}
