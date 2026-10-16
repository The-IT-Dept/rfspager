import { cn, fmtMHz } from "../lib/format";
import type { Page, PageQuality } from "../lib/types";
import { Tag } from "./ui";

interface Segment {
  text: string;
  damaged: boolean;
}

const enc = new TextEncoder();
const dec = new TextDecoder();

/**
 * Splits a message into plain and damaged runs. `damaged` ranges are [start, end)
 * byte offsets into the UTF-8 message (Go string offsets), so non-ASCII text is
 * sliced as bytes rather than UTF-16 code units.
 */
export function splitDamaged(message: string, ranges: [number, number][] | null | undefined): Segment[] {
  if (!ranges || ranges.length === 0) return [{ text: message, damaged: false }];
  const bytes = enc.encode(message);
  const ascii = bytes.length === message.length;
  const slice = (a: number, b: number) => (ascii ? message.slice(a, b) : dec.decode(bytes.subarray(a, b)));
  const sorted = [...ranges]
    .map(([a, b]) => [Math.max(0, a), Math.min(bytes.length, b)] as const)
    .filter(([a, b]) => b > a)
    .sort((x, y) => x[0] - y[0]);
  const out: Segment[] = [];
  let pos = 0;
  for (const [a0, b] of sorted) {
    const a = Math.max(a0, pos);
    if (b <= a) continue;
    if (a > pos) out.push({ text: slice(pos, a), damaged: false });
    out.push({ text: slice(a, b), damaged: true });
    pos = b;
  }
  if (pos < bytes.length) out.push({ text: slice(pos, bytes.length), damaged: false });
  return out;
}

export function PageMessage({ page, className }: { page: Page; className?: string }) {
  const segs = splitDamaged(page.message, page.quality?.damaged);
  if (!page.message) return <span className={cn("italic text-muted-foreground", className)}>(no message)</span>;
  return (
    <span className={cn("break-words", className)}>
      {segs.map((s, i) =>
        s.damaged ? (
          <mark
            key={i}
            className="rounded-sm bg-amber-200/70 px-px text-amber-950 underline decoration-amber-600 decoration-wavy decoration-1 underline-offset-2"
            title="Unrepairable codeword: these characters may be wrong"
          >
            {s.text}
          </mark>
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </span>
  );
}

export type QualityKind = "clean" | "corrected" | "damaged" | "unknown";

export function qualityKind(q: PageQuality | null | undefined): QualityKind {
  if (!q) return "unknown";
  if (q.uncorrectable > 0) return "damaged";
  if (q.corrected > 0) return "corrected";
  return "clean";
}

export function QualityBadge({ quality }: { quality: PageQuality | null | undefined }) {
  const kind = qualityKind(quality);
  if (kind === "unknown" || !quality) return <span className="text-muted-foreground">—</span>;
  const detail = `${quality.codewords} codewords · ${quality.corrected} corrected · ${quality.uncorrectable} unrepairable${quality.truncated ? " · truncated" : ""}`;
  return (
    <span className="inline-flex flex-wrap items-center gap-1" title={detail}>
      {kind === "clean" && (
        <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-green-700">
          <span className="size-1.5 rounded-full bg-green-600" aria-hidden />
          clean
        </span>
      )}
      {kind === "corrected" && (
        <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-sky-700">
          <span className="size-1.5 rounded-full bg-sky-600" aria-hidden />
          {quality.corrected} corrected
        </span>
      )}
      {kind === "damaged" && (
        <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-amber-700">
          <span className="size-1.5 rounded-full bg-amber-500" aria-hidden />
          damaged
        </span>
      )}
      {quality.truncated && <Tag className="bg-amber-50 text-amber-800">truncated</Tag>}
    </span>
  );
}

export function FilteredTag() {
  return (
    <Tag className="bg-zinc-100 text-zinc-500" title="Dropped by the content filter: not forwarded upstream or to webhooks">
      filtered
    </Tag>
  );
}

export function Capcode({ address, className }: { address: string; className?: string }) {
  return <span className={cn("font-mono font-bold text-brand", className)}>{address}</span>;
}

export function channelTitle(p: Pick<Page, "channel" | "freq_hz">): string {
  return p.freq_hz ? `${p.channel} · ${fmtMHz(p.freq_hz)} MHz` : p.channel;
}

/** Same rule as the server's `q`: case-insensitive substring on address or message. */
export function matchesQuery(p: Page, q: string): boolean {
  if (!q) return true;
  const n = q.toLowerCase();
  return p.address.toLowerCase().includes(n) || p.message.toLowerCase().includes(n);
}
