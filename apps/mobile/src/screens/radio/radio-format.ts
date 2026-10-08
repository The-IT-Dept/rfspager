import moment from "moment"
import type { RadioCall } from "../../services/api-client"

// Shared with the web Radio page (apps/web/app/components/radio/agency.ts):
// each agency gets one colour, used only for the stripe that marks whose
// channel a call was on. The brand red is reserved for "live".
const AGENCY_COLOURS: Record<string, string> = {
  RFS: "#F59F00",
  FRNSW: "#B42318",
  SES: "#F76707",
  NSWA: "#0C8599",
  NSWPF: "#364FC7",
  TfNSW: "#495057",
  RMS: "#495057",
  NPWS: "#2F9E44",
  VRA: "#862E9C",
  MRNSW: "#1098AD",
}

const AGENCY_NAMES: Record<string, string> = {
  RFS: "Rural Fire Service",
  FRNSW: "Fire and Rescue NSW",
  SES: "State Emergency Service",
  NSWA: "NSW Ambulance",
  NSWPF: "NSW Police",
  TfNSW: "Transport for NSW",
  RMS: "Transport for NSW",
  NPWS: "National Parks",
  VRA: "Volunteer Rescue",
  MRNSW: "Marine Rescue",
}

export function agencyColour(agency: string | null | undefined): string {
  return (agency && AGENCY_COLOURS[agency]) || "#CED4DA"
}

export function agencyName(agency: string | null | undefined): string | null {
  if (!agency) return null
  return AGENCY_NAMES[agency] ?? (agency === "OTHER" ? null : agency)
}

export function callTitle(c: {
  tg_alpha_tag: string | null
  tgid: number | null
  target_rid: number | null
  patched_with?: string[] | null
}): string {
  let title: string
  if (c.tg_alpha_tag) title = c.tg_alpha_tag
  else if (c.tgid) title = `Talkgroup ${c.tgid}`
  else return c.target_rid ? `Private call to ${c.target_rid}` : "Private call"
  // A patched keyup is one call heard on several talkgroups.
  return [title, ...(c.patched_with ?? [])].join(" + ")
}

export function callSeconds(c: RadioCall): number | null {
  if (c.audio_ms) return c.audio_ms / 1000
  if (c.ended_at) return (Date.parse(c.ended_at) - Date.parse(c.started_at)) / 1000
  return null
}

export function formatSeconds(secs: number): string {
  return `${secs.toFixed(secs < 10 ? 1 : 0)} s`
}

export function clockTime(iso: string): string {
  return moment(iso).format("HH:mm:ss")
}

/** Local calendar day, yyyy-mm-dd. */
export function dayKey(iso: string): string {
  return moment(iso).format("YYYY-MM-DD")
}

export function dayLabel(key: string): string {
  const day = moment(key, "YYYY-MM-DD")
  if (day.isSame(moment(), "day")) return "Today"
  if (day.isSame(moment().subtract(1, "day"), "day")) return "Yesterday"
  return day.format("dddd D MMMM")
}

/**
 * The server stores a patched keyup once, but a copy can show up live
 * before the server knows it's one (its talker only arrives with the call's
 * end). Same radio within two seconds: keep the first.
 */
export function withoutPatchCopies(calls: RadioCall[]): RadioCall[] {
  const firstBySource = new Map<number, number[]>() // source → start times kept
  const byAge = [...calls].sort((a, b) => a.id - b.id)
  const drop = new Set<number>()
  for (const c of byAge) {
    if (!c.source_rid) continue
    const at = Date.parse(c.started_at)
    const kept = firstBySource.get(c.source_rid) ?? []
    if (kept.some((t) => Math.abs(t - at) < 2_000)) drop.add(c.id)
    else firstBySource.set(c.source_rid, [...kept, at])
  }
  return drop.size ? calls.filter((c) => !drop.has(c.id)) : calls
}
