import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowUpCircle, Power, RefreshCw, Usb } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import {
  VERSION_RE,
  errorMessage,
  getCommands,
  getLatestReceiverVersion,
  isActiveCommand,
  postCommand,
  type CommandRequest,
  type CommandRow,
} from "~/lib/receiver-api";
import { ConfirmDialog } from "./confirm-dialog";
import { fmtDate } from "./format";
import { CommandStatusBadge, ToneBadge } from "./status-badge";

const POLL_MS = 5_000;

interface Pending {
  request: CommandRequest;
  title: string;
  description: string;
  confirmLabel: string;
  destructive?: boolean;
}

interface Props {
  systemId: string;
  currentVersion: string | null;
  online: boolean;
  /** Protocol version the receiver declared; null when unknown. Receivers below 2 ignore commands. */
  proto: number | null;
  /** Called when the last active command settles, so the caller can re-read the system row. */
  onSystemChanged?: () => void;
}

export function UpdateTab({ systemId, currentVersion, online, proto, onSystemChanged }: Props) {
  const [latest, setLatest] = useState<string | null>(null);
  const [latestError, setLatestError] = useState<string | null>(null);
  const [versionInput, setVersionInput] = useState("");
  const [commands, setCommands] = useState<CommandRow[] | null>(null);
  const [cmdError, setCmdError] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [posting, setPosting] = useState(false);
  const [postError, setPostError] = useState<string | null>(null);

  const hadActiveRef = useRef(false);
  const onChangedRef = useRef(onSystemChanged);
  onChangedRef.current = onSystemChanged;

  useEffect(() => {
    let cancelled = false;
    getLatestReceiverVersion()
      .then((v) => {
        if (!cancelled) setLatest(v);
      })
      .catch((e: unknown) => {
        if (!cancelled) setLatestError(errorMessage(e));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const loadCommands = useCallback(async () => {
    try {
      const rows = await getCommands(systemId, 50);
      setCommands(rows);
      setCmdError(null);
      const active = rows.some(isActiveCommand);
      if (hadActiveRef.current && !active) onChangedRef.current?.();
      hadActiveRef.current = active;
    } catch (e: unknown) {
      setCmdError(errorMessage(e));
    }
  }, [systemId]);

  useEffect(() => {
    void loadCommands();
  }, [loadCommands]);

  const active = commands?.some(isActiveCommand) ?? false;
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => void loadCommands(), POLL_MS);
    return () => clearInterval(t);
  }, [active, loadCommands]);

  const confirm = async () => {
    if (!pending) return;
    setPosting(true);
    setPostError(null);
    try {
      const row = await postCommand(systemId, pending.request);
      setCommands((prev) => [row, ...(prev ?? []).filter((c) => c.id !== row.id)]);
      hadActiveRef.current = true;
      setPending(null);
    } catch (e: unknown) {
      setPostError(errorMessage(e));
    } finally {
      setPosting(false);
    }
  };

  const askUpdate = (version: string) =>
    setPending({
      request: { type: "update", params: { version } },
      title: `Update receiver to ${version}?`,
      description: `The receiver downloads ${version}, verifies its checksum, swaps the binary and restarts. It rolls back on its own if the new build crash-loops.`,
      confirmLabel: "Update",
    });

  const typedVersion = versionInput.trim();
  const typedValid = VERSION_RE.test(typedVersion);
  // Release tags look like v5.4.0; builds report 5.4.0-<commit>. Compare the
  // numeric part only, as the receiver itself does.
  const baseVersion = (v: string) => v.trim().replace(/^v/i, "").split("-")[0];
  const upToDate =
    latest != null && currentVersion != null && baseVersion(latest) === baseVersion(currentVersion);
  const legacy = proto != null && proto < 2;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Versions</CardTitle>
        </CardHeader>
        <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label="Installed">
            <span className="font-mono">{currentVersion ?? "—"}</span>
          </Field>
          <Field label="Latest release">
            <span className="font-mono">{latest ?? (latestError ? "unknown" : "…")}</span>
            {latestError && <span className="ml-2 text-xs text-destructive">{latestError}</span>}
          </Field>
          <Field label="Status">
            {latest == null || currentVersion == null ? (
              <ToneBadge tone="muted">unknown</ToneBadge>
            ) : upToDate ? (
              <ToneBadge tone="ok">up to date</ToneBadge>
            ) : (
              <ToneBadge tone="warn">update available</ToneBadge>
            )}
          </Field>
        </CardContent>
      </Card>

      {legacy && (
        <p className="rounded-lg border border-amber-400 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-300">
          This receiver speaks protocol {proto} and ignores commands. Re-run the installer from the Overview tab once; after that it is
          fleet-managed.
        </p>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Actions</CardTitle>
          <CardDescription>
            Each action is queued as a command and delivered over the receiver's websocket. Queued commands wait for the next connection and
            expire after an hour.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" disabled={!latest || upToDate} onClick={() => latest && askUpdate(latest)}>
              <ArrowUpCircle /> Update to latest{latest ? ` (${latest})` : ""}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() =>
                setPending({
                  request: { type: "restart" },
                  title: "Restart the receiver?",
                  description: "The receiver process exits and systemd starts it again. Decoding stops for a few seconds.",
                  confirmLabel: "Restart",
                })
              }
            >
              <Power /> Restart receiver
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() =>
                setPending({
                  request: { type: "dongle_reset" },
                  title: "Reset the dongle?",
                  description: "The receiver closes and reopens the RTL-SDR. Use this when stats show the dongle closed or stuck.",
                  confirmLabel: "Reset dongle",
                })
              }
            >
              <Usb /> Reset dongle
            </Button>
          </div>

          <div className="grid gap-2 sm:grid-cols-[14rem_auto] sm:items-end">
            <div className="space-y-1.5">
              <Label htmlFor="update-version">Update to version…</Label>
              <Input
                id="update-version"
                value={versionInput}
                onChange={(e) => setVersionInput(e.target.value)}
                placeholder="v5.5.0"
                className="font-mono"
                aria-invalid={typedVersion !== "" && !typedValid}
              />
            </div>
            <Button type="button" variant="outline" disabled={!typedValid} onClick={() => askUpdate(typedVersion)}>
              <ArrowUpCircle /> Update to {typedValid ? typedVersion : "version"}
            </Button>
          </div>
          {typedVersion !== "" && !typedValid && <p className="text-xs text-destructive">Use a release tag like v5.5.0.</p>}
          {!online && <p className="text-xs text-muted-foreground">The receiver is offline; commands queue until it reconnects.</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center justify-between gap-3">
            <span className="inline-flex items-center gap-2">
              Commands
              {active && <ToneBadge tone="info">polling</ToneBadge>}
            </span>
            <Button type="button" variant="ghost" size="sm" onClick={() => void loadCommands()}>
              <RefreshCw /> Refresh
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {cmdError && <p className="mb-3 text-sm text-destructive">{cmdError}</p>}
          {commands === null && !cmdError && <p className="text-sm text-muted-foreground">Loading…</p>}
          {commands && commands.length === 0 && <p className="text-sm text-muted-foreground">No commands sent yet.</p>}
          {commands && commands.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Stage</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead>Requested by</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Updated</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {commands.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-mono text-xs">
                      {c.type}
                      {c.type === "update" && typeof c.params?.version === "string" && (
                        <span className="ml-1 text-muted-foreground">{c.params.version}</span>
                      )}
                      {c.type === "capture" && typeof c.params?.kind === "string" && (
                        <span className="ml-1 text-muted-foreground">{c.params.kind}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <CommandStatusBadge status={c.status} />
                    </TableCell>
                    <TableCell>{c.stage || "—"}</TableCell>
                    <TableCell className="max-w-md whitespace-normal">{c.message || "—"}</TableCell>
                    <TableCell>{c.requested_by || "—"}</TableCell>
                    <TableCell className="text-muted-foreground">{fmtDate(c.created_at)}</TableCell>
                    <TableCell className="text-muted-foreground">{fmtDate(c.updated_at)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPending(null);
            setPostError(null);
          }
        }}
        title={pending?.title ?? ""}
        description={pending?.description}
        confirmLabel={pending?.confirmLabel}
        destructive={pending?.destructive}
        busy={posting}
        error={postError}
        onConfirm={() => void confirm()}
      />
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm">{children}</p>
    </div>
  );
}
