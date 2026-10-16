import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, errorMessage } from "./api";
import { useEvent, useStreamEpoch } from "./events";
import type { Status } from "./types";

interface StatusCtx {
  status: Status | null;
  error: string | null;
  /** When the status last changed, for staleness. */
  at: number;
}

const Ctx = createContext<StatusCtx>({ status: null, error: null, at: 0 });

/** /api/status once, then every `status` event (5 s); refetched after a stream drop. */
export function StatusProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<StatusCtx>({ status: null, error: null, at: 0 });
  const epoch = useStreamEpoch();

  useEffect(() => {
    let cancelled = false;
    api
      .status()
      .then((s) => !cancelled && setState({ status: s, error: null, at: Date.now() }))
      .catch((e: unknown) => !cancelled && setState((p) => ({ ...p, error: errorMessage(e) })));
    return () => {
      cancelled = true;
    };
  }, [epoch]);

  useEvent("status", (s) => setState({ status: s, error: null, at: Date.now() }));

  return <Ctx.Provider value={state}>{children}</Ctx.Provider>;
}

export function useStatus(): StatusCtx {
  return useContext(Ctx);
}
