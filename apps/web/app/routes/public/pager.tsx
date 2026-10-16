import { SlidersHorizontal } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigation, useSearchParams } from "react-router";

import { AgencyList } from "~/components/pager/agency-list";
import { LiveStatus, NewPagesPill } from "~/components/pager/live-status";
import { DayDivider, MessageRow, SkeletonRows } from "~/components/pager/message-row";
import { SearchField } from "~/components/pager/search-field";
import { ago, dayKey, dayLabel } from "~/components/pager/time";
import type { Agency, MessagePage } from "~/components/pager/types";
import { usePagerFeed } from "~/components/pager/use-pager-feed";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "~/components/ui/sheet";
import type { Route } from "./+types/pager";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";
const PAGE_SIZE = 50;

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const params = new URLSearchParams({ per_page: String(PAGE_SIZE) });
  const search = url.searchParams.get("search")?.trim();
  const agencyId = url.searchParams.get("agency_id");
  if (search) params.set("search", search);
  if (agencyId) params.set("agency_id", agencyId);

  const [messages, agencies] = await Promise.all([
    fetch(`${API_BASE}/api/public/messages?${params}`)
      .then((r) => (r.ok ? r.json() : null))
      .catch(() => null),
    fetch(`${API_BASE}/api/public/agencies`)
      .then((r) => (r.ok ? r.json() : []))
      .catch(() => []),
  ]);

  return {
    messages: (messages ?? { data: [], has_more: false }) as MessagePage,
    agencies: (Array.isArray(agencies) ? agencies : []) as Agency[],
  };
}

export function meta() {
  return [
    { title: "Live Pager Feed — RFSPager" },
    {
      name: "description",
      content:
        "Live feed of NSW PSN Emergency Services pager messages, decoded from our own SDR receivers and streamed in real time.",
    },
  ];
}

export default function Pager({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigation = useNavigation();
  const committedSearch = searchParams.get("search") ?? "";
  const agencyId = searchParams.get("agency_id") ?? "";
  const agencies = loaderData.agencies;
  const activeAgency = agencies.find((a) => String(a.id) === agencyId) ?? null;
  const filtering = navigation.state === "loading";

  const feed = usePagerFeed({
    initial: loaderData.messages,
    agencies,
    search: committedSearch,
    agencyId,
  });

  const setParam = useCallback(
    (key: string, value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set(key, value);
          else next.delete(key);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // The input is the source of truth while typing; the URL follows 300ms
  // behind. Only pull the URL back into the input when it changed somewhere
  // else (back button, a shared link), never on our own debounced commit.
  const [draft, setDraft] = useState(committedSearch);
  const lastSubmitted = useRef(committedSearch);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (committedSearch !== lastSubmitted.current) {
      lastSubmitted.current = committedSearch;
      setDraft(committedSearch);
    }
  }, [committedSearch]);

  const onSearchChange = (value: string) => {
    setDraft(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      const trimmed = value.trim();
      lastSubmitted.current = trimmed;
      setParam("search", trimmed);
    }, 300);
  };

  const clearSearch = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    setDraft("");
    lastSubmitted.current = "";
    setParam("search", "");
  };

  const [sheetOpen, setSheetOpen] = useState(false);
  const onAgency = (id: string) => {
    setSheetOpen(false);
    setParam("agency_id", id);
  };

  const clearAll = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    setDraft("");
    lastSubmitted.current = "";
    setSearchParams(new URLSearchParams(), { replace: true });
  };

  // "Newest page 4 min ago" is relative to the reader's clock, so it only
  // renders after mount and refreshes on a slow tick.
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);
  const newest = feed.messages[0];
  const newestAgo = now && newest ? ago(newest.created_at, now) : null;

  // Interleave day dividers wherever the (Sydney) date changes. `now` is in
  // the deps so "Today" rolls over for a tab left open past midnight.
  const rows = useMemo(() => {
    const out: React.ReactNode[] = [];
    let lastDay = "";
    for (const msg of feed.messages) {
      const key = dayKey(msg.created_at);
      if (key !== lastDay) {
        out.push(<DayDivider key={`day-${key}`} label={dayLabel(key)} />);
        lastDay = key;
      }
      out.push(<MessageRow key={msg.id} msg={msg} arrivedLive={feed.arrivedLive(msg.id)} />);
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [feed.messages, feed.arrivedLive, now]);

  const hasFilter = Boolean(committedSearch || agencyId);
  const isEmpty = feed.messages.length === 0;

  return (
    <div className="flex-1 bg-white">
      <div className="md:flex md:items-start">
        {/* Rail: title, search, agencies. Sticks under the site header and
            scrolls its own agency list, so the document scroll belongs to
            the feed alone. */}
        <aside className="sticky top-16 hidden h-[calc(100dvh-4rem)] w-72 shrink-0 flex-col border-r border-gray-200 bg-white md:flex lg:w-80">
          <div className="px-6 pb-5 pt-7">
            <h1 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Pager feed</h1>
            <p className="mt-2 text-sm leading-relaxed text-gray-500">
              Every page on the NSW Public Safety Network, decoded off the air by our receivers and shown here
              the moment it arrives.
            </p>
          </div>
          <div className="px-4 pb-3">
            <SearchField value={draft} onChange={onSearchChange} onClear={clearSearch} busy={filtering} placeholder="Search message text" />
          </div>
          <AgencyList
            agencies={agencies}
            activeId={agencyId}
            onSelect={onAgency}
            className="min-h-0 flex-1 space-y-px overflow-y-auto px-3 pb-6 pt-1"
          />
        </aside>

        {/* The feed fills the rest of the width, like /map and /coverage; the document scrolls. */}
        <section className="min-w-0 flex-1 bg-white">
          {/* Phone toolbar: search plus the agency sheet. Sticky under the header. */}
          <div className="sticky top-16 z-20 flex h-14 items-center gap-2 border-b border-gray-200 bg-white/95 px-4 backdrop-blur md:hidden">
            <SearchField
              value={draft}
              onChange={onSearchChange}
              onClear={clearSearch}
              busy={filtering}
              placeholder="Search message text"
              className="flex-1"
            />
            <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
              <SheetTrigger
                className={`inline-flex h-10 max-w-[10rem] shrink-0 items-center gap-1.5 rounded-lg border px-3 text-sm font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
                  activeAgency
                    ? "border-red-100 bg-red-50 text-[#F7241D]"
                    : "border-gray-200 bg-white text-gray-700 hover:bg-gray-50"
                }`}
              >
                <SlidersHorizontal className="size-4 shrink-0" />
                <span className="truncate">{activeAgency ? activeAgency.name : "Agencies"}</span>
              </SheetTrigger>
              <SheetContent side="bottom" className="gap-0 rounded-t-2xl p-0">
                <SheetHeader className="border-b border-gray-100 px-5 py-4">
                  <SheetTitle className={`${heading} text-base font-bold text-gray-900`}>Filter by agency</SheetTitle>
                </SheetHeader>
                <AgencyList
                  agencies={agencies}
                  activeId={agencyId}
                  onSelect={onAgency}
                  className="max-h-[60dvh] space-y-px overflow-y-auto px-3 pb-[max(1.5rem,env(safe-area-inset-bottom))] pt-2"
                />
              </SheetContent>
            </Sheet>
          </div>

          <LiveStatus
            connection={feed.connection}
            paused={feed.paused}
            pendingCount={feed.pendingCount}
            newestAgo={newestAgo}
            onTogglePause={feed.togglePause}
          />

          <div className={`transition-opacity duration-200 ${filtering ? "opacity-50" : ""}`}>
            {isEmpty ? (
              <EmptyState hasFilter={hasFilter} search={committedSearch} agency={activeAgency} onClear={clearAll} />
            ) : (
              <ul>{rows}</ul>
            )}
          </div>

          {/* Always in the DOM so the observer keeps a stable target. */}
          <div ref={feed.sentinelRef} aria-hidden className="h-px" />

          {feed.loadingMore && <SkeletonRows />}

          {!feed.hasMore && !isEmpty && (
            <p className="px-4 py-10 text-center text-sm text-gray-400 sm:px-6">
              That's the oldest page we hold{hasFilter ? " for this filter" : ""}.
            </p>
          )}
        </section>
      </div>

      {!feed.atTop && <NewPagesPill count={feed.pendingCount} onJump={feed.jumpToLatest} />}
    </div>
  );
}

function EmptyState({
  hasFilter,
  search,
  agency,
  onClear,
}: {
  hasFilter: boolean;
  search: string;
  agency: Agency | null;
  onClear: () => void;
}) {
  if (!hasFilter) {
    return (
      <div className="px-6 py-28 text-center">
        <p className={`${heading} text-lg font-bold text-gray-900`}>Waiting for the first page.</p>
        <p className="mt-1 text-sm text-gray-500">The feed fills as messages come off the air.</p>
      </div>
    );
  }
  return (
    <div className="px-6 py-28 text-center">
      <p className={`${heading} text-lg font-bold text-gray-900`}>
        No pages
        {search && (
          <>
            {" "}
            containing <span className="text-gray-500">“{search}”</span>
          </>
        )}
        {agency && <> from {agency.name}</>}.
      </p>
      <p className="mt-1 text-sm text-gray-500">Try a shorter word, or widen the filter.</p>
      <button
        type="button"
        onClick={onClear}
        className="mt-6 inline-flex items-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-semibold text-white outline-none transition-colors hover:bg-gray-800 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
      >
        Show all pages
      </button>
    </div>
  );
}
