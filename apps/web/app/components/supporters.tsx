import { useEffect, useState } from "react";
import { Link } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Separator } from "~/components/ui/separator";
import { Textarea } from "~/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Trash2, UserPlus } from "lucide-react";
import { api } from "~/lib/api";

// Supporters on the admin system page: people besides the owner who get free
// Pro (the host comp) while this receiver qualifies. The host comp job grants
// it; this card only keeps the list.

export interface Supporter {
  user_id: number;
  name: string;
  email: string;
  note: string;
  admin_email: string | null;
  created_at: string;
  host_comp_since: string | null;
  host_comp_until: string | null;
  subscription_valid: boolean;
  subscription_platform: string | null;
  subscription_expiry: string | null;
}

function fmtDay(s?: string | null): string {
  return s ? new Date(s).toLocaleDateString() : "—";
}

function compBadge(s: Supporter) {
  if (s.host_comp_since && s.host_comp_until && new Date(s.host_comp_until) > new Date()) {
    return <Badge title={`Since ${fmtDay(s.host_comp_since)}, runs to ${fmtDay(s.host_comp_until)} unless renewed`}>Free Pro</Badge>;
  }
  return <Badge variant="outline">Not yet</Badge>;
}

function storeText(s: Supporter): string {
  if (!s.subscription_valid) return "—";
  const platform = s.subscription_platform ? s.subscription_platform[0].toUpperCase() + s.subscription_platform.slice(1) : "Paid";
  return s.subscription_expiry ? `${platform} to ${fmtDay(s.subscription_expiry)}` : platform;
}

export function SupportersCard({ systemId }: { systemId: string }) {
  const [rows, setRows] = useState<Supporter[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  const load = () =>
    api
      .get<{ data: Supporter[] }>(`/systems/${systemId}/supporters`)
      .then((r) => setRows(r.data))
      .catch((e) => setError(e.message));

  useEffect(() => {
    load();
  }, [systemId]);

  const remove = async (s: Supporter) => {
    if (!confirm(`Remove ${s.name || s.email} as a supporter? Their free Pro ends now unless they host or support another receiver.`)) return;
    try {
      await api.delete(`/systems/${systemId}/supporters/${s.user_id}`);
      load();
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>Supporters</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          <UserPlus className="size-4" /> Add supporter
        </Button>
      </CardHeader>
      <CardContent>
        <Separator className="mb-4" />
        <p className="mb-3 text-sm text-muted-foreground">
          People besides the owner who get free Pro while this receiver qualifies (online 90% of three days and
          decoding). It starts within 15 minutes of them being added, and they get their own email about it.
        </p>
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!rows ? null : rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">No supporters.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Who</TableHead>
                  <TableHead>Pro</TableHead>
                  <TableHead>Store</TableHead>
                  <TableHead>Added</TableHead>
                  <TableHead>Note</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((s) => (
                  <TableRow key={s.user_id}>
                    <TableCell>
                      <Link to={`/admin/users/${s.user_id}`} className="hover:underline">
                        {s.name || s.email}
                      </Link>
                      <p className="text-xs text-muted-foreground">{s.email}</p>
                    </TableCell>
                    <TableCell>{compBadge(s)}</TableCell>
                    <TableCell className="whitespace-nowrap text-xs">{storeText(s)}</TableCell>
                    <TableCell className="whitespace-nowrap text-xs">
                      {fmtDay(s.created_at)}
                      {s.admin_email && <p className="text-muted-foreground">{s.admin_email}</p>}
                    </TableCell>
                    <TableCell className="max-w-xs text-xs">{s.note}</TableCell>
                    <TableCell>
                      <Button variant="ghost" size="sm" onClick={() => remove(s)} title="Remove supporter">
                        <Trash2 className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <AddSupporterDialog open={open} onOpenChange={setOpen} systemId={systemId} onDone={load} />
    </Card>
  );
}

function AddSupporterDialog({
  open,
  onOpenChange,
  systemId,
  onDone,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  systemId: string;
  onDone: () => void;
}) {
  const [email, setEmail] = useState("");
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      await api.post(`/systems/${systemId}/supporters`, { email: email.trim(), note: note.trim() });
      setEmail("");
      setNote("");
      onOpenChange(false);
      onDone();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add supporter</DialogTitle>
          <DialogDescription>
            They get free Pro while this receiver qualifies, the same as the owner. If they pay for Pro in a store,
            they can cancel it and keep Pro while it runs.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div>
            <Label htmlFor="supporter-email">Account email</Label>
            <Input
              id="supporter-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="name@example.com"
              className="mt-1.5"
              autoFocus
            />
          </div>
          <div>
            <Label htmlFor="supporter-note">Note</Label>
            <Textarea
              id="supporter-note"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Why, e.g. co-funded the receiver"
              maxLength={1000}
              className="mt-1.5"
            />
          </div>
          {error && <p className="text-sm text-destructive break-all">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={!email.trim() || !note.trim() || saving}>
              {saving ? "Adding…" : "Add supporter"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
