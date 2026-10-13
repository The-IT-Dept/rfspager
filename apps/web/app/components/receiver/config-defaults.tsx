import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import type { RuntimeConfig } from "~/lib/receiver-api";
import { cn } from "~/lib/utils";
import { fmtMSps, gainLabel } from "./format";

interface Props {
  defaults: RuntimeConfig;
  current: RuntimeConfig | null;
  disabled?: boolean;
  onLoad: () => void;
}

function describeChannels(c: RuntimeConfig): string {
  return c.channels.map((ch) => `${ch.label || "?"} ${(ch.freq_hz / 1e6).toFixed(4)}${ch.enabled ? "" : " (off)"}`).join(", ") || "none";
}

export function ConfigDefaults({ defaults, current, disabled, onLoad }: Props) {
  const rows: { label: string; def: string; cur: string | null }[] = [
    { label: "Tuner gain", def: gainLabel(defaults.tuner_gain_tenths_db), cur: current ? gainLabel(current.tuner_gain_tenths_db) : null },
    { label: "PPM", def: String(defaults.ppm), cur: current ? String(current.ppm) : null },
    { label: "Sample rate", def: fmtMSps(defaults.sample_rate), cur: current ? fmtMSps(current.sample_rate) : null },
    {
      label: "Dongle",
      def: defaults.dongle_serial ? `serial ${defaults.dongle_serial}` : `index ${defaults.dongle_index}`,
      cur: current ? (current.dongle_serial ? `serial ${current.dongle_serial}` : `index ${current.dongle_index}`) : null,
    },
    { label: "Post all pages", def: defaults.post_all ? "yes" : "no", cur: current ? (current.post_all ? "yes" : "no") : null },
    { label: "Log level", def: defaults.log_level, cur: current ? current.log_level : null },
    { label: "Stats interval", def: `${defaults.stats_interval_s} s`, cur: current ? `${current.stats_interval_s} s` : null },
    { label: "Channels", def: describeChannels(defaults), cur: current ? describeChannels(current) : null },
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center justify-between gap-3">
          <span>Defaults</span>
          <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={onLoad}>
            Load defaults into form
          </Button>
        </CardTitle>
        <CardDescription>What a receiver runs with no revision at all. Rows that differ from the current revision are emphasised.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Setting</TableHead>
              <TableHead>Default</TableHead>
              <TableHead>Current revision</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => {
              const differs = r.cur != null && r.cur !== r.def;
              return (
                <TableRow key={r.label}>
                  <TableCell className={cn(differs && "font-medium")}>{r.label}</TableCell>
                  <TableCell className="whitespace-normal text-muted-foreground">{r.def}</TableCell>
                  <TableCell className={cn("whitespace-normal", differs ? "font-medium" : "text-muted-foreground")}>{r.cur ?? "—"}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
