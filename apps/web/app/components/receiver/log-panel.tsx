import { useCallback, useEffect, useRef, useState } from "react";
import { Pause, Play, Trash2 } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Label } from "~/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
import { Switch } from "~/components/ui/switch";
import { errorMessage, getRecentLogs, openSSE, parseLogRow, type LogRow } from "~/lib/receiver-api";
import { compactObject, fmtTime } from "./format";
import { LevelTag, ToneBadge, type Tone } from "./status-badge";
import type { StreamState } from "./use-stats-stream";

const MAX_ROWS = 500;

const LEVEL_OPTIONS = [
  { value: "all", label: "All levels" },
  { value: "debug", label: "Debug" },
  { value: "info", label: "Info" },
  { value: "warn", label: "Warn" },
  { value: "error", label: "Error" },
];

function cmpId(a: string, b: string): number {
  const na = Number(a);
  const nb = Number(b);
  if (Number.isFinite(na) && Number.isFinite(nb)) return na - nb;
  return a.localeCompare(b);
}

/** Appends new rows, drops duplicates by id, keeps time order and the row cap. */
function mergeRows(prev: LogRow[], incoming: LogRow[]): LogRow[] {
  const have = new Set(prev.map((r) => r.id));
  const fresh = incoming.filter((r) => !have.has(r.id));
  if (!fresh.length) return prev;
  const merged = [...prev, ...fresh].sort(
    (a, b) => (Date.parse(a.ts) || 0) - (Date.parse(b.ts) || 0) || cmpId(a.id, b.id),
  );
  return merged.length > MAX_ROWS ? merged.slice(-MAX_ROWS) : merged;
}

/**
 * Live receiver log: the recent endpoint fills the panel, the SSE stream
 * keeps it moving. Pausing holds arrivals aside (capped) and releases them
 * on resume; auto-scroll switches itself off when the reader scrolls up.
 */
export function LogPanel({ systemId }: { systemId: string }) {
  const [rows, setRows] = useState<LogRow[]>([]);
  const [level, setLevel] = useState("all");
  const [paused, setPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [state, setState] = useState<StreamState>("connecting");
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pendingCount, setPendingCount] = useState(0);

  const pausedRef = useRef(false);
  pausedRef.current = paused;
  const pendingRef = useRef<LogRow[]>([]);
  const listRef = useRef<HTMLDivElement>(null);

  const append = useCallback((incoming: LogRow[]) => {
    setRows((prev) => mergeRows(prev, incoming));
  }, []);

  useEffect(() => {
    let cancelled = false;
    getRecentLogs(systemId, 200)
      .then((r) => {
        if (!cancelled) append(r);
      })
      .catch((e: unknown) => {
        if (!cancelled) setLoadError(errorMessage(e));
      });
    return () => {
      cancelled = true;
    };
  }, [systemId, append]);

  useEffect(() => {
    const es = openSSE(`/systems/${systemId}/logs/stream`);
    es.onopen = () => setState("live");
    es.onerror = () => setState("reconnecting");
    es.onmessage = (e: MessageEvent<string>) => {
      const row = parseLogRow(e.data);
      if (!row) return;
      if (pausedRef.current) {
        const held = pendingRef.current;
        held.push(row);
        if (held.length > MAX_ROWS) held.splice(0, held.length - MAX_ROWS);
        setPendingCount(held.length);
      } else {
        append([row]);
      }
    };
    return () => es.close();
  }, [systemId, append]);

  const togglePause = () => {
    if (paused) {
      const held = pendingRef.current;
      pendingRef.current = [];
      setPendingCount(0);
      append(held);
    }
    setPaused(!paused);
  };

  useEffect(() => {
    if (!autoScroll) return;
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [rows, autoScroll, level]);

  const visible = level === "all" ? rows : rows.filter((r) => r.level === level);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-3">
          Live log
          <StreamBadge state={state} paused={paused} />
          <span className="text-xs font-normal text-muted-foreground">
            {visible.length === rows.length ? `${rows.length} lines` : `${visible.length} of ${rows.length} lines`}
            {rows.length >= MAX_ROWS && " (oldest trimmed)"}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <Select items={LEVEL_OPTIONS} value={level} onValueChange={(v) => v != null && setLevel(v)}>
            <SelectTrigger size="sm" className="w-36" aria-label="Level filter">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {LEVEL_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" size="sm" onClick={togglePause}>
            {paused ? <Play /> : <Pause />}
            {paused ? (pendingCount ? `Resume (${pendingCount} new)` : "Resume") : "Pause"}
          </Button>
          <div className="flex items-center gap-2">
            <Switch id="log-autoscroll" size="sm" checked={autoScroll} onCheckedChange={(v) => setAutoScroll(v)} />
            <Label htmlFor="log-autoscroll" className="text-xs">
              Auto-scroll
            </Label>
          </div>
          <Button type="button" variant="ghost" size="sm" onClick={() => setRows([])}>
            <Trash2 /> Clear
          </Button>
        </div>

        {loadError && <p className="text-sm text-destructive">Could not load recent lines: {loadError}</p>}

        <div
          ref={listRef}
          onWheel={(e) => {
            if (e.deltaY < 0 && autoScroll) setAutoScroll(false);
          }}
          className="h-[28rem] overflow-y-auto rounded-md border bg-muted/30 font-mono text-xs"
        >
          {visible.length === 0 ? (
            <p className="p-3 text-muted-foreground">
              {rows.length === 0 ? "No log lines yet." : "No lines at this level."}
            </p>
          ) : (
            visible.map((r) => <LogLine key={r.id} row={r} />)
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function StreamBadge({ state, paused }: { state: StreamState; paused: boolean }) {
  if (paused) return <ToneBadge tone="muted">paused</ToneBadge>;
  const map: Record<StreamState, { tone: Tone; label: string }> = {
    off: { tone: "muted", label: "idle" },
    connecting: { tone: "muted", label: "connecting" },
    live: { tone: "ok", label: "live" },
    reconnecting: { tone: "warn", label: "reconnecting" },
  };
  const { tone, label } = map[state];
  return <ToneBadge tone={tone}>{label}</ToneBadge>;
}

interface PageFields {
  channel: string;
  address: string;
  message: string;
}

/** Page rows ship their decoded fields in `raw`; everything else is shown as key=value. */
function pageFields(raw: Record<string, unknown> | null): PageFields | null {
  if (!raw) return null;
  const address = raw.address ?? raw.capcode;
  if (address == null) return null;
  return {
    channel: String(raw.channel ?? raw.label ?? "?"),
    address: String(address),
    message:
      typeof raw.msg === "string" ? raw.msg : typeof raw.message === "string" ? raw.message : "",
  };
}

function LogLine({ row }: { row: LogRow }) {
  const page = pageFields(row.raw);
  const extra = !page && row.raw && Object.keys(row.raw).length > 0 ? compactObject(row.raw) : "";
  const showPageMessage = page && page.message && page.message !== row.message;
  return (
    <div className="flex gap-2 border-b border-border/40 px-2 py-0.5 leading-5 last:border-0">
      <span className="shrink-0 text-muted-foreground tabular-nums">{fmtTime(row.ts)}</span>
      <LevelTag level={row.level} />
      <span className="min-w-0 whitespace-pre-wrap break-words">
        {row.source && <span className="text-muted-foreground">{row.source} </span>}
        {row.message}
        {page && (
          <span className="ml-2 text-muted-foreground">
            [{page.channel}] {page.address}
            {showPageMessage ? `: ${page.message}` : ""}
          </span>
        )}
        {extra && <span className="ml-2 text-muted-foreground/80">{extra}</span>}
      </span>
    </div>
  );
}
