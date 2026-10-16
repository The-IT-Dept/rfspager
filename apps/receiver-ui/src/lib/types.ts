/** Types for the receiver's local API, as in docs/receiver-local-ui.md. Times are unix ms unless noted. */

export interface Status {
  receiver: {
    hostname: string;
    version: string;
    os: string;
    arch: string;
    started_at: number;
    host_uptime_s: number;
  };
  /** Null until the server has welcomed this receiver since it started. */
  system: { name: string; uuid: string } | null;
  dongle: { open: boolean };
  /** Null on a legacy-only receiver. */
  upstream: { name: string; url: string; connected: boolean; rtt_ms: number } | null;
  webhooks: { total: number; enabled: number; failing: number };
  pages_today: number;
  last_page_at: number | null;
  cpu_temp_c: number;
  undervoltage_now: boolean;
  undervoltage_ever: boolean;
}

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
  dsp_load: number;
}

export interface HostStats {
  cpu_temp_c: number;
  load1: number;
  mem_free_mb: number;
  throttled_hex?: string;
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
  cw_chased?: number;
  pages: number;
  pages_truncated: number;
  last_page_age_s: number;
}

export interface StatsPoint {
  ts: number;
  interval_s: number;
  dongle: DongleStats;
  host: HostStats;
  link: LinkStats;
  channels: ChannelStats[] | null;
}

export interface StatsResponse {
  interval_s: number;
  latest: StatsPoint | null;
  points: StatsPoint[] | null;
}

export interface PageQuality {
  codewords: number;
  corrected: number;
  uncorrectable: number;
  truncated?: boolean;
  /** [start, end) byte ranges of `message` from unrepairable codewords. */
  damaged?: [number, number][] | null;
}

export interface Page {
  id: number;
  ts: number;
  channel: string;
  freq_hz: number;
  baud: number;
  address: string;
  function: number;
  message: string;
  posted: boolean;
  quality?: PageQuality | null;
}

export interface PagesResponse {
  pages: Page[] | null;
  more: boolean;
}

export type LogLevel = "debug" | "info" | "warn" | "error";

export interface LogEntry {
  id: number;
  ts: number;
  level: string;
  message: string;
  attrs?: Record<string, unknown> | null;
}

export interface Endpoint {
  name: string;
  mode: "ws" | "legacy" | string;
  url: string;
  primary: boolean;
  connected: boolean;
  connected_since: number | null;
  rtt_ms: number;
  reconnects_total: number;
  queue_depth: number;
  dropped_frames_total: number;
  pages_sent: number;
  pages_failed: number;
  last_page_at: number | null;
  last_error: string;
  last_error_at: number | null;
}

export type WebhookState = "ok" | "failing" | "idle" | "disabled";

export interface Webhook {
  id: string;
  name: string;
  url: string;
  enabled: boolean;
  has_secret: boolean;
  created_at: number;
  state: WebhookState;
  sent: number;
  failed: number;
  last_status: number;
  last_ok_at: number | null;
  last_error: string;
  last_error_at: number | null;
}

export interface WebhookInput {
  name: string;
  url: string;
  enabled: boolean;
  /** Create: optional. Update: omit to keep the current one, "" removes it. */
  secret?: string;
}

export interface WebhookTestResult {
  ok: boolean;
  status: number;
  error: string;
  duration_ms: number;
}

export interface ConnectionsResponse {
  endpoints: Endpoint[] | null;
  webhooks: Webhook[] | null;
}

export interface ChannelConfig {
  label: string;
  freq_hz: number;
  enabled: boolean;
}

export interface RuntimeConfig {
  tuner_gain_tenths_db: number;
  ppm: number;
  sample_rate: number;
  dongle_serial: string;
  dongle_index: number;
  post_all: boolean;
  log_level: string;
  stats_interval_s: number;
  channels: ChannelConfig[] | null;
}

export interface DongleInfo {
  index: number;
  name: string;
  manufacturer?: string;
  product?: string;
  serial?: string;
  tuner?: string;
}

export interface Settings {
  config: RuntimeConfig;
  config_version: number;
  config_source: string;
  build: { version: string; os: string; arch: string; hostname: string };
  host: { model?: string; kernel?: string; rtlsdr?: string } | null;
  dongles: DongleInfo[] | null;
  endpoints: { name: string; mode: string; url: string; primary: boolean }[] | null;
  state_dir: string;
  ui_listen: string;
}
