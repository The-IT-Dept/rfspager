import { Badge } from "~/components/ui/badge";

/**
 * What a host said about hardware when they applied (systems.hardware):
 * "own" has the gear, "needs" would host one if we help with it. Receivers
 * that predate the question have no answer.
 */
export function HardwareBadge({ value }: { value: unknown }) {
  if (value === "needs") {
    return <Badge className="bg-amber-100 text-amber-900 hover:bg-amber-100">Needs gear</Badge>;
  }
  if (value === "own") {
    return <Badge variant="secondary">Own gear</Badge>;
  }
  return <span className="text-muted-foreground">—</span>;
}
