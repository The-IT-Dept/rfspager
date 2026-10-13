import type { ChannelConfig, FieldErrors, RuntimeConfig } from "~/lib/receiver-api";

/**
 * The form edits strings so partial input ("-", "148.") never turns into
 * NaN mid-keystroke; fromDraft() converts to a RuntimeConfig on save.
 */
export interface ChannelDraft {
  label: string;
  /** MHz with up to four decimals, e.g. "148.5875". */
  freq_mhz: string;
  enabled: boolean;
}

export interface ConfigDraft {
  gain: string;
  ppm: string;
  sample_rate: string;
  dongle_serial: string;
  dongle_index: string;
  post_all: boolean;
  log_level: string;
  stats_interval_s: string;
  channels: ChannelDraft[];
}

export function hzToMHz(hz: number): string {
  return (hz / 1e6).toFixed(4);
}

export function mhzToHz(s: string): number | null {
  const t = s.trim();
  if (t === "") return null;
  const v = Number(t);
  if (!Number.isFinite(v)) return null;
  return Math.round(v * 1e6);
}

export function toDraft(c: RuntimeConfig): ConfigDraft {
  return {
    gain: String(c.tuner_gain_tenths_db),
    ppm: String(c.ppm),
    sample_rate: String(c.sample_rate),
    dongle_serial: c.dongle_serial,
    dongle_index: String(c.dongle_index),
    post_all: c.post_all,
    log_level: c.log_level,
    stats_interval_s: String(c.stats_interval_s),
    channels: c.channels.map((ch) => ({ label: ch.label, freq_mhz: hzToMHz(ch.freq_hz), enabled: ch.enabled })),
  };
}

/** Converts the draft to a RuntimeConfig. Parse problems come back keyed like server errors. */
export function fromDraft(d: ConfigDraft): { config: RuntimeConfig; errors: FieldErrors } {
  const errors: FieldErrors = {};
  const int = (s: string, key: string, fallback = 0): number => {
    const t = s.trim();
    if (t === "" || !/^-?\d+$/.test(t)) {
      errors[key] = "must be a whole number";
      return fallback;
    }
    return Number(t);
  };
  const channels: ChannelConfig[] = d.channels.map((ch, i) => {
    const hz = mhzToHz(ch.freq_mhz);
    if (hz == null) errors[`channels[${i}].freq_hz`] = "must be a frequency in MHz";
    return { label: ch.label.trim(), freq_hz: hz ?? 0, enabled: ch.enabled };
  });
  const config: RuntimeConfig = {
    tuner_gain_tenths_db: int(d.gain, "tuner_gain_tenths_db"),
    ppm: int(d.ppm, "ppm"),
    sample_rate: int(d.sample_rate, "sample_rate"),
    dongle_serial: d.dongle_serial.trim(),
    dongle_index: int(d.dongle_index, "dongle_index", -1),
    post_all: d.post_all,
    log_level: d.log_level,
    stats_interval_s: int(d.stats_interval_s, "stats_interval_s"),
    channels,
  };
  return { config, errors };
}

export function newChannel(): ChannelDraft {
  return { label: "", freq_mhz: "", enabled: true };
}

export function sameConfig(a: RuntimeConfig, b: RuntimeConfig): boolean {
  if (
    a.tuner_gain_tenths_db !== b.tuner_gain_tenths_db ||
    a.ppm !== b.ppm ||
    a.sample_rate !== b.sample_rate ||
    a.dongle_serial !== b.dongle_serial ||
    a.dongle_index !== b.dongle_index ||
    a.post_all !== b.post_all ||
    a.log_level !== b.log_level ||
    a.stats_interval_s !== b.stats_interval_s ||
    a.channels.length !== b.channels.length
  ) {
    return false;
  }
  return a.channels.every((ch, i) => {
    const o = b.channels[i];
    return ch.label === o.label && ch.freq_hz === o.freq_hz && ch.enabled === o.enabled;
  });
}
