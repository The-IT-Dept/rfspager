import { useEffect, useState } from "react";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Gift } from "lucide-react";
import { api } from "~/lib/api";

// "Give free time" on the admin user page: an Apple renewal extension, a
// Google Play deferral, or a gift, chosen by the server from the account.

export interface AccountExtension {
  id: number;
  admin_email: string | null;
  method: "apple" | "google" | "gift";
  status: "applied" | "partial" | "failed";
  days_requested: number;
  days_applied: number;
  reason_code: number | null;
  note: string;
  expiry_before: string | null;
  expiry_after: string | null;
  error: string | null;
  created_at: string;
}

interface ExtensionsResponse {
  data: AccountExtension[];
  method: "apple" | "google" | "gift" | "";
  max_days: number;
  apple_extensions_left: number;
  unavailable?: string;
}

const METHOD_LABEL: Record<string, string> = {
  apple: "Apple renewal extension",
  google: "Google Play deferral",
  gift: "Gift",
};

const METHOD_HELP: Record<string, string> = {
  apple:
    "Apple pushes the next renewal back, so they aren't charged until the free time runs out. Up to 90 days per extension, two a year.",
  google: "Google Play pushes the next renewal back, so they aren't charged until the free time runs out.",
  gift: "No active store subscription, so they get the gifted plan in our own records. It lapses on its own when the time runs out.",
};

const REASONS = [
  { value: "3", label: "Service issue or outage" },
  { value: "1", label: "Customer satisfaction" },
  { value: "2", label: "Other" },
  { value: "0", label: "Undeclared" },
];

const PRESETS = [30, 90, 180];

function fmtDay(s?: string | null): string {
  return s ? new Date(s).toLocaleDateString() : "—";
}

function methodBadge(m: string) {
  if (m === "apple") return <Badge variant="secondary">Apple</Badge>;
  if (m === "google") return <Badge variant="secondary">Google</Badge>;
  return <Badge variant="outline">Gift</Badge>;
}

function statusBadge(e: AccountExtension) {
  if (e.status === "applied") return <Badge>Applied</Badge>;
  if (e.status === "partial") return <Badge variant="destructive">Partial</Badge>;
  return <Badge variant="destructive">Failed</Badge>;
}

export function FreeTimeCard({ userId, onGranted }: { userId: number; onGranted: () => void }) {
  const [info, setInfo] = useState<ExtensionsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  const load = () =>
    api
      .get<ExtensionsResponse>(`/users/${userId}/extensions`)
      .then(setInfo)
      .catch((e) => setError(e.message));

  useEffect(() => {
    load();
  }, [userId]);

  const canGive = !!info && !info.unavailable && info.max_days > 0;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>Free time</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)} disabled={!canGive}>
          <Gift className="size-4" /> Give free time
        </Button>
      </CardHeader>
      <CardContent>
        <Separator className="mb-4" />
        {error && <p className="text-sm text-destructive">{error}</p>}
        {info?.unavailable && (
          <p className="mb-3 text-sm text-muted-foreground">Can't give free time: {info.unavailable}.</p>
        )}
        {info?.method === "apple" && !info.unavailable && info.max_days === 0 && (
          <p className="mb-3 text-sm text-muted-foreground">
            Both Apple extensions for the year are used. Apple allows two per subscription in any 365 days.
          </p>
        )}
        {!info ? null : info.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">No free time given yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Days</TableHead>
                  <TableHead>Expiry</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>By</TableHead>
                  <TableHead>Note</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {info.data.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell>{new Date(e.created_at).toLocaleString()}</TableCell>
                    <TableCell>{methodBadge(e.method)}</TableCell>
                    <TableCell>
                      {e.days_applied === e.days_requested ? e.days_applied : `${e.days_applied} of ${e.days_requested}`}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      {fmtDay(e.expiry_before)} → {fmtDay(e.expiry_after)}
                    </TableCell>
                    <TableCell title={e.error ?? undefined}>{statusBadge(e)}</TableCell>
                    <TableCell className="text-xs">{e.admin_email ?? "—"}</TableCell>
                    <TableCell className="max-w-xs text-xs">
                      {e.note}
                      {e.error && <p className="mt-1 text-destructive break-all">{e.error}</p>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      {info && canGive && (
        <GiveFreeTimeDialog
          open={open}
          onOpenChange={setOpen}
          userId={userId}
          info={info}
          onDone={() => {
            load();
            onGranted();
          }}
        />
      )}
    </Card>
  );
}

function GiveFreeTimeDialog({
  open,
  onOpenChange,
  userId,
  info,
  onDone,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userId: number;
  info: ExtensionsResponse;
  onDone: () => void;
}) {
  const [days, setDays] = useState("");
  const [note, setNote] = useState("");
  const [reason, setReason] = useState("3");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const n = Number(days);
  const validDays = Number.isInteger(n) && n >= 1 && n <= info.max_days;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      await api.post(`/users/${userId}/extensions`, {
        days: n,
        note: note.trim(),
        ...(info.method === "apple" ? { reason: Number(reason) } : {}),
      });
      setDays("");
      setNote("");
      onOpenChange(false);
      onDone();
    } catch (err: any) {
      setError(err.message);
      // A store refusal is still recorded; show it in the history.
      onDone();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Give free time</DialogTitle>
          <DialogDescription>
            <span className="font-medium text-foreground">{METHOD_LABEL[info.method]}.</span>{" "}
            {METHOD_HELP[info.method]}
            {info.method === "apple" && ` ${info.apple_extensions_left} of 2 left this year.`}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit} className="space-y-4">
          <div>
            <Label htmlFor="free-days">Days (up to {info.max_days})</Label>
            <div className="mt-1.5 flex items-center gap-2">
              <Input
                id="free-days"
                type="number"
                min={1}
                max={info.max_days}
                value={days}
                onChange={(e) => setDays(e.target.value)}
                className="w-28"
                autoFocus
              />
              {PRESETS.filter((p) => p <= info.max_days).map((p) => (
                <Button key={p} type="button" variant="outline" size="sm" onClick={() => setDays(String(p))}>
                  {p}d
                </Button>
              ))}
            </div>
            {info.method === "apple" && n > 90 && (
              <p className="mt-1.5 text-xs text-muted-foreground">Over 90 days uses both of this year's Apple extensions.</p>
            )}
          </div>

          {info.method === "apple" && (
            <div>
              <Label>Reason (sent to Apple)</Label>
              <Select items={REASONS} value={reason} onValueChange={(v) => v != null && setReason(v)}>
                <SelectTrigger className="mt-1.5 w-64" aria-label="Reason">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {REASONS.map((r) => (
                    <SelectItem key={r.value} value={r.value}>
                      {r.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <div>
            <Label htmlFor="free-note">Note</Label>
            <Textarea
              id="free-note"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Why, e.g. Libredesk #133, Blue Mountains coverage outage"
              maxLength={1000}
              className="mt-1.5"
            />
          </div>

          {error && <p className="text-sm text-destructive break-all">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={!validDays || !note.trim() || saving}>
              {saving ? "Giving…" : validDays ? `Give ${n} days` : "Give free time"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
