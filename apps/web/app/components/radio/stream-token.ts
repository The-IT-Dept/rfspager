import { useCallback, useEffect, useRef, useState } from "react";

import { getCustomerToken } from "~/lib/customer-api";

// The radio audio and event streams are loaded by <audio> and EventSource,
// which can't send our Authorization header. The server swaps the login token
// for a short-lived, radio-only one that can ride in the URL.

interface StreamToken {
  token: string;
  expiresAt: number;
}

// Refresh this long before expiry, so a URL built from the token still has
// time to be fetched.
const REFRESH_MARGIN_MS = 2 * 60_000;

export function useStreamToken(enabled: boolean) {
  const [token, setToken] = useState<string | null>(null);
  const current = useRef<StreamToken | null>(null);

  const refresh = useCallback(async (): Promise<string | null> => {
    const login = getCustomerToken();
    if (!login) return null;
    const res = await fetch("/api/v2/mobile/radio/stream-token", {
      method: "POST",
      headers: { Authorization: `Bearer ${login}` },
    }).catch(() => null);
    if (!res?.ok) return null;
    const body = (await res.json()) as { token: string; expires_at: string };
    current.current = { token: body.token, expiresAt: Date.parse(body.expires_at) };
    setToken(body.token);
    return body.token;
  }, []);

  useEffect(() => {
    if (!enabled) return;
    let timer: ReturnType<typeof setTimeout>;
    const schedule = async () => {
      await refresh();
      const exp = current.current?.expiresAt ?? Date.now() + 60_000;
      timer = setTimeout(schedule, Math.max(30_000, exp - Date.now() - REFRESH_MARGIN_MS));
    };
    schedule();
    return () => clearTimeout(timer);
  }, [enabled, refresh]);

  /** A token valid for at least a couple of minutes, fetching one if needed. */
  const fresh = useCallback(async () => {
    const t = current.current;
    if (t && t.expiresAt - Date.now() > REFRESH_MARGIN_MS) return t.token;
    return refresh();
  }, [refresh]);

  return { token, fresh };
}
