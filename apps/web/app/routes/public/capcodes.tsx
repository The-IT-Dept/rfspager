import { SlidersHorizontal } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { AgencyList, FilterRow } from "~/components/pager/agency-list";
import { SearchField } from "~/components/pager/search-field";
import { ago } from "~/components/pager/time";
import type { Agency } from "~/components/pager/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "~/components/ui/sheet";
import type { Route } from "./+types/capcodes";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

// The backend caps a page at 2500 and NSW has ~1,630 capcodes, so one trip
// fetches the lot and filtering is instant on the client. `has_more` is
// honoured below in case the catalogue ever outgrows that.
const CATALOGUE_PAGE = 2500;
const UNKNOWN_DAYS = 30;
const UNKNOWN_LIMIT = 300;
const CHUNK = 120;

interface Capcode {
  id: number;
  address: string;
  alias: string | null;
  icon: string | null;
  colour: string | null;
  agency_id: number | null;
  agency_name: string | null;
}

interface UnknownRow {
  capcode_id: number | null;
  address: string;
  alias: string | null;
  agency_name: string | null;
  message_count: number;
  last_seen_at: string;
}

export async function loader() {
  const [cp, ag, unk] = await Promise.all([
    fetch(`${API_BASE}/api/public/capcodes?page=1&per_page=${CATALOGUE_PAGE}`)
      .then((r) => (r.ok ? r.json() : null))
      .catch(() => null),
    fetch(`${API_BASE}/api/public/agencies`)
      .then((r) => (r.ok ? r.json() : []))
      .catch(() => []),
    fetch(`${API_BASE}/api/public/capcodes/unknown?days=${UNKNOWN_DAYS}&limit=${UNKNOWN_LIMIT}`)
      .then((r) => (r.ok ? r.json() : null))
      .catch(() => null),
  ]);
  return {
    capcodes: (cp?.data ?? []) as Capcode[],
    hasMore: Boolean(cp?.has_more),
    agencies: (Array.isArray(ag) ? ag : []) as Agency[],
    unknown: (unk?.data ?? []) as UnknownRow[],
  };
}

// Search and scope live in the URL for sharing, but the whole catalogue is
// already in memory, so a change of query string must not re-run the loader.
export function shouldRevalidate({ currentUrl, nextUrl }: { currentUrl: URL; nextUrl: URL }) {
  return currentUrl.pathname !== nextUrl.pathname;
}

export function meta() {
  return [
    { title: "Capcodes — RFSPager" },
    {
      name: "description",
      content: "Every NSW PSN capcode RFSPager has identified, and the addresses we have heard but not named yet.",
    },
  ];
}

type Scope = { kind: "all" } | { kind: "agency"; id: number } | { kind: "unknown" };

export default function CapcodesPage({ loaderData }: Route.ComponentProps) {
  const { capcodes, hasMore, agencies, unknown } = loaderData;
  const [searchParams, setSearchParams] = useSearchParams();

  const scope: Scope = useMemo(() => {
    if (searchParams.get("tab") === "unknown") return { kind: "unknown" };
    const aid = Number(searchParams.get("agency_id"));
    if (aid && agencies.some((a) => a.id === aid)) return { kind: "agency", id: aid };
    return { kind: "all" };
  }, [searchParams, agencies]);
  const activeAgency = scope.kind === "agency" ? agencies.find((a) => a.id === scope.id) ?? null : null;

  const [search, setSearch] = useState(searchParams.get("search") ?? "");
  useEffect(() => {
    const t = setTimeout(() => {
      setSearchParams(
        (prev) => {
          const p = new URLSearchParams(prev);
          if (search.trim()) p.set("search", search.trim());
          else p.delete("search");
          return p;
        },
        { replace: true, preventScrollReset: true },
      );
    }, 300);
    return () => clearTimeout(t);
  }, [search, setSearchParams]);

  const setScope = (next: Scope) => {
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.delete("tab");
        p.delete("agency_id");
        if (next.kind === "unknown") p.set("tab", "unknown");
        if (next.kind === "agency") p.set("agency_id", String(next.id));
        return p;
      },
      { replace: true },
    );
    setSheetOpen(false);
  };

  const term = search.trim().toLowerCase();

  const filteredCapcodes = useMemo(() => {
    let list = capcodes;
    if (scope.kind === "agency") list = list.filter((c) => c.agency_id === scope.id);
    if (term) {
      list = list.filter(
        (c) =>
          (c.alias ?? "").toLowerCase().includes(term) ||
          c.address.toLowerCase().includes(term) ||
          (c.agency_name ?? "").toLowerCase().includes(term),
      );
    }
    return [...list].sort((a, b) => {
      const aH = Boolean(a.alias?.trim());
      const bH = Boolean(b.alias?.trim());
      if (aH !== bH) return aH ? -1 : 1;
      return (a.alias || a.address).localeCompare(b.alias || b.address, "en-AU", { numeric: true });
    });
  }, [capcodes, scope, term]);

  const filteredUnknown = useMemo(() => {
    if (!term) return unknown;
    return unknown.filter(
      (u) => u.address.toLowerCase().includes(term) || (u.agency_name ?? "").toLowerCase().includes(term),
    );
  }, [unknown, term]);

  // Render in chunks; "Show more" is the reader's intent, not a scroll trap.
  const [shown, setShown] = useState(CHUNK);
  useEffect(() => setShown(CHUNK), [scope, term]);

  const [sheetOpen, setSheetOpen] = useState(false);
  const [target, setTarget] = useState<SuggestTarget | null>(null);

  const unknownCount = unknown.length >= UNKNOWN_LIMIT ? `${UNKNOWN_LIMIT}+` : unknown.length;
  const scopeTitle = scope.kind === "all" ? "All capcodes" : scope.kind === "unknown" ? "Unknown addresses" : activeAgency!.name;

  const filterPanel = (className: string) => (
    <AgencyList
      agencies={agencies}
      activeId={scope.kind === "agency" ? String(scope.id) : scope.kind === "all" ? "" : "unknown"}
      onSelect={(id) => setScope(id ? { kind: "agency", id: Number(id) } : { kind: "all" })}
      allLabel="All capcodes"
      className={className}
    >
      <FilterRow
        label="Unknown addresses"
        count={unknownCount}
        active={scope.kind === "unknown"}
        onClick={() => setScope({ kind: "unknown" })}
      />
      <div className="my-2 border-t border-gray-100" role="presentation" />
    </AgencyList>
  );

  const list = scope.kind === "unknown" ? filteredUnknown : filteredCapcodes;
  const total = scope.kind === "unknown" ? unknown.length : scope.kind === "agency" ? activeAgency!.capcode_count : capcodes.length;

  return (
    <div className="flex-1 bg-white">
      <div className="md:flex md:items-start">
        <aside className="sticky top-16 hidden h-[calc(100dvh-4rem)] w-72 shrink-0 flex-col border-r border-gray-200 bg-white md:flex lg:w-80">
          <div className="px-6 pb-5 pt-7">
            <h1 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Capcodes</h1>
            <p className="mt-2 text-sm leading-relaxed text-gray-500">
              Every pager address we have put a name to, and the ones we have heard but not identified yet.
            </p>
          </div>
          <div className="px-4 pb-3">
            <SearchField value={search} onChange={setSearch} onClear={() => setSearch("")} placeholder="Search alias, address or agency" />
          </div>
          {filterPanel("min-h-0 flex-1 space-y-px overflow-y-auto px-3 pb-6 pt-1")}
        </aside>

        <section className="min-w-0 flex-1 bg-white">
          <div className="sticky top-16 z-20 flex h-14 items-center gap-2 border-b border-gray-200 bg-white/95 px-4 backdrop-blur md:hidden">
            <SearchField value={search} onChange={setSearch} onClear={() => setSearch("")} placeholder="Search capcodes" className="flex-1" />
            <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
              <SheetTrigger
                className={`inline-flex h-10 max-w-[10rem] shrink-0 items-center gap-1.5 rounded-lg border px-3 text-sm font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
                  scope.kind !== "all"
                    ? "border-red-100 bg-red-50 text-[#F7241D]"
                    : "border-gray-200 bg-white text-gray-700 hover:bg-gray-50"
                }`}
              >
                <SlidersHorizontal className="size-4 shrink-0" />
                <span className="truncate">{scope.kind === "all" ? "Filter" : scopeTitle}</span>
              </SheetTrigger>
              <SheetContent side="bottom" className="gap-0 rounded-t-2xl p-0">
                <SheetHeader className="border-b border-gray-100 px-5 py-4">
                  <SheetTitle className={`${heading} text-base font-bold text-gray-900`}>Show</SheetTitle>
                </SheetHeader>
                {filterPanel("max-h-[60dvh] space-y-px overflow-y-auto px-3 pb-[max(1.5rem,env(safe-area-inset-bottom))] pt-2")}
              </SheetContent>
            </Sheet>
          </div>

          <div className="border-b border-gray-200 px-4 py-4 sm:px-6">
            <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
              <h2 className={`${heading} text-xl font-extrabold tracking-tight text-gray-900`}>{scopeTitle}</h2>
              <p className="text-sm tabular-nums text-gray-500">
                {term
                  ? `${list.length.toLocaleString()} of ${total.toLocaleString()} match “${search.trim()}”`
                  : scope.kind === "unknown"
                    ? `${unknownCount} heard in the last ${UNKNOWN_DAYS} days`
                    : `${total.toLocaleString()} capcodes`}
              </p>
            </div>
            {scope.kind === "unknown" ? (
              <p className="mt-2 max-w-2xl text-sm leading-relaxed text-gray-500">
                These addresses turned up in recent pages but we don't have a name for them. If you recognise one, a
                brigade, a unit, a station, tell us. Every suggestion is checked by an admin before it goes live.
              </p>
            ) : hasMore ? (
              <p className="mt-2 text-sm text-amber-800">
                Showing the first {CATALOGUE_PAGE.toLocaleString()}. Pick an agency to narrow the list.
              </p>
            ) : null}
          </div>

          {list.length === 0 ? (
            <div className="px-6 py-28 text-center">
              <p className={`${heading} text-lg font-bold text-gray-900`}>
                {term ? `Nothing matches “${search.trim()}”` : scope.kind === "unknown" ? "Every recent address has a name." : "No capcodes here yet."}
              </p>
              {term && (
                <button type="button" onClick={() => setSearch("")} className="mt-4 text-sm font-semibold text-[#F7241D] hover:underline">
                  Clear the search
                </button>
              )}
            </div>
          ) : scope.kind === "unknown" ? (
            <ul>
              {(list as UnknownRow[]).slice(0, shown).map((row) => (
                <UnknownItem key={`${row.capcode_id ?? "x"}-${row.address}`} row={row} onSuggest={() => setTarget({ capcode_id: row.capcode_id, address: row.address, alias: row.alias, agency_name: row.agency_name })} />
              ))}
            </ul>
          ) : (
            <ul>
              {(list as Capcode[]).slice(0, shown).map((c) => (
                <CapcodeItem key={c.id} c={c} showAgency={scope.kind !== "agency"} onSuggest={() => setTarget({ capcode_id: c.id, address: c.address, alias: c.alias, agency_name: c.agency_name })} />
              ))}
            </ul>
          )}

          {list.length > shown && (
            <div className="px-4 py-6 text-center sm:px-6">
              <button
                type="button"
                onClick={() => setShown((n) => n + CHUNK)}
                className="inline-flex h-10 items-center rounded-lg border border-gray-200 bg-white px-4 text-sm font-semibold text-gray-700 outline-none transition-colors hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
              >
                Show {Math.min(CHUNK, list.length - shown)} more
              </button>
              <p className="mt-2 text-xs text-gray-400">
                {shown.toLocaleString()} of {list.length.toLocaleString()}
              </p>
            </div>
          )}
        </section>
      </div>

      <SuggestDialog target={target} agencies={agencies} onClose={() => setTarget(null)} />
    </div>
  );
}

function CapcodeItem({ c, showAgency, onSuggest }: { c: Capcode; showAgency: boolean; onSuggest: () => void }) {
  const labelled = Boolean(c.alias?.trim());
  return (
    <li className="relative flex items-center gap-4 border-b border-gray-100 px-4 py-2.5 sm:px-6 md:gap-5">
      <span aria-hidden className="absolute inset-y-0 left-0 w-[3px]" style={{ backgroundColor: c.colour || "#d1d5db" }} />
      <span className="w-[4.5rem] shrink-0 font-mono text-[13px] tabular-nums text-gray-500">{c.address}</span>
      <div className="min-w-0 flex-1">
        <p className={`${heading} truncate text-[15px] font-bold ${labelled ? "text-gray-900" : "font-medium text-gray-400"}`}>
          {labelled ? c.alias : "No name yet"}
        </p>
        {showAgency && c.agency_name && <p className="truncate text-xs text-gray-500">{c.agency_name}</p>}
      </div>
      <button
        type="button"
        onClick={onSuggest}
        className={`shrink-0 rounded-md px-2 py-1 text-xs font-semibold outline-none focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
          labelled ? "text-gray-500 hover:bg-gray-100 hover:text-gray-900" : "text-[#F7241D] hover:bg-red-50"
        }`}
      >
        {labelled ? "Suggest a change" : "Help identify"}
      </button>
    </li>
  );
}

function UnknownItem({ row, onSuggest }: { row: UnknownRow; onSuggest: () => void }) {
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => setNow(Date.now()), []);
  return (
    <li className="relative flex items-center gap-4 border-b border-gray-100 px-4 py-2.5 sm:px-6 md:gap-5">
      <span aria-hidden className="absolute inset-y-0 left-0 w-[3px] bg-gray-300" />
      <span className="w-[4.5rem] shrink-0 font-mono text-[13px] tabular-nums text-gray-700">{row.address}</span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[15px] text-gray-700">
          {row.agency_name ? (
            <>
              <span className={`${heading} font-bold text-gray-900`}>{row.agency_name}</span>
              <span className="text-gray-400"> no alias</span>
            </>
          ) : (
            <span className="text-gray-500">No agency yet</span>
          )}
        </p>
        <p className="truncate text-xs text-gray-500" suppressHydrationWarning>
          {row.message_count.toLocaleString()} {row.message_count === 1 ? "page" : "pages"}
          {now ? `, last ${ago(row.last_seen_at, now)}` : ""}
        </p>
      </div>
      <button
        type="button"
        onClick={onSuggest}
        className="shrink-0 rounded-md px-2 py-1 text-xs font-semibold text-[#F7241D] outline-none hover:bg-red-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
      >
        Help identify
      </button>
    </li>
  );
}

interface SuggestTarget {
  capcode_id: number | null;
  address: string;
  alias: string | null;
  agency_name: string | null;
}

interface RecentMessage {
  id: number;
  address: string;
  message: string;
  created_at: string;
  system_name: string | null;
}

function SuggestDialog({ target, agencies, onClose }: { target: SuggestTarget | null; agencies: Agency[]; onClose: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-md">
        {target && <SuggestForm key={target.address} target={target} agencies={agencies} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function SuggestForm({ target, agencies, onClose }: { target: SuggestTarget; agencies: Agency[]; onClose: () => void }) {
  const [alias, setAlias] = useState(target.alias ?? "");
  const [agency, setAgency] = useState(target.agency_name ?? "");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [recent, setRecent] = useState<RecentMessage[] | null>(null);
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [now, setNow] = useState(0);

  useEffect(() => {
    setNow(Date.now());
    const read = () => setSignedIn(Boolean(localStorage.getItem("rfspager_token")));
    read();
    window.addEventListener("rfspager:auth", read);
    return () => window.removeEventListener("rfspager:auth", read);
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetch(`/api/public/capcodes/messages?address=${encodeURIComponent(target.address)}&limit=6`)
      .then((r) => (r.ok ? r.json() : { data: [] }))
      .then((j) => !cancelled && setRecent(j.data ?? []))
      .catch(() => !cancelled && setRecent([]));
    return () => {
      cancelled = true;
    };
  }, [target.address]);

  const isAmendment = target.capcode_id != null;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!alias.trim() && !agency.trim() && !note.trim()) {
      setError("Give us at least one of a name, an agency or a note.");
      return;
    }
    const token = localStorage.getItem("rfspager_token");
    if (!token) {
      setSignedIn(false);
      return;
    }
    setSubmitting(true);
    try {
      const res = await fetch("/api/v2/mobile/capcode-suggestions", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify({ capcode_id: target.capcode_id, address: target.address, alias: alias.trim(), agency: agency.trim(), note: note.trim() }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error(j.error || `The server said ${res.status}.`);
      }
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Couldn't send that. Try again in a moment.");
    } finally {
      setSubmitting(false);
    }
  };

  const inputClass =
    "h-10 w-full rounded-lg border border-gray-200 bg-gray-50 px-3 text-sm text-gray-900 outline-none transition-colors placeholder:text-gray-400 focus:border-[#F7241D] focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15";

  return (
    <>
      <DialogHeader>
        <DialogTitle className={`${heading} text-xl font-extrabold tracking-tight`}>
          {isAmendment ? "Suggest a change" : "Help identify"} <span className="font-mono text-base font-semibold text-gray-500">{target.address}</span>
        </DialogTitle>
        <DialogDescription>
          {isAmendment && (target.alias || target.agency_name)
            ? `Currently ${target.alias || "unnamed"}${target.agency_name ? `, ${target.agency_name}` : ""}.`
            : "An admin checks every suggestion before it goes live."}
        </DialogDescription>
      </DialogHeader>

      {recent && recent.length > 0 && (
        <div className="rounded-lg border border-gray-200 bg-gray-50">
          <p className="border-b border-gray-200 px-3 py-2 text-xs font-semibold text-gray-600">Recent pages to this address</p>
          <ul className="max-h-40 divide-y divide-gray-200 overflow-y-auto">
            {recent.map((m) => (
              <li key={m.id} className="px-3 py-2">
                <p className="line-clamp-2 whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-gray-800">{m.message}</p>
                <p className="mt-0.5 text-[11px] text-gray-500" suppressHydrationWarning>
                  {now ? ago(m.created_at, now) : ""}
                  {m.system_name ? `, decoded by ${m.system_name}` : ""}
                </p>
              </li>
            ))}
          </ul>
        </div>
      )}

      {signedIn === false ? (
        <div className="space-y-3 pt-2 text-sm text-gray-600">
          <p>Sign in to send a suggestion, so we can follow up if we have a question.</p>
          <Link to={`/login?next=${encodeURIComponent("/capcodes")}`} className="inline-flex h-10 items-center rounded-lg bg-[#F7241D] px-4 text-sm font-semibold text-white hover:bg-[#d91f19]">
            Sign in
          </Link>
        </div>
      ) : done ? (
        <div className="space-y-3 pt-2">
          <p className={`${heading} font-bold text-gray-900`}>Sent. Thank you.</p>
          <p className="text-sm text-gray-600">An admin will review it. You can suggest more from this page.</p>
          <button type="button" onClick={onClose} className="inline-flex h-10 items-center rounded-lg bg-gray-900 px-4 text-sm font-semibold text-white hover:bg-gray-800">
            Done
          </button>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-4">
          <div>
            <label htmlFor="sg-agency" className="mb-1.5 block text-sm font-semibold text-gray-800">
              Agency
            </label>
            <input id="sg-agency" list="agencies-list" value={agency} onChange={(e) => setAgency(e.target.value)} placeholder="Pick from the list if it's there" className={inputClass} />
            <datalist id="agencies-list">
              {agencies.map((a) => (
                <option key={a.id} value={a.name} />
              ))}
            </datalist>
          </div>
          <div>
            <label htmlFor="sg-alias" className="mb-1.5 block text-sm font-semibold text-gray-800">
              Name
            </label>
            <input id="sg-alias" value={alias} onChange={(e) => setAlias(e.target.value)} placeholder="e.g. Cooma Fire Brigade" className={inputClass} />
          </div>
          <div>
            <label htmlFor="sg-note" className="mb-1.5 block text-sm font-semibold text-gray-800">
              How do you know?
            </label>
            <textarea id="sg-note" value={note} onChange={(e) => setNote(e.target.value)} rows={3} placeholder="Station number, brigade, anything that helps us verify." className={`${inputClass} h-auto resize-none py-2`} />
          </div>
          {error && (
            <p role="alert" className="text-sm text-red-700">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-1">
            <button type="button" onClick={onClose} className="h-10 rounded-lg px-4 text-sm font-semibold text-gray-700 hover:bg-gray-100">
              Cancel
            </button>
            <button type="submit" disabled={submitting} className="h-10 rounded-lg bg-[#F7241D] px-4 text-sm font-semibold text-white hover:bg-[#d91f19] disabled:opacity-60">
              {submitting ? "Sending…" : "Send suggestion"}
            </button>
          </div>
        </form>
      )}
    </>
  );
}
