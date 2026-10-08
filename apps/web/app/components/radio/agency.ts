// Each agency gets one colour, used only for the stripe that marks whose
// channel a call was on. Close to each service's own livery where that
// doesn't collide with the brand red, which the page reserves for "live".
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
};

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
};

export function agencyColour(agency: string | null | undefined): string {
  return (agency && AGENCY_COLOURS[agency]) || "#CED4DA";
}

export function agencyName(agency: string | null | undefined): string | null {
  if (!agency) return null;
  return AGENCY_NAMES[agency] ?? (agency === "OTHER" ? null : agency);
}
