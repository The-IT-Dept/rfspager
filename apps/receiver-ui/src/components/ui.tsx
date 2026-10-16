import {
  forwardRef,
  useEffect,
  useRef,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
} from "react";
import { TriangleAlert, X } from "lucide-react";
import { cn } from "../lib/format";

type ButtonVariant = "outline" | "primary" | "ghost" | "danger";
type ButtonSize = "sm" | "md";

const BUTTON_VARIANT: Record<ButtonVariant, string> = {
  outline: "border border-border bg-white text-foreground hover:bg-zinc-50 active:bg-zinc-100",
  primary: "border border-brand bg-brand text-white hover:bg-brand-dark hover:border-brand-dark",
  ghost: "border border-transparent text-muted-foreground hover:bg-zinc-100 hover:text-foreground",
  danger: "border border-red-600 bg-red-600 text-white hover:bg-red-700 hover:border-red-700",
};

const BUTTON_SIZE: Record<ButtonSize, string> = {
  sm: "h-7 px-2.5 text-xs gap-1.5 [&_svg]:size-3.5",
  md: "h-8 px-3 text-[13px] gap-2 [&_svg]:size-4",
};

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }
>(function Button({ variant = "outline", size = "sm", className, type = "button", ...props }, ref) {
  return (
    <button
      ref={ref}
      type={type}
      className={cn(
        "inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-md font-semibold transition-colors",
        "outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:opacity-50 disabled:pointer-events-none",
        BUTTON_VARIANT[variant],
        BUTTON_SIZE[size],
        className,
      )}
      {...props}
    />
  );
});

export type Tone = "ok" | "warn" | "bad" | "info" | "muted";

const PILL_TONE: Record<Tone, { pill: string; dot: string }> = {
  ok: { pill: "bg-green-100 text-green-800", dot: "bg-green-600" },
  warn: { pill: "bg-amber-100 text-amber-800", dot: "bg-amber-600" },
  bad: { pill: "bg-red-100 text-red-800", dot: "bg-red-600" },
  info: { pill: "bg-sky-100 text-sky-800", dot: "bg-sky-600" },
  muted: { pill: "bg-zinc-100 text-zinc-600", dot: "bg-zinc-400" },
};

/** Rounded status pill with a dot. Tone is never the only signal: the label says it too. */
export function Pill({
  tone,
  children,
  className,
  pulse,
  title,
}: {
  tone: Tone;
  children: ReactNode;
  className?: string;
  pulse?: boolean;
  title?: string;
}) {
  const t = PILL_TONE[tone];
  return (
    <span
      title={title}
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-semibold leading-4",
        t.pill,
        className,
      )}
    >
      <span className={cn("size-1.5 shrink-0 rounded-full", t.dot, pulse && "animate-pulse")} aria-hidden />
      {children}
    </span>
  );
}

/** Small square-ish tag, e.g. "filtered", "primary", level names. */
export function Tag({ children, className, title }: { children: ReactNode; className?: string; title?: string }) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex items-center whitespace-nowrap rounded px-1.5 text-[10px] font-semibold uppercase leading-4 tracking-wide",
        "bg-zinc-100 text-zinc-600",
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Panel({
  title,
  aside,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cn("min-w-0 rounded-lg border border-border bg-card", className)}>
      {(title || aside) && (
        <header className="flex min-h-10 flex-wrap items-center justify-between gap-x-3 gap-y-1 px-3 pt-2.5 pb-1.5">
          {title && <h2 className="text-[11px] font-bold uppercase tracking-[0.05em] text-muted-foreground">{title}</h2>}
          {aside && <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">{aside}</div>}
        </header>
      )}
      <div className={cn("px-3 pb-3", !title && !aside && "pt-3", bodyClassName)}>{children}</div>
    </section>
  );
}

export function Tile({
  label,
  value,
  unit,
  warn,
  hint,
  sub,
}: {
  label: string;
  value: ReactNode;
  unit?: string;
  /** Warnings carry an icon and a wash, never colour alone. */
  warn?: boolean;
  hint?: string;
  sub?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "min-w-0 rounded-lg border px-3 py-2",
        warn ? "border-amber-400 bg-amber-50" : "border-border bg-card",
      )}
      title={hint}
    >
      <p className="flex items-center gap-1 text-[10.5px] uppercase tracking-[0.04em] text-muted-foreground">
        <span className="truncate">{label}</span>
        {warn && <TriangleAlert className="size-3 shrink-0 text-amber-600" aria-label="warning" />}
      </p>
      <p className="mt-0.5 truncate text-xl font-bold leading-tight tabular-nums">
        {value}
        {unit && <span className="ml-1 text-xs font-medium text-muted-foreground">{unit}</span>}
      </p>
      {sub && <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{sub}</div>}
    </div>
  );
}

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }>(
  function Input({ className, invalid, ...props }, ref) {
    return (
      <input
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(
          "h-8 w-full min-w-0 rounded-md border border-border bg-white px-2.5 text-[13px] outline-none transition-colors",
          "placeholder:text-zinc-400 focus:border-zinc-400 focus:ring-2 focus:ring-zinc-200",
          invalid && "border-red-500 focus:border-red-500 focus:ring-red-100",
          className,
        )}
        {...props}
      />
    );
  },
);

export function Switch({
  checked,
  onChange,
  label,
  id,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label?: ReactNode;
  id?: string;
  disabled?: boolean;
}) {
  return (
    <label className={cn("inline-flex cursor-pointer select-none items-center gap-2 text-xs", disabled && "opacity-50")}>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cn(
          "relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors outline-none",
          "focus-visible:ring-2 focus-visible:ring-ring/60",
          checked ? "bg-zinc-900" : "bg-zinc-300",
        )}
      >
        <span
          className={cn(
            "inline-block size-3 rounded-full bg-white shadow transition-transform",
            checked ? "translate-x-3.5" : "translate-x-0.5",
          )}
        />
      </button>
      {label}
    </label>
  );
}

export function Select({
  value,
  onChange,
  options,
  className,
  ariaLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
  className?: string;
  ariaLabel?: string;
}) {
  return (
    <select
      aria-label={ariaLabel}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={cn(
        "h-7 rounded-md border border-border bg-white px-2 text-xs font-medium outline-none focus:ring-2 focus:ring-zinc-200",
        className,
      )}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

/** Segmented control, e.g. the stats window picker. */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  ariaLabel,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: string }[];
  ariaLabel?: string;
}) {
  return (
    <div role="radiogroup" aria-label={ariaLabel} className="inline-flex rounded-md border border-border bg-zinc-50 p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          onClick={() => onChange(o.value)}
          className={cn(
            "h-6 rounded px-2.5 text-xs font-semibold transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
            o.value === value ? "bg-white text-foreground shadow-sm ring-1 ring-border" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Modal built on <dialog>: focus handling, Escape and the backdrop come from the browser. */
export function Dialog({
  open,
  onClose,
  title,
  children,
  footer,
  className,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
      className={cn(
        "m-auto w-[min(32rem,calc(100vw-1.5rem))] rounded-xl border border-border bg-white p-0 text-foreground shadow-xl",
        "backdrop:bg-zinc-950/30 backdrop:backdrop-blur-[1px]",
        className,
      )}
    >
      {open && (
        <div className="flex max-h-[calc(100dvh-2rem)] flex-col">
          <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
            <h2 className="text-sm font-bold">{title}</h2>
            <button
              type="button"
              onClick={onClose}
              className="rounded p-1 text-muted-foreground hover:bg-zinc-100 hover:text-foreground"
              aria-label="Close"
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="overflow-y-auto px-4 py-4">{children}</div>
          {footer && <div className="flex justify-end gap-2 border-t border-border px-4 py-3">{footer}</div>}
        </div>
      )}
    </dialog>
  );
}

export function EmptyRow({ colSpan, children }: { colSpan: number; children: ReactNode }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-2 py-6 text-center text-muted-foreground">
        {children}
      </td>
    </tr>
  );
}

export function ErrorNote({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800", className)}>{children}</p>
  );
}

/** Prefix a stacked cell with its column name on phones: put `data-label` on the <td>. */
export const CELL_LABEL =
  "max-md:before:mr-1 max-md:before:text-muted-foreground max-md:before:content-[attr(data-label)]";

/**
 * Shared table styling: header row, hairline row rules, horizontal scroll on
 * narrow screens. With `stack`, rows turn into wrapped blocks below md so
 * nothing hides off the right edge of a phone.
 */
export function Table({ children, className, stack }: { children: ReactNode; className?: string; stack?: boolean }) {
  return (
    <div className={cn("-mx-3 overflow-x-auto px-3", className)}>
      <table
        className={cn(
          "w-full border-collapse text-left text-[12.5px] [&_td]:border-b [&_td]:border-hairline [&_td]:px-2 [&_td]:py-2 [&_td]:align-middle [&_th]:whitespace-nowrap [&_th]:border-b [&_th]:border-border [&_th]:px-2 [&_th]:py-1.5 [&_th]:text-[10.5px] [&_th]:font-semibold [&_th]:uppercase [&_th]:tracking-[0.04em] [&_th]:text-muted-foreground [&_tr:last-child_td]:border-b-0",
          stack &&
            "max-md:block max-md:[&_tbody]:block max-md:[&_thead]:hidden max-md:[&_tr]:flex max-md:[&_tr]:flex-wrap max-md:[&_tr]:items-center max-md:[&_tr]:gap-x-3 max-md:[&_tr]:gap-y-1.5 max-md:[&_tr]:border-b max-md:[&_tr]:border-hairline max-md:[&_tr]:py-2.5 max-md:[&_tr:last-child]:border-b-0 max-md:[&_td]:border-b-0 max-md:[&_td]:p-0 max-md:[&_td]:text-left",
        )}
      >
        {children}
      </table>
    </div>
  );
}
