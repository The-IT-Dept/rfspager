import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import type { Route } from "./+types/capcodes";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

interface Capcode {
  id: number;
  address: string;
  alias: string | null;
  icon: string | null;
  colour: string | null;
  agency_id: number | null;
  agency_name: string | null;
}

interface Agency {
  id: number;
  name: string;
  capcode_count: number;
}

interface UnknownRow {
  capcode_id: number | null;
  address: string;
  alias: string | null;
  agency_name: string | null;
  message_count: number;
  last_seen_at: string;
}

interface LoaderData {
  capcodes: { data: Capcode[]; has_more: boolean };
  agencies: Agency[];
  unknown: { data: UnknownRow[] };
}

export async function loader({ request }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const params = new URLSearchParams();
  // We fetch up to 2500 capcodes at once — total NSW count is ~1500 and the
  // backend caps at 2500, so a single trip is fine and beats infinite scroll.
  params.set("page", "1");
  params.set("per_page", "2500");
  if (url.searchParams.get("search")) params.set("search", url.searchParams.get("search")!);
  if (url.searchParams.get("agency_id")) params.set("agency_id", url.searchParams.get("agency_id")!);

  try {
    const [cpRes, agRes, unkRes] = await Promise.all([
      fetch(`${API_BASE}/api/public/capcodes?${params}`),
      fetch(`${API_BASE}/api/public/agencies`),
      fetch(`${API_BASE}/api/public/capcodes/unknown?days=30&limit=300`),
    ]);
    return {
      capcodes: cpRes.ok ? await cpRes.json() : { data: [], has_more: false },
      agencies: agRes.ok ? await agRes.json() : [],
      unknown: unkRes.ok ? await unkRes.json() : { data: [] },
    } satisfies LoaderData;
  } catch {
    return {
      capcodes: { data: [], has_more: false },
      agencies: [],
      unknown: { data: [] },
    } satisfies LoaderData;
  }
}

export function meta() {
  return [
    { title: "Capcodes — RFSPager" },
    {
      name: "description",
      content:
        "Browse every NSW PSN capcode RFSPager decodes. Help us label unknown addresses by submitting a suggestion.",
    },
  ];
}

type Scope =
  | { kind: "all" }
  | { kind: "agency"; id: number; name: string }
  | { kind: "unknown" };

export default function CapcodesPage({ loaderData }: Route.ComponentProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [search, setSearch] = useState(searchParams.get("search") || "");
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [scope, setScope] = useState<Scope>(() => {
    const tab = searchParams.get("tab");
    const aid = searchParams.get("agency_id");
    if (tab === "unknown") return { kind: "unknown" };
    if (aid) {
      const a = loaderData.agencies.find((x: Agency) => String(x.id) === aid);
      if (a) return { kind: "agency", id: a.id, name: a.name };
    }
    return { kind: "all" };
  });

  const [suggestTarget, setSuggestTarget] = useState<{
    capcode_id: number | null;
    address: string;
    alias: string | null;
    agency_name: string | null;
  } | null>(null);

  // Persist scope to URL so it survives reload + sharing.
  useEffect(() => {
    const p = new URLSearchParams(searchParams);
    p.delete("tab");
    p.delete("agency_id");
    if (scope.kind === "unknown") p.set("tab", "unknown");
    if (scope.kind === "agency") p.set("agency_id", String(scope.id));
    setSearchParams(p, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope]);

  const onSearch = (v: string) => {
    setSearch(v);
    if (debounce.current) clearTimeout(debounce.current);
    debounce.current = setTimeout(() => {
      const p = new URLSearchParams(searchParams);
      if (v) p.set("search", v);
      else p.delete("search");
      setSearchParams(p, { replace: true });
    }, 300);
  };

  const capcodes: Capcode[] = loaderData.capcodes.data;
  const agencies: Agency[] = loaderData.agencies;
  const unknown: UnknownRow[] = loaderData.unknown.data;

  const sortedAgencies = useMemo(
    () => [...agencies].sort((a: Agency, b: Agency) => b.capcode_count - a.capcode_count),
    [agencies],
  );

  // Client-side filter by current scope + search.
  const filteredCapcodes = useMemo(() => {
    let list = capcodes;
    if (scope.kind === "agency") {
      list = list.filter((c) => c.agency_id === scope.id);
    }
    if (search.trim()) {
      const s = search.trim().toLowerCase();
      list = list.filter(
        (c) =>
          (c.alias ?? "").toLowerCase().includes(s) ||
          c.address.toLowerCase().includes(s) ||
          (c.agency_name ?? "").toLowerCase().includes(s),
      );
    }
    // Aliased first, then unaliased; both alphabetical.
    return [...list].sort((a, b) => {
      const aH = !!(a.alias && a.alias.trim());
      const bH = !!(b.alias && b.alias.trim());
      if (aH !== bH) return aH ? -1 : 1;
      return (a.alias || a.address).localeCompare(b.alias || b.address);
    });
  }, [capcodes, scope, search]);

  const filteredUnknowns = useMemo(() => {
    if (!search.trim()) return unknown;
    const s = search.trim().toLowerCase();
    return unknown.filter(
      (u) =>
        u.address.toLowerCase().includes(s) ||
        (u.agency_name ?? "").toLowerCase().includes(s),
    );
  }, [unknown, search]);

  const scopeTitle =
    scope.kind === "all"
      ? "All capcodes"
      : scope.kind === "unknown"
        ? "Unknown capcodes"
        : scope.name;
  const scopeCount = scope.kind === "unknown" ? filteredUnknowns.length : filteredCapcodes.length;
  const scopeTotal =
    scope.kind === "all"
      ? capcodes.length
      : scope.kind === "agency"
        ? capcodes.filter((c) => c.agency_id === scope.id).length
        : unknown.length;

  return (
    <div className="flex flex-1 overflow-hidden bg-gray-50 h-[calc(100dvh-4rem)]">
      {/* ——— Sidebar ——— */}
      <aside className="w-72 lg:w-80 bg-white border-r border-gray-200 flex flex-col shrink-0 hidden md:flex">
        <div className="px-5 py-4 border-b border-gray-100">
          <h1 className={`${heading} text-xl font-extrabold text-gray-900 tracking-tight`}>
            Capcodes
          </h1>
          <p className="text-xs text-gray-400 mt-0.5">
            {capcodes.length} total · {unknown.length} unknown
          </p>
        </div>

        {/* Quick scopes */}
        <div className="px-3 py-3 border-b border-gray-100 space-y-1">
          <ScopeRow
            label="All capcodes"
            count={capcodes.length}
            active={scope.kind === "all"}
            onClick={() => setScope({ kind: "all" })}
            tint="#111827"
          />
          <ScopeRow
            label="Unknown"
            count={unknown.length}
            active={scope.kind === "unknown"}
            onClick={() => setScope({ kind: "unknown" })}
            tint="#F7241D"
            highlight
          />
        </div>

        {/* Agency list */}
        <div className="px-5 pt-3 pb-1.5">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider">
            By agency
          </p>
        </div>
        <div className="flex-1 overflow-y-auto px-3 pb-4 space-y-px">
          {sortedAgencies.map((a) => {
            const active = scope.kind === "agency" && scope.id === a.id;
            return (
              <ScopeRow
                key={a.id}
                label={a.name}
                count={a.capcode_count}
                active={active}
                onClick={() => setScope({ kind: "agency", id: a.id, name: a.name })}
              />
            );
          })}
        </div>
      </aside>

      {/* ——— Main pane ——— */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <header className="bg-white border-b border-gray-200 px-4 md:px-6 py-4">
          <div className="flex flex-col md:flex-row md:items-end gap-3 md:gap-6">
            <div className="flex-1 min-w-0">
              <p className="text-[10px] font-bold text-gray-400 uppercase tracking-widest mb-1">
                {scope.kind === "unknown"
                  ? "Help us identify"
                  : scope.kind === "agency"
                    ? "Agency"
                    : "Catalogue"}
              </p>
              <h2
                className={`${heading} text-2xl md:text-3xl font-extrabold text-gray-900 leading-tight tracking-tight truncate`}
              >
                {scopeTitle}
              </h2>
            </div>

            <div className="flex items-center gap-6 shrink-0">
              <div>
                <p
                  className={`${heading} text-2xl md:text-3xl font-extrabold text-gray-900 leading-none tabular-nums`}
                >
                  {scopeCount.toLocaleString()}
                </p>
                <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mt-1">
                  Showing{search && scopeCount !== scopeTotal ? ` of ${scopeTotal}` : ""}
                </p>
              </div>
            </div>
          </div>

          {/* Search */}
          <div className="mt-4 relative max-w-md">
            <svg
              className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
              />
            </svg>
            <input
              type="text"
              placeholder={
                scope.kind === "unknown"
                  ? "Search address…"
                  : "Search alias, address, agency…"
              }
              value={search}
              onChange={(e) => onSearch(e.target.value)}
              className="w-full pl-9 pr-9 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none transition-all"
            />
            {search && (
              <button
                onClick={() => onSearch("")}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M6 18L18 6M6 6l12 12"
                  />
                </svg>
              </button>
            )}
          </div>
        </header>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4 md:p-6">
          {scope.kind === "unknown" ? (
            <UnknownGrid
              rows={filteredUnknowns}
              onSuggest={(row) =>
                setSuggestTarget({
                  capcode_id: row.capcode_id,
                  address: row.address,
                  alias: row.alias,
                  agency_name: row.agency_name,
                })
              }
              searchActive={!!search.trim()}
            />
          ) : (
            <CapcodeGrid
              items={filteredCapcodes}
              onSuggest={(c) =>
                setSuggestTarget({
                  capcode_id: c.id,
                  address: c.address,
                  alias: c.alias,
                  agency_name: c.agency_name,
                })
              }
              searchActive={!!search.trim()}
              showAgency={scope.kind !== "agency"}
            />
          )}
        </div>
      </div>

      {suggestTarget && (
        <SuggestModal
          target={suggestTarget}
          agencies={agencies}
          onClose={() => setSuggestTarget(null)}
        />
      )}
    </div>
  );
}

function ScopeRow({
  label,
  count,
  active,
  onClick,
  tint,
  highlight,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  tint?: string;
  highlight?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 rounded-md flex items-center gap-3 transition-colors group ${
        active
          ? "bg-gray-900 text-white"
          : highlight
            ? "hover:bg-red-50 text-gray-800"
            : "hover:bg-gray-50 text-gray-700"
      }`}
    >
      {tint && (
        <span
          className={`w-1.5 h-1.5 rounded-full shrink-0 ${active ? "opacity-100" : "opacity-70"}`}
          style={{ backgroundColor: tint }}
        />
      )}
      <span className={`text-sm font-medium flex-1 truncate ${active ? "" : ""}`}>{label}</span>
      <span
        className={`text-[11px] tabular-nums ${
          active ? "text-white/70" : "text-gray-400 group-hover:text-gray-500"
        }`}
      >
        {count.toLocaleString()}
      </span>
    </button>
  );
}

function CapcodeGrid({
  items,
  onSuggest,
  searchActive,
  showAgency,
}: {
  items: Capcode[];
  onSuggest: (c: Capcode) => void;
  searchActive: boolean;
  showAgency: boolean;
}) {
  if (items.length === 0) {
    return (
      <div className="text-center py-32 text-sm text-gray-400">
        {searchActive ? "No capcodes match your search." : "No capcodes here yet."}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-3">
      {items.map((c) => {
        const labelled = c.alias && c.alias.trim() !== "";
        return (
          <div
            key={c.id}
            className="group bg-white border border-gray-100 rounded-xl p-4 hover:border-gray-300 hover:shadow-sm transition-all flex flex-col"
          >
            <div className="flex items-start justify-between gap-2 mb-3">
              <span
                className="inline-block px-2 py-0.5 rounded text-[10px] font-bold text-white uppercase tracking-wide font-mono"
                style={{ backgroundColor: c.colour || "#6b7280" }}
              >
                {c.address}
              </span>
              <button
                onClick={() => onSuggest(c)}
                className="text-[10px] font-bold uppercase tracking-wider text-gray-400 hover:text-[#F7241D] opacity-0 group-hover:opacity-100 transition-opacity"
                title="Suggest a change"
              >
                Suggest
              </button>
            </div>
            <p
              className={`text-sm font-semibold leading-snug mb-1 break-words ${
                labelled ? "text-gray-900" : "text-gray-400 italic"
              }`}
            >
              {labelled ? c.alias : "Unknown — no alias yet"}
            </p>
            {showAgency && c.agency_name && (
              <p className="text-[11px] text-gray-400 mt-auto truncate">{c.agency_name}</p>
            )}
          </div>
        );
      })}
    </div>
  );
}

function UnknownGrid({
  rows,
  onSuggest,
  searchActive,
}: {
  rows: UnknownRow[];
  onSuggest: (r: UnknownRow) => void;
  searchActive: boolean;
}) {
  if (rows.length === 0) {
    return (
      <div className="text-center py-32">
        <p className="text-base font-semibold text-gray-900 mb-1">
          {searchActive ? "Nothing matches" : "All clear."}
        </p>
        <p className="text-sm text-gray-400">
          {searchActive
            ? "Try a different search."
            : "No unlabelled capcodes from the last 30 days."}
        </p>
      </div>
    );
  }
  return (
    <>
      <div className="mb-5 rounded-xl bg-amber-50 border border-amber-100 px-4 py-3">
        <p className="text-sm text-amber-900 leading-relaxed">
          These addresses appeared in recent pager messages but we don't have a
          name for them yet. If you recognise one — a brigade, a unit, a
          station — drop a suggestion. Admins review and merge.
        </p>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
        {rows.map((row) => (
          <div
            key={`${row.capcode_id ?? "x"}-${row.address}`}
            className="bg-white border border-gray-100 rounded-xl p-4 flex flex-col"
          >
            <div className="flex items-start justify-between gap-2 mb-3">
              <span className="inline-block px-2 py-0.5 rounded text-[10px] font-bold text-white uppercase tracking-wide bg-gray-500 font-mono">
                {row.address}
              </span>
              <span className="text-[10px] font-bold text-gray-400 tabular-nums">
                {row.message_count} msgs
              </span>
            </div>
            <p className="text-sm text-gray-700 mb-1 leading-snug">
              {row.agency_name ? (
                <>
                  <span className="font-semibold text-gray-900">{row.agency_name}</span>
                  <span className="text-gray-400 italic"> — no alias set</span>
                </>
              ) : (
                <span className="italic text-gray-500">Unmatched address</span>
              )}
            </p>
            <p className="text-[11px] text-gray-400 mb-3">last seen {timeAgo(row.last_seen_at)}</p>
            <button
              onClick={() => onSuggest(row)}
              className="mt-auto px-3 py-2 rounded-lg bg-[#F7241D] hover:bg-[#d91f19] text-white text-xs font-bold uppercase tracking-wider transition-colors"
            >
              Help identify
            </button>
          </div>
        ))}
      </div>
    </>
  );
}

function timeAgo(dateStr: string): string {
  const mins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function SuggestModal({
  target,
  agencies,
  onClose,
}: {
  target: {
    capcode_id: number | null;
    address: string;
    alias: string | null;
    agency_name: string | null;
  };
  agencies: Agency[];
  onClose: () => void;
}) {
  const [alias, setAlias] = useState(target.alias ?? "");
  const [agency, setAgency] = useState(target.agency_name ?? "");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isAmendment = target.capcode_id != null;
  const tokenAvailable =
    typeof window !== "undefined" && !!localStorage.getItem("rfspager_token");

  const submit = async () => {
    setError(null);
    if (!alias && !agency && !note) {
      setError("Provide an alias, agency, or note.");
      return;
    }
    setSubmitting(true);
    try {
      const token = localStorage.getItem("rfspager_token");
      const res = await fetch("/api/v2/mobile/capcode-suggestions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          capcode_id: target.capcode_id,
          address: target.address,
          alias,
          agency,
          note,
        }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error(j.error || `HTTP ${res.status}`);
      }
      setDone(true);
    } catch (e: any) {
      setError(e.message || "Failed to submit");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end md:items-center justify-center bg-black/40 backdrop-blur-sm animate-in fade-in-0 duration-150">
      <div className="absolute inset-0" onClick={onClose} />
      <div className="relative bg-white w-full md:w-[480px] md:rounded-2xl rounded-t-2xl shadow-2xl max-h-[90vh] overflow-y-auto animate-in slide-in-from-bottom-4 duration-200">
        <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between sticky top-0 bg-white">
          <div>
            <p className="text-[10px] font-bold text-[#F7241D] uppercase tracking-[0.15em] mb-1">
              {isAmendment ? "Amend capcode" : "Identify capcode"}
            </p>
            <h2 className={`${heading} text-xl font-extrabold text-gray-900`}>
              {target.address}
            </h2>
          </div>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-700 text-2xl leading-none w-8 h-8 flex items-center justify-center rounded-full hover:bg-gray-100"
          >
            ×
          </button>
        </div>

        {!tokenAvailable ? (
          <div className="p-6 text-center space-y-3">
            <p className="text-sm text-gray-700">
              Sign in to submit a suggestion. Admins review every change before
              it goes live.
            </p>
            <Link
              to="/login"
              className="inline-block px-5 py-2.5 rounded-lg bg-[#F7241D] hover:bg-[#d91f19] text-white text-sm font-bold transition-colors"
            >
              Sign in
            </Link>
          </div>
        ) : done ? (
          <div className="p-8 text-center space-y-3">
            <div className="mx-auto w-14 h-14 rounded-full bg-emerald-50 flex items-center justify-center">
              <svg className="w-7 h-7 text-emerald-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
              </svg>
            </div>
            <p className="font-bold text-gray-900">Submitted. Thanks.</p>
            <p className="text-sm text-gray-500 max-w-sm mx-auto">
              An admin will review your suggestion. You can submit more from this page.
            </p>
            <button
              onClick={onClose}
              className="mt-4 px-5 py-2 rounded-lg bg-gray-100 hover:bg-gray-200 text-sm font-semibold text-gray-700"
            >
              Done
            </button>
          </div>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
            className="p-6 space-y-5"
          >
            {isAmendment && (target.alias || target.agency_name) && (
              <div className="rounded-lg bg-gray-50 border border-gray-100 p-3 text-xs">
                <p className="text-gray-400 uppercase tracking-wider font-bold mb-1">Currently</p>
                <p className="text-gray-700">
                  {target.alias || <span className="italic text-gray-400">no alias</span>}
                  {target.agency_name && <span className="text-gray-400"> · {target.agency_name}</span>}
                </p>
              </div>
            )}

            <Field label="Alias">
              <input
                type="text"
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
                placeholder="e.g. Cooma Fire Brigade"
                className="w-full px-3 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none"
              />
            </Field>

            <Field label="Agency">
              <input
                type="text"
                value={agency}
                onChange={(e) => setAgency(e.target.value)}
                placeholder="e.g. NSW Rural Fire Service"
                list="agencies-list"
                className="w-full px-3 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none"
              />
              <datalist id="agencies-list">
                {agencies.map((a) => (
                  <option key={a.id} value={a.name} />
                ))}
              </datalist>
            </Field>

            <Field
              label="Note for the reviewer"
              hint="How do you know this? Source, station number, brigade, anything that'll help us verify."
            >
              <textarea
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={3}
                placeholder="e.g. Spent 8 years there — this address pages our truck."
                className="w-full px-3 py-2 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15 focus:border-[#F7241D] outline-none resize-none"
              />
            </Field>

            {error && <p className="text-xs text-[#F7241D] font-medium">{error}</p>}

            <div className="flex items-center gap-3 pt-2">
              <button
                type="button"
                onClick={onClose}
                className="flex-1 px-4 py-2.5 rounded-lg bg-gray-100 hover:bg-gray-200 text-sm font-semibold text-gray-700"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={submitting}
                className="flex-1 px-4 py-2.5 rounded-lg bg-[#F7241D] hover:bg-[#d91f19] disabled:opacity-50 disabled:cursor-not-allowed text-sm font-bold text-white"
              >
                {submitting ? "Submitting…" : "Submit suggestion"}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="text-[11px] font-bold uppercase tracking-wider text-gray-500 mb-1.5 block">
        {label}
      </span>
      {children}
      {hint && <p className="text-[11px] text-gray-400 mt-1.5 leading-relaxed">{hint}</p>}
    </label>
  );
}
