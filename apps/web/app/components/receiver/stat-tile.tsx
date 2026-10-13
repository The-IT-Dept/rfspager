import type { ReactNode } from "react";
import { TriangleAlert } from "lucide-react";
import { cn } from "~/lib/utils";

interface TileProps {
  label: string;
  value: ReactNode;
  unit?: string;
  /** Warnings carry an icon and a wash, never colour alone. */
  warn?: boolean;
  hint?: string;
  className?: string;
}

export function StatTile({ label, value, unit, warn, hint, className }: TileProps) {
  return (
    <div
      className={cn(
        "min-w-0 rounded-lg border px-3 py-2",
        warn ? "border-amber-400 bg-amber-50 dark:border-amber-700 dark:bg-amber-950/30" : "border-border bg-card",
        className,
      )}
      title={hint}
    >
      <p className="flex items-center gap-1 text-[11px] uppercase tracking-wide text-muted-foreground">
        <span className="truncate">{label}</span>
        {warn && (
          <TriangleAlert className="size-3 shrink-0 text-amber-600 dark:text-amber-400" aria-label="warning" />
        )}
      </p>
      <p className="mt-0.5 truncate text-lg font-semibold leading-tight">
        {value}
        {unit && <span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span>}
      </p>
    </div>
  );
}

export function TileGroup({ title, aside, children }: { title: ReactNode; aside?: ReactNode; children: ReactNode }) {
  return (
    <section>
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        {aside && <span className="text-xs text-muted-foreground">{aside}</span>}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">{children}</div>
    </section>
  );
}
