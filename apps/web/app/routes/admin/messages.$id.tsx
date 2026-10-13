import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table";
import { ArrowLeft, Check, Copy, ExternalLink, Pencil, Trash2 } from "lucide-react";
import { api } from "~/lib/api";

// The receiver's decode assessment stored with the kept copy of a page
// (messages.quality, see internal/rxproto PageQuality).
interface Quality {
  codewords?: number;
  corrected?: number;
  uncorrectable?: number;
  truncated?: boolean;
  damaged?: [number, number][];
}

interface MessageDetail {
  id: number;
  system_id?: number | null;
  system_name?: string | null;
  capcode_id?: number | null;
  capcode_alias?: string | null;
  capcode_address?: string | null;
  agency_name?: string | null;
  address: string;
  message: string;
  md5?: string | null;
  quality?: Quality | string | null;
  incident_key?: string | null;
  replaced_count?: number | null;
  lat?: number | string | null;
  lng?: number | string | null;
  fanned_out_at?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
}

interface CopyRow {
  id: number;
  system_id?: number | null;
  system_name?: string | null;
  ts?: string;
  received_at?: string;
  channel?: string | null;
  message?: string | null;
  corrected?: number | null;
  uncorrectable?: number | null;
  truncated?: boolean;
  kept?: boolean;
}

interface IncidentRow {
  id: number;
  address: string;
  capcode_alias?: string | null;
  system_name?: string | null;
  created_at?: string;
  replaced_count?: number | null;
  quality?: Quality | string | null;
  message: string;
  notifications_count?: number;
  notifications_pushed_count?: number;
}

interface NotificationRow {
  id: string;
  user_id?: number | null;
  user_email?: string | null;
  user_name?: string | null;
  created_at?: string;
  pushed_at?: string | null;
  push_failed_at?: string | null;
  push_attempts?: number | null;
  push_required?: boolean;
  push_last_error?: string | null;
  read_at?: string | null;
  pushed_message?: string | null;
}

interface Notifications {
  data: NotificationRow[];
  total: number;
  pushed: number;
  failed: number;
}

const NOTIFICATION_LIMIT = 200;

function fmtDate(v?: string | null) {
  if (!v) return "—";
  return new Date(v).toLocaleString();
}

function parseQuality(q: Quality | string | null | undefined): Quality | null {
  if (!q) return null;
  if (typeof q === "string") {
    try {
      return JSON.parse(q) as Quality;
    } catch {
      return null;
    }
  }
  return q;
}

function Field({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className={`mt-1 text-sm break-all ${mono ? "font-mono" : ""}`}>{children}</p>
    </div>
  );
}

function CopyValue({ value }: { value?: string | null }) {
  const [copied, setCopied] = useState(false);
  if (!value) return <span className="text-muted-foreground">—</span>;
  return (
    <span className="inline-flex items-center gap-2">
      <span className="font-mono text-xs break-all">{value}</span>
      <button
        type="button"
        onClick={() => {
          navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        }}
        className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
        title="Copy"
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </span>
  );
}

// Clean / corrected / unrepairable / truncated / replaced, in the colours
// the receiver pages use.
function QualityBadges({ quality, replaced }: { quality: Quality | null; replaced?: number | null }) {
  const badges: React.ReactNode[] = [];
  if (!quality) {
    badges.push(<Badge key="none" variant="outline">no quality data</Badge>);
  } else {
    const corrected = quality.corrected ?? 0;
    const bad = quality.uncorrectable ?? 0;
    if (corrected === 0 && bad === 0 && !quality.truncated) {
      badges.push(<Badge key="clean" variant="secondary">clean</Badge>);
    }
    if (corrected > 0) badges.push(<Badge key="corr" variant="outline">{corrected} corrected</Badge>);
    if (bad > 0) badges.push(<Badge key="bad" variant="destructive">{bad} unrepairable</Badge>);
    if (quality.truncated) badges.push(<Badge key="trunc" variant="destructive">truncated</Badge>);
  }
  if (replaced && replaced > 0) {
    badges.push(
      <Badge key="repl" variant="outline" title="worse copies of this page replaced by this one">
        replaced ×{replaced}
      </Badge>,
    );
  }
  return <span className="inline-flex flex-wrap gap-1">{badges}</span>;
}

// The stored text with the byte ranges the decoder could not repair marked.
// Pager text is ASCII, so byte offsets are character offsets.
function DamagedText({ text, ranges }: { text: string; ranges?: [number, number][] }) {
  const sorted = (ranges ?? [])
    .filter((r) => Array.isArray(r) && r.length === 2 && r[0] < r[1])
    .sort((a, b) => a[0] - b[0]);
  if (sorted.length === 0) return <>{text}</>;
  const parts: React.ReactNode[] = [];
  let pos = 0;
  sorted.forEach(([start, end], i) => {
    const s = Math.max(start, pos);
    const e = Math.min(end, text.length);
    if (s >= e) return;
    if (s > pos) parts.push(<span key={`t${i}`}>{text.slice(pos, s)}</span>);
    parts.push(
      <mark key={`d${i}`} className="rounded bg-destructive/20 px-0.5 text-inherit" title="unrepairable codeword">
        {text.slice(s, e)}
      </mark>,
    );
    pos = e;
  });
  if (pos < text.length) parts.push(<span key="tail">{text.slice(pos)}</span>);
  return <>{parts}</>;
}

function NotifCount({ pushed, total }: { pushed?: number; total?: number }) {
  const t = Number(total ?? 0);
  const p = Number(pushed ?? 0);
  if (t === 0) return <span className="text-muted-foreground">0</span>;
  if (p === t) return <Badge variant="secondary">{t}</Badge>;
  return <span className="font-mono text-xs" title={`${p} pushed / ${t} total`}>{p}/{t}</span>;
}

function Mono({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-xs whitespace-pre-wrap break-words">{children}</span>;
}

function clip(s: string, n = 100) {
  return s.length > n ? s.slice(0, n) + "…" : s;
}

export default function MessageDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [msg, setMsg] = useState<MessageDetail | null>(null);
  const [copies, setCopies] = useState<CopyRow[]>([]);
  const [incident, setIncident] = useState<IncidentRow[]>([]);
  const [notifs, setNotifs] = useState<Notifications | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    Promise.all([
      api.get<MessageDetail>(`/messages/${id}`),
      api.get<{ data: CopyRow[] }>(`/messages/${id}/copies`),
      api.get<{ data: IncidentRow[] }>(`/messages/${id}/incident`),
      api.get<Notifications>(`/messages/${id}/notifications?limit=${NOTIFICATION_LIMIT}`),
    ])
      .then(([m, c, i, n]) => {
        setMsg(m);
        setCopies(c.data || []);
        setIncident(i.data || []);
        setNotifs(n);
      })
      .catch((e) => setError(e.message));
  }, [id]);

  const onDelete = async () => {
    if (!msg) return;
    if (!confirm("Delete this message? It is soft-deleted and disappears from the feed.")) return;
    await api.delete(`/messages/${msg.id}`);
    navigate("/admin/messages");
  };

  if (error) return <p className="text-destructive">{error}</p>;
  if (!msg) return <p className="text-muted-foreground">Loading...</p>;

  const quality = parseQuality(msg.quality);
  const hasLocation = msg.lat != null && msg.lng != null && msg.lat !== "" && msg.lng !== "";
  const capcodeLabel = msg.capcode_alias ? `${msg.capcode_alias} (${msg.address})` : `${msg.address} (unknown capcode)`;

  return (
    <div className="space-y-6 min-w-0">
      <div className="flex items-center gap-3">
        <Link to="/admin/messages">
          <Button variant="ghost" size="sm"><ArrowLeft className="size-4" /> Back to Messages</Button>
        </Link>
      </div>

      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div className="min-w-0">
          <h1 className="text-2xl font-bold flex items-center gap-2 flex-wrap">
            Message #{msg.id}
            <QualityBadges quality={quality} replaced={msg.replaced_count} />
            {msg.deleted_at && <Badge variant="destructive">deleted</Badge>}
          </h1>
          <p className="text-sm text-muted-foreground">
            {msg.system_name ?? `system ${msg.system_id ?? "?"}`} · {capcodeLabel}
            {msg.agency_name ? ` · ${msg.agency_name}` : ""} · {fmtDate(msg.created_at)}
          </p>
        </div>
        <div className="flex gap-2">
          <Link to={`/admin/messages/${msg.id}/edit`}>
            <Button variant="outline"><Pencil className="size-4" /> Edit</Button>
          </Link>
          <Button variant="outline" className="text-destructive" onClick={onDelete}>
            <Trash2 className="size-4" /> Delete
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader><CardTitle>Message</CardTitle></CardHeader>
        <CardContent className="space-y-6">
          <pre className="whitespace-pre-wrap break-all rounded-md border bg-muted/40 p-3 font-mono text-sm">
            <DamagedText text={msg.message} ranges={quality?.damaged} />
          </pre>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <Field label="System">
              {msg.system_id ? (
                <Link to={`/admin/systems/${msg.system_id}`} className="hover:underline">{msg.system_name ?? msg.system_id}</Link>
              ) : "—"}
            </Field>
            <Field label="Capcode">
              {msg.capcode_id ? (
                <Link to={`/admin/capcodes/${msg.capcode_id}/edit`} className="hover:underline">{capcodeLabel}</Link>
              ) : capcodeLabel}
            </Field>
            <Field label="Agency">{msg.agency_name ?? "—"}</Field>
            <Field label="Location">
              {hasLocation ? (
                <a
                  href={`https://www.google.com/maps?q=${msg.lat},${msg.lng}`}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 hover:underline"
                >
                  <span className="font-mono text-xs">{msg.lat}, {msg.lng}</span>
                  <ExternalLink className="size-3" />
                </a>
              ) : "—"}
            </Field>
            <Field label="Created">{fmtDate(msg.created_at)}</Field>
            <Field label="Fanned out">{fmtDate(msg.fanned_out_at)}</Field>
            <Field label="Updated">{fmtDate(msg.updated_at)}</Field>
            <Field label="Deleted">{fmtDate(msg.deleted_at)}</Field>
            <Field label="Incident key"><CopyValue value={msg.incident_key} /></Field>
            <Field label="MD5"><CopyValue value={msg.md5} /></Field>
            <Field label="Replaced copies">{msg.replaced_count ?? 0}</Field>
            <Field label="Decode quality">
              {quality
                ? `${quality.codewords ?? "?"} codewords, ${quality.corrected ?? 0} corrected, ${quality.uncorrectable ?? 0} unrepairable${quality.truncated ? ", truncated" : ""}`
                : "none recorded (stored before 5.6)"}
            </Field>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Receiver copies ({copies.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {copies.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No receiver page logs within five minutes of this message. Only proto-2 receivers ship page logs, and older rows predate them.
            </p>
          ) : (
            <div className="rounded-md border overflow-x-auto">
              <Table className="min-w-[1100px]">
                <TableHeader>
                  <TableRow>
                    <TableHead>Receiver</TableHead>
                    <TableHead>Heard</TableHead>
                    <TableHead>Channel</TableHead>
                    <TableHead className="text-right">Corrected</TableHead>
                    <TableHead className="text-right">Unrepairable</TableHead>
                    <TableHead>Flags</TableHead>
                    <TableHead className="min-w-[28rem]">Text</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {copies.map((c) => (
                    <TableRow key={c.id} className={c.kept ? "bg-muted/40" : undefined}>
                      <TableCell>
                        {c.system_id ? (
                          <Link to={`/admin/systems/${c.system_id}`} className="hover:underline">{c.system_name ?? c.system_id}</Link>
                        ) : "—"}
                      </TableCell>
                      <TableCell className="whitespace-nowrap">{fmtDate(c.ts)}</TableCell>
                      <TableCell>{c.channel ?? "—"}</TableCell>
                      <TableCell className="text-right font-mono text-xs">{c.corrected ?? "—"}</TableCell>
                      <TableCell className="text-right font-mono text-xs">{c.uncorrectable ?? "—"}</TableCell>
                      <TableCell>
                        <span className="inline-flex gap-1">
                          {c.kept && <Badge variant="secondary">kept</Badge>}
                          {c.truncated && <Badge variant="destructive">truncated</Badge>}
                        </span>
                      </TableCell>
                      <TableCell><Mono>{c.message ?? ""}</Mono></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            Same incident ({incident.length})
            {msg.incident_key && <span className="ml-2 font-mono text-xs font-normal text-muted-foreground">{msg.incident_key}</span>}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {!msg.incident_key ? (
            <p className="text-sm text-muted-foreground">No incident key on this message, so it is never grouped for push suppression.</p>
          ) : incident.length === 0 ? (
            <p className="text-sm text-muted-foreground">No other messages with this incident key within 15 minutes.</p>
          ) : (
            <div className="rounded-md border overflow-x-auto">
              <Table className="min-w-[1100px]">
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>Capcode</TableHead>
                    <TableHead>System</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead>Quality</TableHead>
                    <TableHead>Notifs</TableHead>
                    <TableHead className="min-w-[28rem]">Message</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {incident.map((r) => (
                    <TableRow key={r.id}>
                      <TableCell>
                        <Link to={`/admin/messages/${r.id}`} className="font-mono hover:underline">{r.id}</Link>
                      </TableCell>
                      <TableCell>{r.capcode_alias ? `${r.capcode_alias} (${r.address})` : r.address}</TableCell>
                      <TableCell>{r.system_name ?? "—"}</TableCell>
                      <TableCell className="whitespace-nowrap">{fmtDate(r.created_at)}</TableCell>
                      <TableCell><QualityBadges quality={parseQuality(r.quality)} replaced={r.replaced_count} /></TableCell>
                      <TableCell><NotifCount pushed={r.notifications_pushed_count} total={r.notifications_count} /></TableCell>
                      <TableCell><Mono>{clip(r.message)}</Mono></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            Notifications
            {notifs && (
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                {notifs.pushed} pushed / {notifs.total} total{notifs.failed > 0 ? `, ${notifs.failed} failed` : ""}
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {!notifs || notifs.total === 0 ? (
            <p className="text-sm text-muted-foreground">Nobody was notified for this message.</p>
          ) : (
            <div className="space-y-2">
              {notifs.total > notifs.data.length && (
                <p className="text-xs text-muted-foreground">Showing the newest {notifs.data.length} of {notifs.total}.</p>
              )}
              <div className="rounded-md border overflow-x-auto">
                <Table className="min-w-[1100px]">
                  <TableHeader>
                    <TableRow>
                      <TableHead>User</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>Pushed</TableHead>
                      <TableHead>Read</TableHead>
                      <TableHead className="min-w-[28rem]">Pushed text</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {notifs.data.map((n) => {
                      const pushedText = n.pushed_message ?? null;
                      const differs = pushedText != null && pushedText.trim() !== msg.message.trim();
                      return (
                        <TableRow key={n.id}>
                          <TableCell>
                            {n.user_id ? (
                              <Link to={`/admin/users/${n.user_id}`} className="hover:underline">
                                {n.user_email ?? n.user_name ?? `user ${n.user_id}`}
                              </Link>
                            ) : "—"}
                          </TableCell>
                          <TableCell className="whitespace-nowrap">{fmtDate(n.created_at)}</TableCell>
                          <TableCell className="whitespace-nowrap">
                            {n.pushed_at ? (
                              fmtDate(n.pushed_at)
                            ) : n.push_failed_at ? (
                              <span className="text-destructive" title={n.push_last_error ?? undefined}>
                                failed after {n.push_attempts ?? 0} {n.push_attempts === 1 ? "attempt" : "attempts"}
                              </span>
                            ) : n.push_required ? (
                              <span className="text-muted-foreground">pending</span>
                            ) : (
                              <span className="text-muted-foreground">not required</span>
                            )}
                          </TableCell>
                          <TableCell className="whitespace-nowrap">{fmtDate(n.read_at)}</TableCell>
                          <TableCell>
                            {differs && pushedText ? (
                              <span title="the text this push carried; a cleaner copy of the page replaced the stored text afterwards">
                                <Badge variant="outline" className="mr-1">differs</Badge>
                                <Mono>{clip(pushedText)}</Mono>
                              </span>
                            ) : (
                              <span className="text-muted-foreground">same as stored</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
