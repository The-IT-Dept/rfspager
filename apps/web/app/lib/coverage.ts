/**
 * Types and helpers for the admin coverage map (GET /api/v1/admin/coverage).
 * Shape documented in docs/plans/2026-10-14-coverage-map.md ("API").
 */

export type CoverageChannel = "RFS" | "FRNSW";
export type CoverageChannelParam = CoverageChannel | "all";

export type TransmitterStatus = "probable" | "possible" | "district_heard" | "not_heard" | "unmapped";
export type LinkKind = "probable" | "possible";
export type DistrictSource = "auto" | "manual";

export interface CoverageWindow {
  days: number;
  since: string;
  until: string;
}

export interface CoverageReceiver {
  id: number;
  name: string;
  lat: number;
  lng: number;
  located_since: string | null;
  pages: number;
  districts_heard: number;
  reach_km: number | null;
  last_heard_at: string | null;
}

export interface CoverageTransmitter {
  id: number;
  channel: CoverageChannel;
  name: string;
  licensee: string;
  lat: number;
  lng: number;
  eirp_w: number | null;
  height_m: number | null;
  elevation_m: number | null;
  agency_id: number | null;
  agency: string | null;
  district_source: DistrictSource;
  district_confidence: number | null;
  status: TransmitterStatus;
  heard_by: number[];
  pages: number;
}

export interface CoverageLink {
  system_id: number;
  transmitter_id: number;
  kind: LinkKind;
  agency_id: number;
  pages: number;
  distance_km: number;
  score_db: number;
}

export interface CoverageDistrictReceiver {
  system_id: number;
  pages: number;
}

export interface CoverageDistrict {
  agency_id: number;
  agency: string;
  channel: CoverageChannel;
  pages: number;
  receivers: CoverageDistrictReceiver[];
  capcodes_heard: number;
  capcodes_known: number;
  /** Pages to unknown capcodes placed here by prefix; may include garbled decodes. */
  unknown_capcode_pages: number;
  transmitters: number;
  last_heard_at: string | null;
}

export interface UnmappedPrefix {
  prefix: string;
  pages: number;
}

/** [lng, lat, weight] — weight is distinct incidents in the ~1 km cell. */
export type HeatCell = [number, number, number];

export interface CoverageResponse {
  window: CoverageWindow;
  receptions_since: string | null;
  generated_at: string;
  /** Distinct pages (address + minute) in scope, resolved to a district or not. */
  total_pages: number;
  receivers: CoverageReceiver[];
  transmitters: CoverageTransmitter[];
  links: CoverageLink[];
  districts: CoverageDistrict[];
  unmapped_prefixes: UnmappedPrefix[];
  heat: HeatCell[];
}

/** Body for PUT /coverage/transmitters/{id}. */
export type TransmitterDistrictUpdate = { agency_id: number | null } | { auto: true };

export interface StatusStyle {
  label: string;
  fill: string;
  stroke: string;
  description: string;
}

// Greens for "we hear it", amber for "district heard, can't place the site",
// greys for silence. Kept clear of brand red (receivers) and the blue/violet
// heat ramp so the three layers never read as each other.
export const STATUS_STYLES: Record<TransmitterStatus, StatusStyle> = {
  probable: {
    label: "Probable",
    fill: "#15803d",
    stroke: "#ffffff",
    description: "Best-scoring site of a district this receiver hears",
  },
  possible: {
    label: "Possible",
    fill: "#4ade80",
    stroke: "#166534",
    description: "Within 6 dB of the probable site",
  },
  district_heard: {
    label: "District heard",
    fill: "#f59e0b",
    stroke: "#ffffff",
    description: "District heard in the window, site not attributed",
  },
  not_heard: {
    label: "Not heard",
    fill: "#9ca3af",
    stroke: "#ffffff",
    description: "District mapped, no pages in the window",
  },
  unmapped: {
    label: "Unmapped",
    fill: "#ffffff",
    stroke: "#9ca3af",
    description: "No district assigned",
  },
};

export const STATUS_ORDER: TransmitterStatus[] = ["probable", "possible", "district_heard", "not_heard", "unmapped"];

export const RECEIVER_COLOR = "#ff1003";
export const LINK_COLOR = "#334155";

// Sequential blue -> violet ramp for the incident heat layer (light = few).
export const HEAT_RAMP: Array<[number, string]> = [
  [0, "rgba(59, 130, 246, 0)"],
  [0.15, "rgba(147, 197, 253, 0.55)"],
  [0.35, "rgba(96, 165, 250, 0.7)"],
  [0.6, "rgba(79, 70, 229, 0.8)"],
  [0.85, "rgba(109, 40, 217, 0.88)"],
  [1, "rgba(76, 29, 149, 0.95)"],
];

/** Days since the timestamp, or null when missing/unparseable. */
export function daysSince(s: string | null | undefined, now: number = Date.now()): number | null {
  if (!s) return null;
  const t = new Date(s).getTime();
  if (Number.isNaN(t)) return null;
  return (now - t) / 86_400_000;
}

/** A district is flagged when it has never been heard or not in the last 14 days. */
export const STALE_DISTRICT_DAYS = 14;

export function isStaleDistrict(d: CoverageDistrict, now: number = Date.now()): boolean {
  const age = daysSince(d.last_heard_at, now);
  return age == null || age > STALE_DISTRICT_DAYS;
}

export function fmtEirp(w: number | null | undefined): string {
  if (w == null || !Number.isFinite(w)) return "—";
  return w >= 100 ? `${Math.round(w)} W` : `${w.toFixed(w < 10 ? 1 : 0)} W`;
}
