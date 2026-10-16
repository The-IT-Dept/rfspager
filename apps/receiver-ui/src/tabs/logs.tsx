import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, Pause, Play, Search } from "lucide-react";
import { Button, ErrorNote, Input, Panel, Pill, Select } from "../components/ui";
import { api, errorMessage } from "../lib/api";
import { useEvent, useStreamEpoch, useStreamState } from "../lib/events";
import { attrValue, cn, fmtDateTime, fmtStamp } from "../lib/format";
import { useDebounced } from "../lib/hooks";
import type { LogEntry } from "../lib/types";

const MAX_ROWS = 2000;
const LEVELS = ["debug", "info", "warn", "error"] as const;
const RANK: Record<string, number> = { debug: 0, info: 1, warn: 2, warning: 2, error: 3, fatal: 3 };
const LEVEL_OPTIONS = [
  { value: "debug", label: "Debug and up" },
  { value: "info", label: "Info and up" },
  { value: "warn", label: "Warn and up" },
  { value: "error", label: "Errors only" },
];

const rank = (l: string) => RANK[l.toLowerCase()] ?? 1;

/** Oldest first, deduplicated by id, capped. */
function merge(prev: LogEntry[], incoming: LogEntry[]): LogEntry[] {
  if (!incoming.length) return prev;
  const have = new Set(prev.map((r) => r.id));
  const fresh = incoming.filter((r) => !have.has(r.id));
  if (!fresh.length) return prev;
  const out = [...prev, ...fresh];
  if (prev.length && fresh[0].id < prev[prev.length - 1].id) out.sort((a, b) => a.id - b.id);
  return out.length > MAX_ROWS ? out.slice(-MAX_ROWS) : out;
}

export function LogsTab() {
  const [level, setLevel] = useState<(typeof LEVELS)[number]>("info");
  const [text, setText] = useState("");
  const filter = useDebounced(text.trim().toLowerCase(), 150);
  const [rows, setRows] = useState<LogEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [paused, setPaused] = useState(false);
  const [held, setHeld] = useState<LogEntry[]>([]);
  const [follow, setFollow] = useState(true);
  const epoch = useStreamEpoch();
  const stream = useStreamState();
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .logs({ limit: 500, level })
      .then((r) => {
        if (cancelled) return;
        // Newest first from the API; the tail reads top to bottom.
        const list = [...(r.logs ?? [])].reverse();
        setRows((prev) => (epoch > 0 && prev.length ? merge(prev.filter((x) => rank(x.level) >= rank(level)), list) : list));
        setError(null);
      })
      .catch((e: unknown) => !cancelled && setError(errorMessage(e)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [level, epoch]);

  useEvent("log", (entry) => {
    if (rank(entry.level) < rank(level)) return;
    if (paused) {
      setHeld((h) => (h.length >= MAX_ROWS ? [...h.slice(1), entry] : [...h, entry]));
    } else {
      setRows((prev) => merge(prev, [entry]));
    }
  });

  const resume = () => {
    setRows((prev) => merge(prev, held));
    setHeld([]);
    setPaused(false);
  };

  const visible = useMemo(() => {
    if (!filter) return rows;
    return rows.filter((r) => {
      if (r.message.toLowerCase().includes(filter)) return true;
      if (!r.attrs) return false;
      return Object.entries(r.attrs).some(
        ([k, v]) => k.toLowerCase().includes(filter) || attrValue(v).toLowerCase().includes(filter),
      );
    });
  }, [rows, filter]);

  // Stick to the bottom while following; reading back up stops it.
  useLayoutEffect(() => {
    if (!follow) return;
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [visible, follow]);

  const onScroll = () => {
    const el = listRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    if (atBottom !== follow) setFollow(atBottom);
  };

  const jump = () => {
    setFollow(true);
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  };

  return (
    <Panel
      title="Logs"
      aside={
        <>
          <span className="tabular-nums">
            {visible.length === rows.length ? `${rows.length} lines` : `${visible.length} of ${rows.length} lines`}
          </span>
          {paused ? (
            <Pill tone="muted">paused</Pill>
          ) : stream === "live" ? (
            <Pill tone="ok" pulse>
              live
            </Pill>
          ) : (
            <Pill tone="muted">{stream === "down" ? "reconnecting" : "connecting"}</Pill>
          )}
        </>
      }
    >
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <Select
          ariaLabel="Minimum level"
          value={level}
          onChange={(v) => setLevel(v as (typeof LEVELS)[number])}
          options={LEVEL_OPTIONS}
        />
        <div className="relative w-full sm:w-72">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-400" />
          <Input
            type="search"
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Filter text"
            aria-label="Filter text"
            className="h-7 pl-8 text-xs"
          />
        </div>
        <Button onClick={paused ? resume : () => setPaused(true)}>
          {paused ? <Play /> : <Pause />}
          {paused ? (held.length ? `Resume (${held.length} new)` : "Resume") : "Pause"}
        </Button>
      </div>

      {error && <ErrorNote className="mb-2">Could not load logs: {error}</ErrorNote>}

      <div className="relative">
        <div
          ref={listRef}
          onScroll={onScroll}
          className="h-[calc(100dvh-15rem)] min-h-80 overflow-y-auto rounded-md border border-border bg-zinc-50/70 py-1 font-mono text-[11.5px] leading-[1.15rem]"
          role="log"
          aria-live="off"
        >
          {visible.length === 0 ? (
            <p className="p-3 text-muted-foreground">
              {loading ? "Loading…" : rows.length === 0 ? "No log lines yet." : "No lines match the filter."}
            </p>
          ) : (
            visible.map((r) => <LogLine key={r.id} r={r} />)
          )}
        </div>
        {!follow && visible.length > 0 && (
          <Button onClick={jump} className="absolute right-4 bottom-3 shadow-md">
            <ArrowDown /> Latest
          </Button>
        )}
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">The receiver keeps its newest 2000 lines in memory; they start empty after a restart.</p>
    </Panel>
  );
}

const LEVEL_TEXT: Record<number, string> = {
  0: "text-zinc-400",
  1: "text-sky-700",
  2: "text-amber-700",
  3: "text-red-700",
};

function LogLine({ r }: { r: LogEntry }) {
  const n = rank(r.level);
  const attrs = r.attrs ? Object.entries(r.attrs) : [];
  return (
    <div
      className={cn(
        "flex gap-2 px-2 py-px whitespace-pre-wrap",
        n === 2 && "bg-amber-50",
        n === 3 && "bg-red-50",
        n === 0 && "text-zinc-500",
      )}
    >
      <span className="shrink-0 text-zinc-400 tabular-nums" title={fmtDateTime(r.ts)}>
        {fmtStamp(r.ts)}
      </span>
      <span className={cn("w-10 shrink-0 font-semibold uppercase", LEVEL_TEXT[n])}>{(r.level || "info").slice(0, 5)}</span>
      <span className="min-w-0 break-words">
        <span className={cn(n === 3 && "text-red-800", n === 2 && "text-amber-900")}>{r.message}</span>
        {attrs.map(([k, v]) => (
          <span key={k} className="text-zinc-500">
            {" "}
            <span className="text-zinc-400">{k}=</span>
            {attrValue(v)}
          </span>
        ))}
      </span>
    </div>
  );
}
