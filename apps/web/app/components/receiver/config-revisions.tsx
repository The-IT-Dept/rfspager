import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import type { AppliedConfig, ConfigRevision, RuntimeConfig } from "~/lib/receiver-api";
import { fmtDate, fmtMSps, gainLabel } from "./format";
import { ToneBadge, configStatusTone } from "./status-badge";

interface Props {
  revisions: ConfigRevision[];
  currentVersion: number | null;
  applied: AppliedConfig | null;
  disabled?: boolean;
  onLoad: (rev: ConfigRevision) => void;
}

export function summarizeConfig(c: RuntimeConfig): string {
  const chans = c.channels
    .map((ch) => `${ch.label || "?"} ${(ch.freq_hz / 1e6).toFixed(4)}${ch.enabled ? "" : " (off)"}`)
    .join(", ");
  return `${gainLabel(c.tuner_gain_tenths_db)} · ${c.ppm} ppm · ${fmtMSps(c.sample_rate)} · ${chans || "no channels"}`;
}

export function ConfigRevisions({ revisions, currentVersion, applied, disabled, onLoad }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Revisions</CardTitle>
        <CardDescription>The newest revision is what the receiver is asked to run. Load an older one into the form to roll back.</CardDescription>
      </CardHeader>
      <CardContent>
        {revisions.length === 0 ? (
          <p className="text-sm text-muted-foreground">No revisions saved yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-32">Version</TableHead>
                <TableHead>Source</TableHead>
                <TableHead>Note</TableHead>
                <TableHead>Summary</TableHead>
                <TableHead>Saved</TableHead>
                <TableHead className="w-32" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {revisions.map((r) => (
                <TableRow key={r.version}>
                  <TableCell>
                    <span className="inline-flex flex-wrap items-center gap-1.5">
                      <span className="font-mono">{r.version}</span>
                      {r.version === currentVersion && <ToneBadge tone="info">current</ToneBadge>}
                      {applied && applied.version === r.version && (
                        <ToneBadge tone={configStatusTone(applied.status)}>{applied.status || "reported"}</ToneBadge>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs">{r.source || "—"}</TableCell>
                  <TableCell className="max-w-48 truncate" title={r.note}>
                    {r.note || "—"}
                  </TableCell>
                  <TableCell className="max-w-md whitespace-normal text-xs text-muted-foreground">{summarizeConfig(r.config)}</TableCell>
                  <TableCell className="text-muted-foreground">{fmtDate(r.created_at)}</TableCell>
                  <TableCell>
                    <Button type="button" variant="outline" size="xs" disabled={disabled} onClick={() => onLoad(r)}>
                      Load into form
                    </Button>
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
