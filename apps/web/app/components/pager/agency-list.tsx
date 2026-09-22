import { useMemo } from "react";

import type { Agency } from "./types";

/**
 * Single-select agency filter, shared by the desktop rail and the phone sheet.
 * Sorted by capcode count so the busiest agencies sit at the top.
 */
export function AgencyList({
  agencies,
  activeId,
  onSelect,
  allLabel = "All agencies",
  children,
  className = "",
}: {
  agencies: Agency[];
  activeId: string;
  onSelect: (id: string) => void;
  /** Label for the "everything" row at the top. */
  allLabel?: string;
  /** Extra rows rendered between the "all" row and the agencies. */
  children?: React.ReactNode;
  className?: string;
}) {
  const sorted = useMemo(() => [...agencies].sort((a, b) => b.capcode_count - a.capcode_count), [agencies]);
  const total = useMemo(() => agencies.reduce((n, a) => n + a.capcode_count, 0), [agencies]);

  return (
    <div role="radiogroup" aria-label="Filter by agency" className={className}>
      <FilterRow label={allLabel} count={total} active={activeId === ""} onClick={() => onSelect("")} />
      {children}
      {sorted.map((a) => {
        const id = String(a.id);
        const active = id === activeId;
        return (
          <FilterRow
            key={a.id}
            label={a.name}
            count={a.capcode_count}
            active={active}
            onClick={() => onSelect(active ? "" : id)}
          />
        );
      })}
    </div>
  );
}

export function FilterRow({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number | string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      className={`group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 ${
        active ? "bg-gray-900 text-white" : "text-gray-700 hover:bg-gray-100"
      }`}
    >
      <span className="min-w-0 flex-1 truncate text-sm font-medium">{label}</span>
      <span
        className={`text-xs tabular-nums ${active ? "text-white/70" : "text-gray-400 group-hover:text-gray-500"}`}
        aria-label={`${count} capcodes`}
      >
        {typeof count === "number" ? count.toLocaleString() : count}
      </span>
    </button>
  );
}
