/**
 * Types and helpers for the public coverage map (GET /api/public/coverage).
 * Shape documented in docs/plans/2026-10-14-public-coverage-and-hosting.md,
 * section 1. Unlike the admin report (./coverage), this carries no ids,
 * owners, licensees or model scores.
 */

export type CoverageDays = 30 | 90;
export const COVERAGE_DAYS: CoverageDays[] = [30, 90];
export const DEFAULT_COVERAGE_DAYS: CoverageDays = 90;

export interface PublicCoverageReceiver {
  name: string;
  lat: number;
  lng: number;
  last_heard_at: string | null;
}

export interface PublicCoverageTransmitter {
  lat: number;
  lng: number;
  heard: boolean;
  district: string | null;
}

export interface PublicCoverageDistrict {
  name: string;
  heard: boolean;
  pages: number;
  last_heard_at: string | null;
}

/** [lng, lat, weight] — weight is distinct incidents in the ~1 km cell. */
export type PublicHeatCell = [number, number, number];

export interface PublicCoverage {
  window: { days: number; since: string; until: string };
  generated_at: string;
  total_pages: number;
  receivers: PublicCoverageReceiver[];
  transmitters: PublicCoverageTransmitter[];
  districts: PublicCoverageDistrict[];
  heat: PublicHeatCell[];
}

export function coverageDays(raw: unknown): CoverageDays {
  return Number(raw) === 30 ? 30 : 90;
}

/**
 * Accept only something shaped like the documented response; anything else
 * (an HTML error page, a half-deployed server) is treated as "no data" so the
 * page still renders.
 */
export function parseCoverage(raw: unknown): PublicCoverage | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Partial<PublicCoverage>;
  return {
    window: r.window ?? { days: DEFAULT_COVERAGE_DAYS, since: "", until: "" },
    generated_at: r.generated_at ?? "",
    total_pages: typeof r.total_pages === "number" ? r.total_pages : 0,
    receivers: Array.isArray(r.receivers) ? r.receivers : [],
    transmitters: Array.isArray(r.transmitters) ? r.transmitters : [],
    districts: Array.isArray(r.districts) ? r.districts : [],
    heat: Array.isArray(r.heat) ? r.heat : [],
  };
}

/** Server-side (loader) fetch against API_URL. Never throws. */
export async function fetchCoverageSSR(apiBase: string, days: CoverageDays): Promise<PublicCoverage | null> {
  try {
    const res = await fetch(`${apiBase}/api/public/coverage?days=${days}`);
    if (!res.ok) return null;
    return parseCoverage(await res.json());
  } catch {
    return null;
  }
}

/** Browser fetch through the same-origin /api proxy. Throws on failure. */
export async function fetchCoverage(days: CoverageDays, signal?: AbortSignal): Promise<PublicCoverage> {
  const res = await fetch(`/api/public/coverage?days=${days}`, { signal });
  if (!res.ok) throw new Error(`coverage ${res.status}`);
  const parsed = parseCoverage(await res.json());
  if (!parsed) throw new Error("coverage: bad response");
  return parsed;
}

export interface CoverageSummary {
  receivers: number;
  districtsHeard: number;
  districtsTotal: number;
  pages: number;
  sitesHeard: number;
  sitesTotal: number;
}

export function summarise(data: PublicCoverage | null): CoverageSummary | null {
  if (!data) return null;
  return {
    receivers: data.receivers.length,
    districtsHeard: data.districts.filter((d) => d.heard).length,
    districtsTotal: data.districts.length,
    pages: data.total_pages,
    sitesHeard: data.transmitters.filter((t) => t.heard).length,
    sitesTotal: data.transmitters.length,
  };
}

// Teal for "we hear it" sits clear of brand red (receivers) and the
// blue/violet heat ramp; grey for sites we can't hear yet.
export const HEARD_COLOR = "#0d9488";
export const NOT_HEARD_COLOR = "#9ca3af";
export const RECEIVER_COLOR = "#ff1003";
