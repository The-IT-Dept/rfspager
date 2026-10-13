import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { errorMessage, getSystemEvents, type SystemEvent } from "~/lib/receiver-api";
import { compactObject, fmtDate } from "./format";
import { SeverityBadge } from "./status-badge";

export function EventsTable({ systemId }: { systemId: string }) {
  const [events, setEvents] = useState<SystemEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setEvents(await getSystemEvents(systemId, 200));
      setError(null);
    } catch (e: unknown) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [systemId]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-3">
          <span>Events</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} /> Refresh
          </Button>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {error && <p className="mb-3 text-sm text-destructive">{error}</p>}
        {events === null && !error && <p className="text-sm text-muted-foreground">Loading…</p>}
        {events && events.length === 0 && <p className="text-sm text-muted-foreground">No events recorded for this system.</p>}
        {events && events.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Message</TableHead>
                <TableHead>Details</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((ev) => (
                <TableRow key={ev.id || `${ev.created_at}-${ev.type}`}>
                  <TableCell className="text-muted-foreground">{fmtDate(ev.created_at)}</TableCell>
                  <TableCell className="font-mono text-xs">{ev.type || "—"}</TableCell>
                  <TableCell>
                    <SeverityBadge severity={ev.severity} />
                  </TableCell>
                  <TableCell className="whitespace-normal">{ev.message || "—"}</TableCell>
                  <TableCell
                    className="max-w-xs truncate font-mono text-xs text-muted-foreground"
                    title={ev.metadata ? JSON.stringify(ev.metadata, null, 2) : undefined}
                  >
                    {compactObject(ev.metadata) || "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
