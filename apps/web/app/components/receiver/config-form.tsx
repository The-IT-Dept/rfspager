import { useMemo, type ReactNode } from "react";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import {
  CONFIG_LIMITS,
  GAIN_LADDER_TENTHS,
  LOG_LEVELS,
  SAMPLE_RATES,
  centreHz,
  type DongleInfo,
  type FieldErrors,
} from "~/lib/receiver-api";
import { fromDraft, newChannel, type ChannelDraft, type ConfigDraft } from "./config-draft";
import { fmtMHz, fmtMSps, gainLabel } from "./format";

interface Props {
  draft: ConfigDraft;
  errors: FieldErrors;
  disabled?: boolean;
  /** Dongles the receiver reported, offered as serial suggestions. */
  dongles: DongleInfo[];
  onChange: (next: ConfigDraft) => void;
}

interface Item {
  value: string;
  label: string;
}

/** Adds the current value as an extra option when it is not on the list, so nothing is silently changed. */
function withCustom(items: Item[], value: string, label: (v: string) => string): Item[] {
  if (value === "" || items.some((i) => i.value === value)) return items;
  return [...items, { value, label: label(value) }];
}

export function ConfigForm({ draft, errors, disabled, dongles, onChange }: Props) {
  const set = <K extends keyof ConfigDraft>(key: K, value: ConfigDraft[K]) => onChange({ ...draft, [key]: value });
  const setChannel = (i: number, patch: Partial<ChannelDraft>) =>
    onChange({ ...draft, channels: draft.channels.map((ch, k) => (k === i ? { ...ch, ...patch } : ch)) });
  const removeChannel = (i: number) => onChange({ ...draft, channels: draft.channels.filter((_, k) => k !== i) });
  const addChannel = () => onChange({ ...draft, channels: [...draft.channels, newChannel()] });

  const gainItems = useMemo<Item[]>(
    () =>
      withCustom(
        GAIN_LADDER_TENTHS.map((t) => ({ value: String(t), label: t === 0 ? "AGC (automatic)" : gainLabel(t) })),
        draft.gain,
        (v) => `${v} tenths of a dB (custom)`,
      ),
    [draft.gain],
  );
  const rateItems = useMemo<Item[]>(
    () =>
      withCustom(
        SAMPLE_RATES.map((r) => ({ value: String(r), label: fmtMSps(r) })),
        draft.sample_rate,
        (v) => `${v} Hz (custom)`,
      ),
    [draft.sample_rate],
  );
  const levelItems = useMemo<Item[]>(
    () =>
      withCustom(
        LOG_LEVELS.map((l) => ({ value: l, label: l })),
        draft.log_level,
        (v) => `${v} (unknown)`,
      ),
    [draft.log_level],
  );

  const parsed = fromDraft(draft).config;
  const centre = centreHz(parsed);
  const enabledFreqs = parsed.channels.filter((c) => c.enabled && c.freq_hz > 0).map((c) => c.freq_hz);
  const span = enabledFreqs.length ? Math.max(...enabledFreqs) - Math.min(...enabledFreqs) : 0;
  const usable = CONFIG_LIMITS.usableBandwidth * parsed.sample_rate;
  const serialSuggestions = dongles.filter((d) => d.serial);

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Field id="cfg-gain" label="Tuner gain" error={errors.tuner_gain_tenths_db} hint="AGC, or a manual step on the R828D ladder.">
          <Select items={gainItems} value={draft.gain} onValueChange={(v) => v != null && set("gain", v)} disabled={disabled}>
            <SelectTrigger id="cfg-gain" className="w-full" aria-invalid={!!errors.tuner_gain_tenths_db}>
              <SelectValue placeholder="Select gain" />
            </SelectTrigger>
            <SelectContent>
              {gainItems.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field id="cfg-ppm" label="Frequency correction (ppm)" error={errors.ppm} hint={`-${CONFIG_LIMITS.maxPPM} to ${CONFIG_LIMITS.maxPPM}.`}>
          <Input
            id="cfg-ppm"
            type="number"
            inputMode="numeric"
            min={-CONFIG_LIMITS.maxPPM}
            max={CONFIG_LIMITS.maxPPM}
            step={1}
            value={draft.ppm}
            disabled={disabled}
            aria-invalid={!!errors.ppm}
            onChange={(e) => set("ppm", e.target.value)}
          />
        </Field>

        <Field id="cfg-rate" label="Sample rate" error={errors.sample_rate} hint="Multiples of 38.4 kHz between 0.768 and 2.4 MS/s.">
          <Select items={rateItems} value={draft.sample_rate} onValueChange={(v) => v != null && set("sample_rate", v)} disabled={disabled}>
            <SelectTrigger id="cfg-rate" className="w-full" aria-invalid={!!errors.sample_rate}>
              <SelectValue placeholder="Select rate" />
            </SelectTrigger>
            <SelectContent>
              {rateItems.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field id="cfg-serial" label="Dongle serial" error={errors.dongle_serial} hint="Blank picks the dongle by index.">
          <Input
            id="cfg-serial"
            list={serialSuggestions.length ? "cfg-serial-options" : undefined}
            value={draft.dongle_serial}
            disabled={disabled}
            aria-invalid={!!errors.dongle_serial}
            onChange={(e) => set("dongle_serial", e.target.value)}
            className="font-mono"
          />
          {serialSuggestions.length > 0 && (
            <datalist id="cfg-serial-options">
              {serialSuggestions.map((d) => (
                <option key={d.index} value={d.serial}>
                  {d.name ?? d.product ?? `dongle #${d.index}`}
                </option>
              ))}
            </datalist>
          )}
        </Field>

        <Field id="cfg-index" label="Dongle index" error={errors.dongle_index} hint="-1 means the first available dongle.">
          <Input
            id="cfg-index"
            type="number"
            inputMode="numeric"
            min={-1}
            step={1}
            value={draft.dongle_index}
            disabled={disabled}
            aria-invalid={!!errors.dongle_index}
            onChange={(e) => set("dongle_index", e.target.value)}
          />
        </Field>

        <Field id="cfg-level" label="Log level" error={errors.log_level}>
          <Select items={levelItems} value={draft.log_level} onValueChange={(v) => v != null && set("log_level", v)} disabled={disabled}>
            <SelectTrigger id="cfg-level" className="w-full" aria-invalid={!!errors.log_level}>
              <SelectValue placeholder="Select level" />
            </SelectTrigger>
            <SelectContent>
              {levelItems.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field
          id="cfg-interval"
          label="Stats interval (seconds)"
          error={errors.stats_interval_s}
          hint={`${CONFIG_LIMITS.minStatsIntervalS} to ${CONFIG_LIMITS.maxStatsIntervalS}.`}
        >
          <Input
            id="cfg-interval"
            type="number"
            inputMode="numeric"
            min={CONFIG_LIMITS.minStatsIntervalS}
            max={CONFIG_LIMITS.maxStatsIntervalS}
            step={1}
            value={draft.stats_interval_s}
            disabled={disabled}
            aria-invalid={!!errors.stats_interval_s}
            onChange={(e) => set("stats_interval_s", e.target.value)}
          />
        </Field>

        <Field id="cfg-postall" label="Post all pages" error={errors.post_all} hint="Bypasses the receiver's content filter: every decoded page is posted upstream.">
          <div className="flex h-8 items-center">
            <Switch id="cfg-postall" checked={draft.post_all} disabled={disabled} onCheckedChange={(v) => set("post_all", v)} />
          </div>
        </Field>
      </div>

      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Label>Channels</Label>
          <Button type="button" variant="outline" size="sm" onClick={addChannel} disabled={disabled || draft.channels.length >= CONFIG_LIMITS.maxChannels}>
            <Plus /> Add channel
          </Button>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">Enabled</TableHead>
              <TableHead>Label</TableHead>
              <TableHead className="w-48">Frequency (MHz)</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {draft.channels.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  No channels. Add at least one.
                </TableCell>
              </TableRow>
            )}
            {draft.channels.map((ch, i) => {
              const labelErr = errors[`channels[${i}].label`];
              const freqErr = errors[`channels[${i}].freq_hz`];
              return (
                <TableRow key={i} className="align-top">
                  <TableCell>
                    <div className="flex h-8 items-center">
                      <Switch
                        checked={ch.enabled}
                        disabled={disabled}
                        aria-label={`Channel ${ch.label || i + 1} enabled`}
                        onCheckedChange={(v) => setChannel(i, { enabled: v })}
                      />
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-normal">
                    <Input
                      value={ch.label}
                      disabled={disabled}
                      placeholder="RFS"
                      maxLength={32}
                      aria-invalid={!!labelErr}
                      aria-label={`Channel ${i + 1} label`}
                      onChange={(e) => setChannel(i, { label: e.target.value })}
                    />
                    {labelErr && <p className="mt-1 text-xs text-destructive">{labelErr}</p>}
                  </TableCell>
                  <TableCell className="whitespace-normal">
                    <Input
                      type="number"
                      inputMode="decimal"
                      step="0.0001"
                      min={CONFIG_LIMITS.minFreqHz / 1e6}
                      max={CONFIG_LIMITS.maxFreqHz / 1e6}
                      value={ch.freq_mhz}
                      disabled={disabled}
                      placeholder="148.5875"
                      aria-invalid={!!freqErr}
                      aria-label={`Channel ${i + 1} frequency in MHz`}
                      className="font-mono"
                      onChange={(e) => setChannel(i, { freq_mhz: e.target.value })}
                    />
                    {freqErr && <p className="mt-1 text-xs text-destructive">{freqErr}</p>}
                  </TableCell>
                  <TableCell>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={disabled || draft.channels.length <= 1}
                      aria-label={`Remove channel ${ch.label || i + 1}`}
                      onClick={() => removeChannel(i)}
                    >
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        {errors.channels && <p className="text-xs text-destructive">{errors.channels}</p>}
        <p className="text-xs text-muted-foreground">
          {centre != null
            ? `Dongle centre ${fmtMHz(centre)}; enabled channels span ${(span / 1e3).toFixed(1)} kHz of the ${(usable / 1e3).toFixed(1)} kHz usable at this sample rate.`
            : "Enable at least one channel to set the dongle centre."}
        </p>
      </div>
    </div>
  );
}

function Field({ id, label, hint, error, children }: { id: string; label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error ? <p className="text-xs text-destructive">{error}</p> : hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}
