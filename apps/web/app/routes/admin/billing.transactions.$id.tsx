import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Separator } from "~/components/ui/separator";
import { ArrowLeft } from "lucide-react";
import { api } from "~/lib/api";

interface Txn {
  id: number;
  user_id: number | null;
  user_name?: string | null;
  user_email?: string | null;
  platform: string;
  transaction_id: string | null;
  original_transaction_id: string | null;
  product_id: string;
  event_type: string;
  is_valid: boolean;
  expires_at: string | null;
  will_renew: boolean | null;
  environment: string | null;
  raw_payload: string | null;
  created_at: string;
}

function base64UrlDecode(s: string): string | null {
  try {
    const padded = s.replace(/-/g, "+").replace(/_/g, "/");
    return atob(padded + "=".repeat((4 - (padded.length % 4)) % 4));
  } catch {
    return null;
  }
}

// Apple JWS is "header.payload.signature" with each part base64url-encoded.
function tryDecodeAppleJWS(payload: string): string | null {
  const parts = payload.split(".");
  if (parts.length !== 3) return null;
  const decoded = base64UrlDecode(parts[1]);
  if (!decoded) return null;
  try {
    return JSON.stringify(JSON.parse(decoded), null, 2);
  } catch {
    return decoded;
  }
}

function prettyPayload(raw: string | null): { content: string; format: string } {
  if (!raw) return { content: "(empty)", format: "" };
  // Try plain JSON first
  try {
    return { content: JSON.stringify(JSON.parse(raw), null, 2), format: "JSON" };
  } catch {
    // continue
  }
  // Try Apple JWS
  const jws = tryDecodeAppleJWS(raw);
  if (jws) return { content: jws, format: "Apple JWS (decoded)" };
  return { content: raw, format: "raw" };
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm font-mono break-all">{children}</p>
    </div>
  );
}

export default function TransactionDetailPage() {
  const { id } = useParams();
  const [txn, setTxn] = useState<Txn | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.get<Txn>(`/billing/transactions/${id}`)
      .then(setTxn)
      .catch((e) => setError(e.message));
  }, [id]);

  if (error) return <p className="text-destructive">{error}</p>;
  if (!txn) return <p className="text-muted-foreground">Loading...</p>;

  const payload = prettyPayload(txn.raw_payload);

  return (
    <div className="space-y-6 min-w-0">
      <div className="flex items-center gap-3">
        <Link to="/admin/billing">
          <Button variant="ghost" size="sm"><ArrowLeft className="size-4" /> Back to Billing</Button>
        </Link>
      </div>

      <div>
        <h1 className="text-2xl font-bold">Transaction #{txn.id}</h1>
        <p className="text-sm text-muted-foreground">
          {new Date(txn.created_at).toLocaleString()}
        </p>
      </div>

      <Card>
        <CardHeader><CardTitle>Overview</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          <Field label="Platform"><Badge variant="secondary">{txn.platform}</Badge></Field>
          <Field label="Event Type"><Badge>{txn.event_type}</Badge></Field>
          <Field label="Valid">{txn.is_valid ? <Badge>Yes</Badge> : <Badge variant="outline">No</Badge>}</Field>
          <Field label="Product">{txn.product_id || "—"}</Field>
          <Field label="Environment">{txn.environment || "—"}</Field>
          <Field label="Will Renew">{txn.will_renew == null ? "—" : (txn.will_renew ? "Yes" : "No")}</Field>
          <Field label="Expires At">{txn.expires_at ? new Date(txn.expires_at).toLocaleString() : "—"}</Field>
          <Field label="Transaction ID">{txn.transaction_id || "—"}</Field>
          <Field label="Original Transaction ID">{txn.original_transaction_id || "—"}</Field>
          <Field label="User">
            {txn.user_id ? (
              <Link to={`/admin/users/${txn.user_id}`} className="text-primary hover:underline">
                {txn.user_email || txn.user_name || `#${txn.user_id}`}
              </Link>
            ) : "—"}
          </Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Raw payload</CardTitle>
          {payload.format && <Badge variant="outline">{payload.format}</Badge>}
        </CardHeader>
        <CardContent>
          <Separator className="mb-4" />
          <pre className="text-xs font-mono whitespace-pre-wrap break-all bg-muted p-4 rounded-md max-h-[600px] overflow-auto">
            {payload.content}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
