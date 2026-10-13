import { useEffect, useState } from "react";
import { openSSE, parseStatsSample, type StatsSample } from "~/lib/receiver-api";

export type StreamState = "off" | "connecting" | "live" | "reconnecting";

/**
 * Subscribes to GET /systems/{id}/stats/stream and keeps the newest frame.
 * The server backfills the last hour on connect, so the first frame lands
 * almost immediately for a receiver that has been reporting.
 */
export function useStatsStream(systemId: string | undefined, enabled: boolean) {
  const [latest, setLatest] = useState<StatsSample | null>(null);
  const [receivedAt, setReceivedAt] = useState<number | null>(null);
  const [state, setState] = useState<StreamState>("off");

  useEffect(() => {
    if (!systemId || !enabled) {
      setState("off");
      return;
    }
    setState("connecting");
    const es = openSSE(`/systems/${systemId}/stats/stream`);
    es.onopen = () => setState("live");
    es.onerror = () => setState("reconnecting");
    es.onmessage = (e: MessageEvent<string>) => {
      const sample = parseStatsSample(e.data);
      if (!sample) return;
      setLatest(sample);
      setReceivedAt(Date.now());
    };
    return () => es.close();
  }, [systemId, enabled]);

  return { latest, receivedAt, state };
}
