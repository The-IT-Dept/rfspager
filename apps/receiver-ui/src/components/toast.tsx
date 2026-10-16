import { useSyncExternalStore } from "react";
import { CircleAlert, CircleCheck, X } from "lucide-react";
import { cn } from "../lib/format";

interface ToastItem {
  id: number;
  tone: "ok" | "bad";
  title: string;
  body?: string;
}

let items: ToastItem[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

function dismiss(id: number) {
  items = items.filter((t) => t.id !== id);
  emit();
}

export function toast(tone: ToastItem["tone"], title: string, body?: string) {
  const id = nextId++;
  items = [...items.slice(-3), { id, tone, title, body }];
  emit();
  setTimeout(() => dismiss(id), tone === "bad" ? 8000 : 4500);
}

export function Toaster() {
  const list = useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => items,
  );
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-3 sm:items-end"
    >
      {list.map((t) => (
        <div
          key={t.id}
          role="status"
          className="pointer-events-auto flex w-full max-w-sm animate-toast-in items-start gap-2.5 rounded-lg border border-border bg-white px-3 py-2.5 shadow-lg"
        >
          {t.tone === "ok" ? (
            <CircleCheck className="mt-0.5 size-4 shrink-0 text-green-600" />
          ) : (
            <CircleAlert className="mt-0.5 size-4 shrink-0 text-red-600" />
          )}
          <div className="min-w-0 flex-1">
            <p className={cn("text-[13px] font-semibold")}>{t.title}</p>
            {t.body && <p className="mt-0.5 break-words text-xs text-muted-foreground">{t.body}</p>}
          </div>
          <button
            type="button"
            onClick={() => dismiss(t.id)}
            className="rounded p-0.5 text-muted-foreground hover:text-foreground"
            aria-label="Dismiss"
          >
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}
