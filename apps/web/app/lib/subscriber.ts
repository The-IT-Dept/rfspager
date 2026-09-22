import { useEffect, useState } from "react";

/**
 * Whether the visitor is a paying subscriber.
 *
 * `null` means "not known yet" — a token exists but the lookup has not come
 * back. Callers that gate ads must treat null as "wait", not as "no": Laravel
 * decided this server-side and never showed a subscriber an ad, so flashing one
 * up for a moment while the fetch lands would be a regression they would
 * reasonably complain about.
 */
export function useIsSubscriber(): boolean | null {
  const [state, setState] = useState<boolean | null>(() =>
    typeof window === "undefined" ? null : localStorage.getItem("rfspager_token") ? null : false,
  );
  const [trigger, setTrigger] = useState(0);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onAuth = () => setTrigger((n) => n + 1);
    window.addEventListener("rfspager:auth", onAuth);
    return () => window.removeEventListener("rfspager:auth", onAuth);
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const token = localStorage.getItem("rfspager_token");
    if (!token) {
      setState(false); // anonymous visitors saw ads on the old site too
      return;
    }
    let cancelled = false;
    fetch("/api/v2/mobile/me", { headers: { Authorization: `Bearer ${token}` } })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!cancelled) setState(data?.subscription_valid === true);
      })
      .catch(() => {
        // A failed lookup must not silently turn a subscriber's ads back on.
        if (!cancelled) setState(true);
      });
    return () => {
      cancelled = true;
    };
  }, [trigger]);

  return state;
}
