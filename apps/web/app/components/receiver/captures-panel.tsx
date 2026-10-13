import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Download, RefreshCw } from "lucide-react";
import { Button, buttonVariants } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import {
  MAX_CAPTURE_SECONDS,
  captureDownloadPath,
  downloadUrl,
  errorMessage,
  getCaptures,
  isActiveCapture,
  postCommand,
  type CaptureRow,
  type ChannelConfig,
} from "~/lib/receiver-api";
import { cn } from "~/lib/utils";
import { fmtBytes, fmtDate } from "./format";
import { CaptureStatusBadge } from "./status-badge";

const POLL_MS = 3_000;

const KIND_OPTIONS = [
  { value: "audio", label: "Audio (FM demod, int16 LE)" },
  { value: "iq", label: "IQ (post-FIR baseband, int16 LE)" },
];

interface Props {
  systemId: string;
  channels: ChannelConfig[];
  online: boolean;
  /** Bump to re-read the list, e.g. after a spectrum snapshot is requested elsewhere. */
  refreshKey?: number;
}

export function CapturesPanel({ systemId, channels, online, refreshKey = 0 }: Props) {
  const [captures, setCaptures] = useState<CaptureRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const enabledChannels = channels.filter((c) => c.enabled && c.label);
  const channelItems = enabledChannels.map((c) => ({ value: c.label, label: `${c.label} (${(c.freq_hz / 1e6).toFixed(4)} MHz)` }));
  const firstLabel = enabledChannels[0]?.label ?? "";

  const [kind, setKind] = useState("audio");
  const [channel, setChannel] = useState("");
  const [seconds, setSeconds] = useState("10");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitOk, setSubmitOk] = useState<string | null>(null);

  useEffect(() => {
    if (!channel && firstLabel) setChannel(firstLabel);
  }, [channel, firstLabel]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setCaptures(await getCaptures(systemId));
      setError(null);
    } catch (e: unknown) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [systemId]);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  const active = captures?.some(isActiveCapture) ?? false;
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(t);
  }, [active, load]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const secs = Number(seconds);
    if (!Number.isInteger(secs) || secs < 1 || secs > MAX_CAPTURE_SECONDS) {
      setSubmitError(`Seconds must be a whole number from 1 to ${MAX_CAPTURE_SECONDS}.`);
      return;
    }
    if (!channel) {
      setSubmitError("Pick a channel.");
      return;
    }
    if (kind !== "audio" && kind !== "iq") {
      setSubmitError("Pick a capture kind.");
      return;
    }
    setSubmitting(true);
    setSubmitError(null);
    setSubmitOk(null);
    try {
      await postCommand(systemId, { type: "capture", params: { kind, channel, seconds: secs } });
      setSubmitOk(`${kind === "iq" ? "IQ" : "Audio"} capture of ${channel} for ${secs} s requested.`);
      await load();
    } catch (err: unknown) {
      setSubmitError(errorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center justify-between gap-3">
          <span>Captures</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} /> Refresh
          </Button>
        </CardTitle>
        <CardDescription>
          Audio and IQ captures of one channel, up to {MAX_CAPTURE_SECONDS} s, uploaded by the receiver. Replay them with cmd/replay to test
          decoder changes against real signal.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <form onSubmit={(e) => void submit(e)} className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_8rem_auto] lg:items-end">
          <div className="space-y-1.5">
            <Label htmlFor="cap-kind">Kind</Label>
            <Select items={KIND_OPTIONS} value={kind} onValueChange={(v) => v != null && setKind(v)} disabled={submitting}>
              <SelectTrigger id="cap-kind" className="w-full">
                <SelectValue placeholder="Kind" />
              </SelectTrigger>
              <SelectContent>
                {KIND_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cap-channel">Channel</Label>
            <Select
              items={channelItems}
              value={channel || null}
              onValueChange={(v) => v != null && setChannel(v)}
              disabled={submitting || channelItems.length === 0}
            >
              <SelectTrigger id="cap-channel" className="w-full">
                <SelectValue placeholder={channelItems.length ? "Channel" : "No enabled channels"} />
              </SelectTrigger>
              <SelectContent>
                {channelItems.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cap-seconds">Seconds</Label>
            <Input
              id="cap-seconds"
              type="number"
              inputMode="numeric"
              min={1}
              max={MAX_CAPTURE_SECONDS}
              step={1}
              value={seconds}
              disabled={submitting}
              onChange={(e) => setSeconds(e.target.value)}
            />
          </div>
          <Button type="submit" disabled={submitting || !online || channelItems.length === 0}>
            {submitting ? "Requesting…" : "Request capture"}
          </Button>
        </form>
        {!online && <p className="text-xs text-muted-foreground">The receiver is offline; captures need a live receiver.</p>}
        {submitError && <p className="text-sm text-destructive">{submitError}</p>}
        {submitOk && <p className="text-sm text-emerald-700 dark:text-emerald-300">{submitOk}</p>}

        {error && <p className="text-sm text-destructive">{error}</p>}
        {captures === null && !error && <p className="text-sm text-muted-foreground">Loading…</p>}
        {captures && captures.length === 0 && <p className="text-sm text-muted-foreground">No captures yet.</p>}
        {captures && captures.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Kind</TableHead>
                <TableHead>Channel</TableHead>
                <TableHead>Seconds</TableHead>
                <TableHead>Rate</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>Requested by</TableHead>
                <TableHead>Requested</TableHead>
                <TableHead>Ready</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {captures.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-mono text-xs">{c.kind || "—"}</TableCell>
                  <TableCell>{c.channel || "—"}</TableCell>
                  <TableCell>{c.seconds ?? "—"}</TableCell>
                  <TableCell>{c.sample_rate ? `${c.sample_rate.toLocaleString()} Hz` : "—"}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-2">
                      <CaptureStatusBadge status={c.status} />
                      {c.status === "error" && c.error && (
                        <span className="max-w-48 truncate text-xs text-destructive" title={c.error}>
                          {c.error}
                        </span>
                      )}
                    </span>
                  </TableCell>
                  <TableCell>{fmtBytes(c.size_bytes)}</TableCell>
                  <TableCell>{c.requested_by || "—"}</TableCell>
                  <TableCell className="text-muted-foreground">{fmtDate(c.created_at)}</TableCell>
                  <TableCell className="text-muted-foreground">{fmtDate(c.ready_at)}</TableCell>
                  <TableCell>
                    {c.status === "ready" && (
                      <a
                        href={downloadUrl(captureDownloadPath(systemId, c.id))}
                        target="_blank"
                        rel="noreferrer"
                        className={cn(buttonVariants({ variant: "outline", size: "xs" }))}
                      >
                        <Download /> Download
                      </a>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
