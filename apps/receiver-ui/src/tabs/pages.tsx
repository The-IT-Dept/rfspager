import { useEffect, useRef, useState } from "react";
import { Search, X } from "lucide-react";
import {
  Capcode,
  FilteredTag,
  PageMessage,
  QualityBadge,
  channelTitle,
  matchesQuery,
} from "../components/page-bits";
import { Button, EmptyRow, ErrorNote, Input, Panel, Pill, Switch } from "../components/ui";
import { api, errorMessage } from "../lib/api";
import { useEvent, useStreamEpoch, useStreamState } from "../lib/events";
import { cn, fmtDateTime, fmtInt, fmtStamp } from "../lib/format";
import { useDebounced, useNow } from "../lib/hooks";
import type { Page } from "../lib/types";

const PAGE_SIZE = 100;
/** Live arrivals beyond this many are trimmed from the bottom so the table stays light. */
const MAX_ROWS = 3000;

export function PagesTab() {
  const [query, setQuery] = useState("");
  const q = useDebounced(query.trim(), 300);
  const [filtered, setFiltered] = useState(false);
  const [pages, setPages] = useState<Page[]>([]);
  const [more, setMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fresh, setFresh] = useState<Set<number>>(() => new Set());
  const epoch = useStreamEpoch();
  const stream = useStreamState();
  const now = useNow();
  const reqRef = useRef(0);

  useEffect(() => {
    const req = ++reqRef.current;
    setLoading(true);
    api
      .pages({ limit: PAGE_SIZE, filtered, q })
      .then((r) => {
        if (req !== reqRef.current) return;
        setPages(r.pages ?? []);
        setMore(r.more);
        setFresh(new Set());
        setError(null);
      })
      .catch((e: unknown) => req === reqRef.current && setError(errorMessage(e)))
      .finally(() => req === reqRef.current && setLoading(false));
  }, [filtered, q, epoch]);

  const loadMore = () => {
    const last = pages[pages.length - 1];
    if (!last) return;
    const req = reqRef.current;
    setLoadingMore(true);
    api
      .pages({ limit: PAGE_SIZE, filtered, q, before: last.id })
      .then((r) => {
        if (req !== reqRef.current) return;
        setPages((prev) => {
          const have = new Set(prev.map((p) => p.id));
          return [...prev, ...(r.pages ?? []).filter((p) => !have.has(p.id))];
        });
        setMore(r.more);
        setError(null);
      })
      .catch((e: unknown) => req === reqRef.current && setError(errorMessage(e)))
      .finally(() => setLoadingMore(false));
  };

  useEvent("page", (p) => {
    if (!p.posted && !filtered) return;
    if (!matchesQuery(p, q)) return;
    setPages((prev) => {
      if (prev.some((x) => x.id === p.id)) return prev;
      const next = [p, ...prev];
      return next.length > MAX_ROWS ? next.slice(0, MAX_ROWS) : next;
    });
    setFresh((s) => new Set(s).add(p.id));
  });

  return (
    <Panel
      title="Pages"
      aside={
        <>
          <span className="tabular-nums">
            {fmtInt(pages.length)} shown{more ? "+" : ""}
          </span>
          {stream === "live" ? (
            <Pill tone="ok" pulse>
              live
            </Pill>
          ) : (
            <Pill tone="muted">{stream === "down" ? "reconnecting" : "connecting"}</Pill>
          )}
        </>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="relative w-full sm:w-80">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-400" />
          <Input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search capcode or message"
            aria-label="Search capcode or message"
            className="pr-7 pl-8"
          />
          {query && (
            <button
              type="button"
              onClick={() => setQuery("")}
              className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded p-1 text-zinc-400 hover:text-foreground"
              aria-label="Clear search"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
        <Switch
          checked={filtered}
          onChange={setFiltered}
          label={
            <span>
              Show filtered decodes
              <span className="hidden text-muted-foreground sm:inline"> (noise, numeric beacons)</span>
            </span>
          }
        />
      </div>

      {error && <ErrorNote className="mb-3">Could not load pages: {error}</ErrorNote>}

      <div className={cn("transition-opacity", loading && pages.length > 0 && "opacity-60")}>
        {/* Wide screens: table. */}
        <div className="-mx-3 hidden overflow-x-auto px-3 md:block">
          <table className="w-full border-collapse text-left text-[12.5px]">
            <thead>
              <tr className="border-b border-border text-[10.5px] uppercase tracking-[0.04em] text-muted-foreground [&>th]:px-2 [&>th]:py-1.5 [&>th]:font-semibold [&>th]:whitespace-nowrap">
                <th>Time</th>
                <th>Capcode</th>
                <th>Channel</th>
                <th className="text-right">Baud</th>
                <th className="text-right">Fn</th>
                <th>Quality</th>
                <th className="w-full">Message</th>
              </tr>
            </thead>
            <tbody>
              {!loading && pages.length === 0 && (
                <EmptyRow colSpan={7}>{q ? `No pages match "${q}".` : "No pages yet."}</EmptyRow>
              )}
              {loading && pages.length === 0 && <EmptyRow colSpan={7}>Loading…</EmptyRow>}
              {pages.map((p) => (
                <tr
                  key={p.id}
                  className={cn(
                    "border-b border-hairline align-top [&>td]:px-2 [&>td]:py-1.5",
                    !p.posted && "bg-zinc-50/70 text-zinc-500",
                    fresh.has(p.id) && "animate-row-in",
                  )}
                >
                  <td className="font-mono text-[11.5px] whitespace-nowrap text-muted-foreground tabular-nums" title={fmtDateTime(p.ts)}>
                    {fmtStamp(p.ts, now)}
                  </td>
                  <td className="whitespace-nowrap">
                    <Capcode address={p.address} className={cn(!p.posted && "text-brand/55")} />
                  </td>
                  <td className="whitespace-nowrap" title={channelTitle(p)}>
                    {p.channel}
                  </td>
                  <td className="text-right font-mono tabular-nums">{p.baud}</td>
                  <td className="text-right font-mono tabular-nums">{p.function}</td>
                  <td className="whitespace-nowrap">
                    <QualityBadge quality={p.quality} />
                  </td>
                  <td className="min-w-64">
                    {!p.posted && (
                      <span className="mr-1.5 align-[1px]">
                        <FilteredTag />
                      </span>
                    )}
                    <PageMessage page={p} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* Phones: stacked rows. */}
        <ul className="-mx-3 md:hidden">
          {!loading && pages.length === 0 && (
            <li className="px-3 py-8 text-center text-muted-foreground">{q ? `No pages match "${q}".` : "No pages yet."}</li>
          )}
          {loading && pages.length === 0 && <li className="px-3 py-8 text-center text-muted-foreground">Loading…</li>}
          {pages.map((p) => (
            <li
              key={p.id}
              className={cn(
                "border-b border-hairline px-3 py-2",
                !p.posted && "bg-zinc-50 text-zinc-500",
                fresh.has(p.id) && "animate-row-in",
              )}
            >
              <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
                <span className="font-mono tabular-nums" title={fmtDateTime(p.ts)}>
                  {fmtStamp(p.ts, now)}
                </span>
                <Capcode address={p.address} className={cn("text-[12px]", !p.posted && "text-brand/55")} />
                <span title={channelTitle(p)}>
                  {p.channel} · {p.baud} · fn {p.function}
                </span>
                <span className="ml-auto">
                  <QualityBadge quality={p.quality} />
                </span>
              </div>
              <p className="mt-1">
                {!p.posted && (
                  <span className="mr-1.5 align-[1px]">
                    <FilteredTag />
                  </span>
                )}
                <PageMessage page={p} />
              </p>
            </li>
          ))}
        </ul>
      </div>

      {more && (
        <div className="mt-3 flex justify-center">
          <Button onClick={loadMore} disabled={loadingMore || loading}>
            {loadingMore ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
      {!more && pages.length > 0 && !loading && (
        <p className="mt-3 text-center text-[11px] text-muted-foreground">
          End of history. The receiver keeps its newest few thousand decodes.
        </p>
      )}
    </Panel>
  );
}
