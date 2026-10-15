import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Separator } from "~/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "~/components/ui/table";
import { Pencil, ArrowLeft, ShieldCheck, Copy, Check } from "lucide-react";
import { api } from "~/lib/api";
import { FreeTimeCard } from "~/components/free-time";

interface UserDetail {
  id: number;
  user_id?: string | null; // UUID, used as Adapty customer_user_id + mobile client identifier
  name: string;
  email: string;
  email_verified_at?: string | null;
  superuser?: boolean;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string | null;
  subscription_valid?: boolean;
  subscription_product_id?: string | null;
  subscription_expiry?: string | null;
  subscription_platform?: string | null;
  will_renew?: boolean | null;
  cancellation_reason?: string | null;
  last_store_sync_at?: string | null;
  apple_original_transaction_id?: string | null;
  google_purchase_token?: string | null;
  stripe_id?: string | null;
  stripe_subscription_id?: string | null;
  stripe_subscription_status?: string | null;
  stripe_subscription_start_date?: string | null;
  stripe_subscription_end_date?: string | null;
  adapty_id?: string | null;
  last_adapty_sync_at?: string | null;
  free_time_days?: number | null; // total given with "Give free time"
  free_time_last_at?: string | null;
}

interface Installation {
  id: string;
  vendor?: string;
  model?: string;
  version?: string;
  app_version?: string;
  push_type?: string;
  timezone?: string;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string | null;
}

interface TxnRow {
  id: number;
  created_at: string;
  platform: string;
  event_type: string;
  product_id: string;
  is_valid: boolean;
  expires_at: string | null;
  transaction_id: string | null;
}

function fmtDate(s?: string | null): string {
  if (!s) return "—";
  return new Date(s).toLocaleString();
}

function Field({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className={`mt-1 text-sm break-all ${mono ? "font-mono" : ""}`}>{children}</p>
    </div>
  );
}

// CopyValue renders a monospace value with a small copy-to-clipboard button.
// Falls back to plain text on null/empty.
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

const platformBadge = (v?: string | null) => {
  if (!v) return <Badge variant="outline">none</Badge>;
  if (v === "apple") return <Badge variant="secondary">Apple</Badge>;
  if (v === "google") return <Badge variant="secondary">Google</Badge>;
  if (v === "stripe") return <Badge variant="secondary">Stripe</Badge>;
  return <Badge variant="outline">{v}</Badge>;
};

export default function UserDetailPage() {
  const { id } = useParams();
  const [user, setUser] = useState<UserDetail | null>(null);
  const [installations, setInstallations] = useState<Installation[]>([]);
  const [txns, setTxns] = useState<TxnRow[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    if (!id) return;
    Promise.all([
      api.get<UserDetail>(`/users/${id}`),
      api.get<{ data: Installation[] }>(`/users/${id}/installations`),
      api.get<{ data: TxnRow[] }>(`/users/${id}/iap-transactions?per_page=20`),
    ])
      .then(([u, devs, tx]) => {
        setUser(u);
        setInstallations(devs.data || []);
        setTxns(tx.data || []);
      })
      .catch((e) => setError(e.message));
  };

  useEffect(load, [id]);

  if (error) return <p className="text-destructive">{error}</p>;
  if (!user) return <p className="text-muted-foreground">Loading...</p>;

  return (
    <div className="space-y-6 min-w-0">
      <div className="flex items-center gap-3">
        <Link to="/admin/users">
          <Button variant="ghost" size="sm"><ArrowLeft className="size-4" /> Back to Users</Button>
        </Link>
      </div>

      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-2">
            {user.name}
            {user.superuser && <Badge variant="destructive" className="gap-1"><ShieldCheck className="size-3" /> Admin</Badge>}
          </h1>
          <p className="text-sm text-muted-foreground">{user.email}</p>
        </div>
        <Link to={`/admin/users/${user.id}/edit`}>
          <Button variant="outline"><Pencil className="size-4" /> Edit</Button>
        </Link>
      </div>

      <Card>
        <CardHeader><CardTitle>Identifiers</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Field label="Internal ID"><CopyValue value={String(user.id)} /></Field>
          <Field label="User ID (UUID)"><CopyValue value={user.user_id} /></Field>
          <Field label="Email"><CopyValue value={user.email} /></Field>
          <Field label="Email Verified">{fmtDate(user.email_verified_at)}</Field>
          <Field label="Adapty Profile ID"><CopyValue value={user.adapty_id} /></Field>
          <Field label="Last Adapty Sync">{fmtDate(user.last_adapty_sync_at)}</Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Subscription</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          <Field label="Status">
            {user.subscription_valid
              ? <Badge>Pro</Badge>
              : <Badge variant="outline">Free</Badge>}
          </Field>
          <Field label="Platform">{platformBadge(user.subscription_platform)}</Field>
          <Field label="Product">{user.subscription_product_id || "—"}</Field>
          <Field label="Expires">{fmtDate(user.subscription_expiry)}</Field>
          <Field label="Will Renew">
            {user.will_renew == null ? "—" : (user.will_renew ? "Yes" : <span className="text-amber-600">No</span>)}
          </Field>
          <Field label="Last Store Sync">{fmtDate(user.last_store_sync_at)}</Field>
          {!!user.free_time_days && (
            <Field label="Free Time Given">
              <Badge variant="secondary">+{user.free_time_days}d</Badge>{" "}
              <span className="text-muted-foreground">last {fmtDate(user.free_time_last_at)}</span>
            </Field>
          )}
          {user.cancellation_reason && (
            <Field label="Cancellation Reason">
              <Badge variant="destructive">{user.cancellation_reason}</Badge>
            </Field>
          )}
        </CardContent>
      </Card>

      <FreeTimeCard userId={user.id} onGranted={load} />

      <Card>
        <CardHeader><CardTitle>Store Identifiers</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Field label="Apple Original Transaction ID"><CopyValue value={user.apple_original_transaction_id} /></Field>
          <Field label="Google Purchase Token"><CopyValue value={user.google_purchase_token} /></Field>
          <Field label="Stripe Customer ID"><CopyValue value={user.stripe_id} /></Field>
          <Field label="Stripe Subscription ID"><CopyValue value={user.stripe_subscription_id} /></Field>
          {user.stripe_subscription_status && (
            <>
              <Field label="Stripe Status">
                <Badge variant="secondary">{user.stripe_subscription_status}</Badge>
              </Field>
              <Field label="Stripe Period">
                {fmtDate(user.stripe_subscription_start_date)} → {fmtDate(user.stripe_subscription_end_date)}
              </Field>
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Recent IAP Transactions</CardTitle>
          <span className="text-xs text-muted-foreground">{txns.length} most recent</span>
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          {txns.length === 0 ? (
            <p className="text-sm text-muted-foreground">No transactions yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>When</TableHead>
                    <TableHead>Platform</TableHead>
                    <TableHead>Event</TableHead>
                    <TableHead>Product</TableHead>
                    <TableHead>Valid</TableHead>
                    <TableHead>Expires</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {txns.map((t) => (
                    <TableRow key={t.id} className="cursor-pointer hover:bg-muted/50">
                      <TableCell>
                        <Link to={`/admin/billing/transactions/${t.id}`} className="hover:underline">
                          {fmtDate(t.created_at)}
                        </Link>
                      </TableCell>
                      <TableCell>{platformBadge(t.platform)}</TableCell>
                      <TableCell><Badge variant="outline">{t.event_type}</Badge></TableCell>
                      <TableCell className="font-mono text-xs">{t.product_id || "—"}</TableCell>
                      <TableCell>{t.is_valid ? <Badge>Yes</Badge> : <Badge variant="outline">No</Badge>}</TableCell>
                      <TableCell>{t.expires_at ? new Date(t.expires_at).toLocaleDateString() : "—"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Devices</CardTitle>
          <span className="text-xs text-muted-foreground">{installations.length} total</span>
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          {installations.length === 0 ? (
            <p className="text-sm text-muted-foreground">No installations.</p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Device</TableHead>
                    <TableHead>OS</TableHead>
                    <TableHead>App</TableHead>
                    <TableHead>Push</TableHead>
                    <TableHead>Last seen</TableHead>
                    <TableHead>Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {installations.map((i) => (
                    <TableRow key={i.id}>
                      <TableCell>{[i.vendor, i.model].filter(Boolean).join(" ") || "—"}</TableCell>
                      <TableCell>{i.version || "—"}</TableCell>
                      <TableCell>{i.app_version || "—"}</TableCell>
                      <TableCell>{i.push_type || "—"}</TableCell>
                      <TableCell>{fmtDate(i.updated_at)}</TableCell>
                      <TableCell>
                        {i.deleted_at
                          ? <Badge variant="outline">Removed</Badge>
                          : <Badge>Active</Badge>}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Account</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field label="Created">{fmtDate(user.created_at)}</Field>
          <Field label="Updated">{fmtDate(user.updated_at)}</Field>
          {user.deleted_at && (
            <Field label="Deleted">
              <Badge variant="destructive">{fmtDate(user.deleted_at)}</Badge>
            </Field>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
