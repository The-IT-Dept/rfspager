/**
 * Label for a capcode row's Filter / Notify button. The active state is spelt
 * out rather than carried by colour alone, so "is red on or off?" never comes up.
 */
export function capcodeActionLabel(kind: "filter" | "notify", active: boolean): string {
  if (kind === "filter") return active ? "Filtering" : "Filter"
  return active ? "Notifying" : "Notify"
}
