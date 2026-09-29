import { Link } from "react-router";

import { BrandLockup } from "~/components/brand";

const heading = "font-[family-name:var(--font-heading)]";

/**
 * The frame every sign-in style screen shares: the mark, a one-line title, a
 * white card on the grey canvas, and a single link out underneath.
 */
export function AuthShell({
  title,
  intro,
  children,
  footer,
}: {
  title: string;
  intro?: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <div className="flex flex-1 items-start justify-center bg-gray-50 px-4 py-12 sm:items-center sm:py-16">
      <div className="w-full max-w-[400px]">
        <Link to="/" className="mx-auto mb-8 block w-fit">
          <BrandLockup markClassName="h-9 w-auto text-[#ff1003]" wordmarkClassName="h-7 w-auto" />
        </Link>
        <h1 className={`${heading} text-center text-2xl font-extrabold tracking-tight text-gray-900`}>{title}</h1>
        {intro && <p className="mt-2 text-center text-sm leading-relaxed text-gray-500">{intro}</p>}
        <div className="mt-8 rounded-xl border border-gray-200 bg-white p-6 sm:p-8">{children}</div>
        {footer && <p className="mt-6 text-center text-sm text-gray-500">{footer}</p>}
      </div>
    </div>
  );
}

export function Field({
  id,
  label,
  hint,
  trailing,
  ...input
}: React.InputHTMLAttributes<HTMLInputElement> & {
  id: string;
  label: string;
  hint?: string;
  trailing?: React.ReactNode;
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-baseline justify-between">
        <label htmlFor={id} className="text-sm font-semibold text-gray-800">
          {label}
        </label>
        {trailing}
      </div>
      <input
        id={id}
        {...input}
        className="h-10 w-full rounded-lg border border-gray-200 bg-gray-50 px-3 text-sm text-gray-900 outline-none transition-colors placeholder:text-gray-400 focus:border-[#F7241D] focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15"
      />
      {hint && <p className="mt-1.5 text-xs text-gray-500">{hint}</p>}
    </div>
  );
}

export function SubmitButton({ children, busy, busyLabel }: { children: React.ReactNode; busy: boolean; busyLabel: string }) {
  return (
    <button
      type="submit"
      disabled={busy}
      className="h-10 w-full rounded-lg bg-[#F7241D] text-sm font-semibold text-white outline-none transition-colors hover:bg-[#d91f19] focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 focus-visible:ring-offset-2 disabled:opacity-60"
    >
      {busy ? busyLabel : children}
    </button>
  );
}

export function Notice({ tone, children }: { tone: "error" | "success"; children: React.ReactNode }) {
  return (
    <p
      role={tone === "error" ? "alert" : "status"}
      className={`rounded-lg px-3.5 py-2.5 text-sm ${
        tone === "error" ? "bg-red-50 text-red-800" : "bg-emerald-50 text-emerald-800"
      }`}
    >
      {children}
    </p>
  );
}

export const linkClass = "font-semibold text-[#F7241D] hover:underline";

/**
 * Only ever follow same-origin paths from `?next=`; anything else goes home.
 *
 * Resolve against the current origin rather than string-matching: the router
 * and the URL parser both treat a leading backslash as a slash, so
 * `"/\\evil.com"` is a *different origin* even though it starts with one slash
 * and contains no scheme separator. Resolving also hands the router the exact
 * path it will use, so it never rejects a value this function accepted.
 */
export function safeNext(raw: string | null, fallback: string): string {
  if (!raw) return fallback;
  try {
    const url = new URL(raw, window.location.href);
    if (url.origin !== window.location.origin) return fallback;
    return url.pathname + url.search + url.hash;
  } catch {
    return fallback;
  }
}
