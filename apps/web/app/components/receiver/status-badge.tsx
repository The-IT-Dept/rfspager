import type { ReactNode } from "react";
import { Badge } from "~/components/ui/badge";
import { cn } from "~/lib/utils";

export type Tone = "ok" | "info" | "warn" | "error" | "muted";

const TONE_CLASS: Record<Tone, string> = {
  ok: "border-emerald-500 text-emerald-700 bg-emerald-50 dark:bg-emerald-950/40 dark:text-emerald-300",
  info: "border-sky-500 text-sky-700 bg-sky-50 dark:bg-sky-950/40 dark:text-sky-300",
  warn: "border-amber-500 text-amber-700 bg-amber-50 dark:bg-amber-950/40 dark:text-amber-300",
  error: "border-red-500 text-red-700 bg-red-50 dark:bg-red-950/40 dark:text-red-300",
  muted: "text-muted-foreground",
};

export function ToneBadge({ tone, className, children }: { tone: Tone; className?: string; children: ReactNode }) {
  return (
    <Badge variant="outline" className={cn(TONE_CLASS[tone], className)}>
      {children}
    </Badge>
  );
}

export function commandTone(status: string): Tone {
  switch (status) {
    case "ok": return "ok";
    case "sent":
    case "progress": return "info";
    case "error": return "error";
    case "expired": return "warn";
    default: return "muted";
  }
}

export function captureTone(status: string): Tone {
  switch (status) {
    case "ready": return "ok";
    case "recording": return "info";
    case "error": return "error";
    default: return "muted";
  }
}

export function levelTone(level: string): Tone {
  switch (level) {
    case "warn":
    case "warning": return "warn";
    case "error":
    case "fatal": return "error";
    default: return "muted";
  }
}

export function severityTone(severity: string): Tone {
  switch (severity) {
    case "warn":
    case "warning": return "warn";
    case "error":
    case "critical": return "error";
    case "ok":
    case "success": return "ok";
    case "info": return "info";
    default: return "muted";
  }
}

export function configStatusTone(status: string): Tone {
  switch (status) {
    case "applied": return "ok";
    case "rejected": return "error";
    default: return "muted";
  }
}

export function CommandStatusBadge({ status }: { status: string }) {
  return <ToneBadge tone={commandTone(status)}>{status || "unknown"}</ToneBadge>;
}

export function CaptureStatusBadge({ status }: { status: string }) {
  return <ToneBadge tone={captureTone(status)}>{status || "unknown"}</ToneBadge>;
}

export function SeverityBadge({ severity }: { severity: string }) {
  return <ToneBadge tone={severityTone(severity)}>{severity || "info"}</ToneBadge>;
}

/** Fixed-width level tag for dense log lines. */
export function LevelTag({ level }: { level: string }) {
  const tone = levelTone(level);
  return (
    <span
      className={cn(
        "inline-block w-11 shrink-0 rounded px-1 text-center text-[10px] font-semibold uppercase leading-4",
        tone === "warn" && "bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
        tone === "error" && "bg-red-100 text-red-800 dark:bg-red-950/60 dark:text-red-300",
        tone === "muted" && (level === "debug" ? "bg-muted text-muted-foreground/70" : "bg-muted text-muted-foreground"),
      )}
    >
      {level || "info"}
    </span>
  );
}
