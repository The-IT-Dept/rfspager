/**
 * Typed client for the receiver remote-management admin API
 * (/api/v1/admin/systems/{id}/...). The JSON shapes mirror
 * internal/rxproto. The server is authoritative for validation; PUT config
 * answers 422 {errors: {field: message}} which surfaces as
 * ConfigValidationError so the form can attach messages to inputs.
 *
 * Everything coming back from the server is normalised defensively: jsonb
 * columns may arrive as objects or as JSON strings, Go nil slices arrive as
 * null, and older rows may lack newer keys.
 */
import { api, getToken } from "~/lib/api";

const API_BASE = "/api/v1/admin";

// ---------------------------------------------------------------------------
// Runtime config (rxproto.RuntimeConfig)

export interface ChannelConfig {
  label: string;
  freq_hz: number;
  enabled: boolean;
}

export const LOG_LEVELS = ["debug", "info", "warn"] as const;
export type LogLevel = (typeof LOG_LEVELS)[number];

export interface RuntimeConfig {
  /** Tenths of a dB on the R828D ladder; 0 means AGC. */
  tuner_gain_tenths_db: number;
  ppm: number;
  sample_rate: number;
  /** Empty string picks the dongle by index instead. */
  dongle_serial: string;
  /** -1 means the first available dongle. */
  dongle_index: number;
  post_all: boolean;
  log_level: string;
  stats_interval_s: number;
  channels: ChannelConfig[];
}

/** R828D tuner gain steps in tenths of a dB. 0 is AGC. */
export const GAIN_LADDER_TENTHS: readonly number[] = [
  0, 9, 14, 27, 37, 77, 87, 125, 144, 157, 166, 197, 207, 229, 254, 280, 297, 328, 338, 364, 372,
  386, 402, 421, 434, 439, 445, 480, 496,
];

/** Sample rates offered in the form; every one is a multiple of the 38.4 kHz audio rate. */
export const SAMPLE_RATES: readonly number[] = [1_920_000, 1_152_000, 768_000, 2_304_000];

/** Mirrors the validation limits in rxproto. The server re-checks everything. */
export const CONFIG_LIMITS = {
  maxGainTenthsDB: 600,
  maxPPM: 200,
  minSampleRate: 768_000,
  maxSampleRate: 2_400_000,
  audioRate: 38_400,
  maxChannels: 4,
  minStatsIntervalS: 5,
  maxStatsIntervalS: 300,
  usableBandwidth: 0.8,
  minFreqHz: 24_000_000,
  maxFreqHz: 1_750_000_000,
} as const;

/** rxproto.Defaults(): what a receiver with no revisions runs. */
export function defaultRuntimeConfig(): RuntimeConfig {
  return {
    tuner_gain_tenths_db: 0,
    ppm: 0,
    sample_rate: 1_920_000,
    dongle_serial: "",
    dongle_index: -1,
    post_all: false,
    log_level: "info",
    stats_interval_s: 15,
    channels: [
      { label: "RFS", freq_hz: 148_587_500, enabled: true },
      { label: "FRNSW", freq_hz: 148_962_500, enabled: true },
    ],
  };
}

export type FieldErrors = Record<string, string>;

/**
 * Client-side mirror of RuntimeConfig.Validate() so the form can catch the
 * obvious problems before a round trip. Keys match the server's: JSON names,
 * with channel problems keyed "channels[i].field".
 */
export function validateRuntimeConfig(c: RuntimeConfig): FieldErrors {
  const L = CONFIG_LIMITS;
  const errs: FieldErrors = {};
  if (!Number.isInteger(c.tuner_gain_tenths_db) || c.tuner_gain_tenths_db < 0 || c.tuner_gain_tenths_db > L.maxGainTenthsDB) {
    errs.tuner_gain_tenths_db = `must be 0 (AGC) to ${L.maxGainTenthsDB}`;
  }
  if (!Number.isInteger(c.ppm) || c.ppm < -L.maxPPM || c.ppm > L.maxPPM) {
    errs.ppm = `must be between -${L.maxPPM} and ${L.maxPPM}`;
  }
  if (!Number.isInteger(c.sample_rate) || c.sample_rate < L.minSampleRate || c.sample_rate > L.maxSampleRate) {
    errs.sample_rate = `must be between ${L.minSampleRate} and ${L.maxSampleRate}`;
  } else if (c.sample_rate % L.audioRate !== 0) {
    errs.sample_rate = `must be a multiple of ${L.audioRate}`;
  }
  if (!Number.isInteger(c.dongle_index) || c.dongle_index < -1) {
    errs.dongle_index = "must be -1 (first available) or a device index";
  }
  if (!(LOG_LEVELS as readonly string[]).includes(c.log_level)) {
    errs.log_level = `must be one of ${LOG_LEVELS.join(", ")}`;
  }
  if (!Number.isInteger(c.stats_interval_s) || c.stats_interval_s < L.minStatsIntervalS || c.stats_interval_s > L.maxStatsIntervalS) {
    errs.stats_interval_s = `must be between ${L.minStatsIntervalS} and ${L.maxStatsIntervalS} seconds`;
  }
  validateChannels(c, errs);
  return errs;
}

function validateChannels(c: RuntimeConfig, errs: FieldErrors) {
  const L = CONFIG_LIMITS;
  if (c.channels.length === 0) {
    errs.channels = "at least one channel is required";
    return;
  }
  if (c.channels.length > L.maxChannels) {
    errs.channels = `at most ${L.maxChannels} channels`;
    return;
  }
  const seen = new Set<string>();
  c.channels.forEach((ch, i) => {
    const label = ch.label.trim();
    if (label === "") errs[`channels[${i}].label`] = "label is required";
    else if (seen.has(label)) errs[`channels[${i}].label`] = "duplicate label";
    seen.add(label);
    if (!Number.isFinite(ch.freq_hz) || ch.freq_hz < L.minFreqHz || ch.freq_hz > L.maxFreqHz) {
      errs[`channels[${i}].freq_hz`] = "must be within the RTL-SDR tuning range (24 MHz to 1.75 GHz)";
    }
  });
  const enabled = c.channels.filter((ch) => ch.enabled);
  if (enabled.length === 0) {
    errs.channels = "at least one channel must be enabled";
    return;
  }
  const freqs = enabled.map((ch) => ch.freq_hz);
  const span = Math.max(...freqs) - Math.min(...freqs);
  const usable = L.usableBandwidth * c.sample_rate;
  if (c.sample_rate > 0 && span > usable) {
    errs.channels = `enabled channels span ${span} Hz; at most ${Math.round(usable)} Hz fits in a ${c.sample_rate} Hz capture`;
  }
}

/** Centre frequency the receiver will tune: midpoint of the enabled channels. */
export function centreHz(c: RuntimeConfig): number | null {
  const enabled = c.channels.filter((ch) => ch.enabled && Number.isFinite(ch.freq_hz));
  if (enabled.length === 0) return null;
  const freqs = enabled.map((ch) => ch.freq_hz);
  const lo = Math.min(...freqs);
  const hi = Math.max(...freqs);
  return lo + Math.floor((hi - lo) / 2);
}

// ---------------------------------------------------------------------------
// Stats (rxproto.Stats)

export interface DongleStats {
  open: boolean;
  gain_tenths_db: number;
  agc: boolean;
  ppm: number;
  centre_hz: number;
  sample_rate: number;
  iq_buffers: number;
  iq_dropped: number;
  gaps: number;
  /** Processing time over buffer duration; 1.0 means falling behind. */
  dsp_load: number;
}

export interface HostStats {
  cpu_temp_c: number;
  load1: number;
  mem_free_mb: number;
  /** Pi firmware get_throttled value, "" when unavailable. */
  throttled_hex: string;
  undervoltage_now: boolean;
  undervoltage_ever: boolean;
  uptime_s: number;
}

export interface LinkStats {
  reconnects_total: number;
  queue_depth: number;
  dropped_frames_total: number;
  rtt_ms: number;
}

export interface ChannelStats {
  label: string;
  freq_hz: number;
  power_dbfs: number;
  noise_floor_dbfs: number;
  snr_db: number;
  carrier_pct: number;
  sync_locks: number;
  sync_losses: number;
  cw_clean: number;
  cw_corrected: number;
  cw_uncorrectable: number;
  pages: number;
  pages_truncated: number;
  /** -1 when no page since start. */
  last_page_age_s: number;
}

export interface Stats {
  interval_s: number;
  dongle: DongleStats;
  host: HostStats;
  link: LinkStats;
  channels: ChannelStats[];
}

export interface StatsSample {
  ts: string;
  stats: Stats;
}

const EMPTY_DONGLE: DongleStats = {
  open: false, gain_tenths_db: 0, agc: false, ppm: 0, centre_hz: 0, sample_rate: 0,
  iq_buffers: 0, iq_dropped: 0, gaps: 0, dsp_load: 0,
};
const EMPTY_HOST: HostStats = {
  cpu_temp_c: 0, load1: 0, mem_free_mb: 0, throttled_hex: "",
  undervoltage_now: false, undervoltage_ever: false, uptime_s: 0,
};
const EMPTY_LINK: LinkStats = { reconnects_total: 0, queue_depth: 0, dropped_frames_total: 0, rtt_ms: 0 };

/** Share of codewords that could not be corrected, 0..1, or null when none were decoded. */
export function uncorrectableShare(ch: ChannelStats): number | null {
  const total = (ch.cw_clean ?? 0) + (ch.cw_corrected ?? 0) + (ch.cw_uncorrectable ?? 0);
  if (total <= 0) return null;
  return (ch.cw_uncorrectable ?? 0) / total;
}

export function normalizeStats(raw: unknown): Stats | null {
  const obj = parseJSONField<Partial<Stats>>(raw);
  if (!obj) return null;
  return {
    interval_s: toInt(obj.interval_s, 0),
    dongle: { ...EMPTY_DONGLE, ...(isObject(obj.dongle) ? obj.dongle : {}) },
    host: { ...EMPTY_HOST, ...(isObject(obj.host) ? obj.host : {}) },
    link: { ...EMPTY_LINK, ...(isObject(obj.link) ? obj.link : {}) },
    channels: Array.isArray(obj.channels) ? obj.channels.filter(isObject) : [],
  };
}

export function normalizeStatsSample(raw: unknown): StatsSample | null {
  const obj = parseJSONField<{ ts?: unknown; stats?: unknown }>(raw);
  if (!obj) return null;
  const stats = normalizeStats(obj.stats);
  if (!stats) return null;
  return { ts: toISO(obj.ts), stats };
}

/** Parses one SSE `data:` payload from the stats stream. */
export function parseStatsSample(data: string): StatsSample | null {
  try {
    return normalizeStatsSample(JSON.parse(data));
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Connection extras on the system row

export interface HostInfo {
  model?: string;
  kernel?: string;
  uptime_s?: number;
  rtlsdr?: string;
}

export interface DongleInfo {
  index: number;
  name?: string;
  manufacturer?: string;
  product?: string;
  serial?: string;
  tuner?: string;
}

export function parseHostInfo(raw: unknown): HostInfo | null {
  const obj = parseJSONField<HostInfo>(raw);
  return obj && isObject(obj) ? obj : null;
}

export function parseDongles(raw: unknown): DongleInfo[] {
  const list = parseJSONField<unknown>(raw);
  if (!Array.isArray(list)) return [];
  return list.filter(isObject).map((d, i) => ({
    index: toInt(d.index, i),
    name: optString(d.name),
    manufacturer: optString(d.manufacturer),
    product: optString(d.product),
    serial: optString(d.serial),
    tuner: optString(d.tuner),
  }));
}

function optString(v: unknown): string | undefined {
  return typeof v === "string" && v !== "" ? v : undefined;
}

// ---------------------------------------------------------------------------
// Config endpoint

export interface ConfigRevision {
  version: number;
  config: RuntimeConfig;
  source: string;
  note: string;
  created_at: string;
}

export interface AppliedConfig {
  version: number;
  status: string; // applied | rejected
}

export interface ConfigResponse {
  current: ConfigRevision | null;
  defaults: RuntimeConfig;
  revisions: ConfigRevision[];
  applied: AppliedConfig | null;
}

export function normalizeRuntimeConfig(raw: unknown, base: RuntimeConfig = defaultRuntimeConfig()): RuntimeConfig {
  const obj = parseJSONField<Partial<RuntimeConfig>>(raw);
  if (!obj) return { ...base, channels: base.channels.map((c) => ({ ...c })) };
  const channels = Array.isArray(obj.channels)
    ? obj.channels.filter(isObject).map((c) => ({
        label: typeof c.label === "string" ? c.label : "",
        freq_hz: toInt(c.freq_hz, 0),
        enabled: c.enabled === true,
      }))
    : base.channels.map((c) => ({ ...c }));
  return {
    tuner_gain_tenths_db: toInt(obj.tuner_gain_tenths_db, base.tuner_gain_tenths_db),
    ppm: toInt(obj.ppm, base.ppm),
    sample_rate: toInt(obj.sample_rate, base.sample_rate),
    dongle_serial: typeof obj.dongle_serial === "string" ? obj.dongle_serial : base.dongle_serial,
    dongle_index: toInt(obj.dongle_index, base.dongle_index),
    post_all: typeof obj.post_all === "boolean" ? obj.post_all : base.post_all,
    log_level: typeof obj.log_level === "string" ? obj.log_level : base.log_level,
    stats_interval_s: toInt(obj.stats_interval_s, base.stats_interval_s),
    channels,
  };
}

function normalizeRevision(raw: unknown, defaults: RuntimeConfig): ConfigRevision | null {
  const obj = parseJSONField<Partial<ConfigRevision>>(raw);
  if (!obj) return null;
  return {
    version: toInt(obj.version, 0),
    config: normalizeRuntimeConfig(obj.config, defaults),
    source: typeof obj.source === "string" ? obj.source : "",
    note: typeof obj.note === "string" ? obj.note : "",
    created_at: toISO(obj.created_at),
  };
}

export function normalizeConfigResponse(raw: unknown): ConfigResponse {
  const obj = parseJSONField<Partial<ConfigResponse>>(raw) ?? {};
  const defaults = normalizeRuntimeConfig(obj.defaults);
  const revisions = (Array.isArray(obj.revisions) ? obj.revisions : [])
    .map((r) => normalizeRevision(r, defaults))
    .filter(notNull)
    .sort((a, b) => b.version - a.version);
  const current = normalizeRevision(obj.current, defaults);
  const appliedObj = parseJSONField<Partial<AppliedConfig>>(obj.applied);
  const applied =
    appliedObj && typeof appliedObj.version === "number"
      ? { version: appliedObj.version, status: typeof appliedObj.status === "string" ? appliedObj.status : "" }
      : null;
  return { current, defaults, revisions, applied };
}

export class ConfigValidationError extends Error {
  readonly errors: FieldErrors;
  constructor(errors: FieldErrors, message?: string) {
    super(message || "The server rejected the configuration");
    this.name = "ConfigValidationError";
    this.errors = errors;
  }
}

function normalizeFieldErrors(raw: unknown): FieldErrors {
  const out: FieldErrors = {};
  if (!isObject(raw)) return out;
  for (const [k, v] of Object.entries(raw)) {
    if (typeof v === "string") out[k] = v;
    else if (Array.isArray(v)) out[k] = v.map(String).join("; ");
    else if (v != null) out[k] = String(v);
  }
  return out;
}

export function getSystemConfig(systemId: string | number): Promise<ConfigResponse> {
  return api.get<unknown>(`/systems/${systemId}/config`).then(normalizeConfigResponse);
}

export async function putSystemConfig(
  systemId: string | number,
  body: { config: RuntimeConfig; note?: string },
): Promise<ConfigResponse> {
  const res = await adminFetch(`/systems/${systemId}/config`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (res.status === 422) {
    const payload = (await res.json().catch(() => ({}))) as { errors?: unknown; error?: string };
    throw new ConfigValidationError(normalizeFieldErrors(payload.errors), payload.error);
  }
  if (!res.ok) throw new Error(await errorText(res));
  // The server answers with the new revision alone; re-read the full shape
  // (current, defaults, revisions, applied) so the caller has one source.
  const result: unknown = await res.json().catch(() => null);
  if (isObject(result) && "defaults" in result && "revisions" in result) return normalizeConfigResponse(result);
  return getSystemConfig(systemId);
}

// ---------------------------------------------------------------------------
// Stats history

export type StatsRange = "6h" | "24h";

/** Sampled stats rows, oldest first. The server answers {since, step, data: [{ts, stats}]}. */
export async function getSystemStats(systemId: string | number, since: string, step: string): Promise<StatsSample[]> {
  const q = new URLSearchParams({ since, step });
  const res = await api.get<unknown>(`/systems/${systemId}/stats?${q.toString()}`);
  return unwrapList(res)
    .map(normalizeStatsSample)
    .filter(notNull)
    .sort((a, b) => Date.parse(a.ts) - Date.parse(b.ts));
}

// ---------------------------------------------------------------------------
// Logs and events

export interface LogRow {
  id: string;
  ts: string;
  level: string;
  source: string;
  message: string;
  raw: Record<string, unknown> | null;
}

export function normalizeLogRow(raw: unknown): LogRow | null {
  const obj = parseJSONField<Record<string, unknown>>(raw);
  if (!obj || !isObject(obj)) return null;
  const message = obj.message;
  if (typeof message !== "string") return null;
  const level = typeof obj.level === "string" ? obj.level.toLowerCase() : "info";
  const source = typeof obj.source === "string" ? obj.source : "";
  // Stored log rows carry no id; the recent endpoint and the stream's
  // backfill overlap, so build a stable key to drop duplicates.
  const id = obj.id != null ? String(obj.id) : `${String(obj.ts ?? "")}|${level}|${source}|${message}`;
  return {
    id,
    ts: toISO(obj.ts),
    level,
    source,
    message,
    raw: parseJSONField<Record<string, unknown>>(obj.raw),
  };
}

/** Parses one SSE `data:` payload from the log stream. */
export function parseLogRow(data: string): LogRow | null {
  try {
    return normalizeLogRow(JSON.parse(data));
  } catch {
    return null;
  }
}

export async function getRecentLogs(systemId: string | number, limit = 200): Promise<LogRow[]> {
  const res = await api.get<unknown>(`/systems/${systemId}/logs/recent?limit=${limit}`);
  return unwrapList(res).map(normalizeLogRow).filter(notNull);
}

export interface SystemEvent {
  id: string;
  type: string;
  severity: string;
  message: string;
  metadata: Record<string, unknown> | null;
  created_at: string;
}

export async function getSystemEvents(systemId: string | number, limit = 200): Promise<SystemEvent[]> {
  const res = await api.get<unknown>(`/systems/${systemId}/events?limit=${limit}`);
  return unwrapList(res)
    .map((r): SystemEvent | null => {
      const obj = parseJSONField<Record<string, unknown>>(r);
      if (!obj || !isObject(obj)) return null;
      return {
        id: String(obj.id ?? ""),
        type: typeof obj.type === "string" ? obj.type : "",
        severity: typeof obj.severity === "string" ? obj.severity.toLowerCase() : "info",
        message: typeof obj.message === "string" ? obj.message : "",
        metadata: parseJSONField<Record<string, unknown>>(obj.metadata),
        created_at: toISO(obj.created_at),
      };
    })
    .filter(notNull);
}

// ---------------------------------------------------------------------------
// Commands

export type CommandType = "restart" | "dongle_reset" | "update" | "capture";
export type CommandStatus = "queued" | "sent" | "progress" | "ok" | "error" | "expired";
export const ACTIVE_COMMAND_STATUSES: readonly CommandStatus[] = ["queued", "sent", "progress"];

export type CaptureKind = "spectrum" | "audio" | "iq";
export type CaptureStatus = "requested" | "recording" | "ready" | "error";
export const ACTIVE_CAPTURE_STATUSES: readonly CaptureStatus[] = ["requested", "recording"];

export const MAX_CAPTURE_SECONDS = 30;

export const VERSION_RE = /^v\d+\.\d+\.\d+$/;

export type CommandRequest =
  | { type: "restart" }
  | { type: "dongle_reset" }
  | { type: "update"; params: { version: string } }
  | { type: "capture"; params: { kind: CaptureKind; channel?: string; seconds?: number } };

export interface CommandRow {
  id: string;
  system_id: number;
  type: CommandType | string;
  params: Record<string, unknown> | null;
  requested_by: string;
  status: CommandStatus | string;
  stage: string;
  message: string;
  created_at: string;
  updated_at: string;
  /** The capture row a capture command created; only on the POST response. */
  capture: CaptureRow | null;
}

function normalizeCommand(raw: unknown): CommandRow | null {
  const obj = parseJSONField<Record<string, unknown>>(raw);
  if (!obj || !isObject(obj) || obj.id == null) return null;
  return {
    id: String(obj.id),
    system_id: toInt(obj.system_id, 0),
    type: typeof obj.type === "string" ? obj.type : "",
    params: parseJSONField<Record<string, unknown>>(obj.params),
    requested_by: obj.requested_by == null ? "" : String(obj.requested_by),
    status: typeof obj.status === "string" ? obj.status : "",
    stage: typeof obj.stage === "string" ? obj.stage : "",
    message: typeof obj.message === "string" ? obj.message : "",
    created_at: toISO(obj.created_at),
    updated_at: toISO(obj.updated_at),
    capture: normalizeCapture(obj.capture),
  };
}

export function isActiveCommand(c: CommandRow): boolean {
  return (ACTIVE_COMMAND_STATUSES as readonly string[]).includes(c.status);
}

/**
 * Queues a command. Validation failures (bad version tag, unknown channel,
 * seconds out of range) come back as 422 {errors: {field: reason}} and are
 * folded into the thrown message.
 */
export async function postCommand(systemId: string | number, req: CommandRequest): Promise<CommandRow> {
  const params = "params" in req ? req.params : {};
  const res = await adminFetch(`/systems/${systemId}/commands`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ type: req.type, params }),
  });
  if (res.status === 422) {
    const payload = (await res.json().catch(() => ({}))) as { errors?: unknown; error?: string };
    const errs = normalizeFieldErrors(payload.errors);
    const detail = Object.entries(errs)
      .map(([k, v]) => `${k}: ${v}`)
      .join("; ");
    throw new Error(detail || payload.error || "The server rejected the command");
  }
  if (!res.ok) throw new Error(await errorText(res));
  const cmd = normalizeCommand(await res.json());
  if (!cmd) throw new Error("The server did not return the command");
  return cmd;
}

export async function getCommands(systemId: string | number, limit = 50): Promise<CommandRow[]> {
  const res = await api.get<unknown>(`/systems/${systemId}/commands?limit=${limit}`);
  return unwrapList(res)
    .map(normalizeCommand)
    .filter(notNull)
    .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
}

// ---------------------------------------------------------------------------
// Captures

export interface CaptureRow {
  id: string;
  command_id: string | null;
  kind: CaptureKind | string;
  channel: string;
  seconds: number | null;
  sample_rate: number | null;
  status: CaptureStatus | string;
  path: string;
  size_bytes: number | null;
  error: string;
  requested_by: string;
  created_at: string;
  ready_at: string | null;
}

export interface SpectrumCapture {
  centre_hz: number;
  sample_rate: number;
  bins_db: number[];
}

function normalizeCapture(raw: unknown): CaptureRow | null {
  const obj = parseJSONField<Record<string, unknown>>(raw);
  if (!obj || !isObject(obj) || obj.id == null) return null;
  return {
    id: String(obj.id),
    command_id: obj.command_id == null ? null : String(obj.command_id),
    kind: typeof obj.kind === "string" ? obj.kind : "",
    channel: typeof obj.channel === "string" ? obj.channel : "",
    seconds: optInt(obj.seconds),
    sample_rate: optInt(obj.sample_rate),
    status: typeof obj.status === "string" ? obj.status : "",
    path: typeof obj.path === "string" ? obj.path : "",
    size_bytes: optInt(obj.size_bytes),
    error: typeof obj.error === "string" ? obj.error : "",
    requested_by: obj.requested_by == null ? "" : String(obj.requested_by),
    created_at: toISO(obj.created_at),
    ready_at: obj.ready_at == null ? null : toISO(obj.ready_at),
  };
}

export function isActiveCapture(c: CaptureRow): boolean {
  return (ACTIVE_CAPTURE_STATUSES as readonly string[]).includes(c.status);
}

export async function getCaptures(systemId: string | number): Promise<CaptureRow[]> {
  const res = await api.get<unknown>(`/systems/${systemId}/captures`);
  return unwrapList(res)
    .map(normalizeCapture)
    .filter(notNull)
    .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
}

export function captureDownloadPath(systemId: string | number, captureId: string): string {
  return `/systems/${systemId}/captures/${encodeURIComponent(captureId)}/download`;
}

/** Fetches a JSON capture (spectrum) with the admin token in the header. */
export async function fetchCaptureJSON<T>(systemId: string | number, captureId: string): Promise<T> {
  const res = await adminFetch(captureDownloadPath(systemId, captureId));
  if (!res.ok) throw new Error(await errorText(res));
  return (await res.json()) as T;
}

export function normalizeSpectrum(raw: unknown): SpectrumCapture | null {
  const obj = parseJSONField<Partial<SpectrumCapture>>(raw);
  if (!obj || !Array.isArray(obj.bins_db) || obj.bins_db.length === 0) return null;
  const bins = obj.bins_db.map((v) => (typeof v === "number" && Number.isFinite(v) ? v : NaN));
  if (bins.some((v) => Number.isNaN(v))) return null;
  return { centre_hz: toInt(obj.centre_hz, 0), sample_rate: toInt(obj.sample_rate, 0), bins_db: bins };
}

// ---------------------------------------------------------------------------
// Releases

export async function getLatestReceiverVersion(): Promise<string | null> {
  const res = await api.get<{ version?: unknown } | null>("/receivers/latest-version");
  return res && typeof res.version === "string" && res.version ? res.version : null;
}

// ---------------------------------------------------------------------------
// SSE and downloads: EventSource and <a href> cannot set headers, so the
// token travels as ?token=.

function withToken(path: string): string {
  const url = `${API_BASE}${path}`;
  const token = getToken();
  if (!token) return url;
  return `${url}${url.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`;
}

/** Opens an EventSource on an admin SSE endpoint, e.g. "/systems/3/stats/stream". */
export function openSSE(path: string): EventSource {
  return new EventSource(withToken(path));
}

/** URL for a file endpoint that can be opened in a new tab. */
export function downloadUrl(path: string): string {
  return withToken(path);
}

// ---------------------------------------------------------------------------
// Helpers

async function adminFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return fetch(`${API_BASE}${path}`, { ...init, headers });
}

async function errorText(res: Response): Promise<string> {
  const body = (await res.json().catch(() => null)) as { error?: unknown } | null;
  return body && typeof body.error === "string" && body.error ? body.error : `Request failed: ${res.status}`;
}

/** List endpoints answer {"data": [...]}; tolerate a bare array and Go's null slice too. */
function unwrapList(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  if (isObject(raw) && Array.isArray(raw.data)) return raw.data;
  return [];
}

/** Accepts an object, or a JSON string of one (jsonb cast to text), or nothing. */
export function parseJSONField<T>(v: unknown): T | null {
  if (v == null) return null;
  if (typeof v === "string") {
    const s = v.trim();
    if (!s) return null;
    try {
      return JSON.parse(s) as T;
    } catch {
      return null;
    }
  }
  if (typeof v === "object") return v as T;
  return null;
}

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message;
  return String(e);
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function notNull<T>(v: T | null | undefined): v is T {
  return v != null;
}

function toInt(v: unknown, fallback: number): number {
  if (typeof v === "number" && Number.isFinite(v)) return Math.trunc(v);
  if (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v))) return Math.trunc(Number(v));
  return fallback;
}

function optInt(v: unknown): number | null {
  if (v == null) return null;
  const n = toInt(v, Number.NaN);
  return Number.isNaN(n) ? null : n;
}

/** Timestamps arrive as ISO strings, or unix seconds/milliseconds from receiver clocks. */
function toISO(v: unknown): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" && Number.isFinite(v)) {
    const ms = v > 1e12 ? v : v * 1000;
    return new Date(ms).toISOString();
  }
  return "";
}
