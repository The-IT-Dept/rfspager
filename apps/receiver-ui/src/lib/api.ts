import type {
  ConnectionsResponse,
  LogEntry,
  PagesResponse,
  Settings,
  Status,
  StatsResponse,
  Webhook,
  WebhookInput,
  WebhookTestResult,
} from "./types";

export class ApiError extends Error {
  status: number;
  /** Field errors from a 422. */
  fields: Record<string, string>;
  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: { Accept: "application/json" } };
  if (method !== "GET") {
    // The receiver answers 415 to anything else; that is its CSRF guard.
    init.headers = { ...init.headers, "Content-Type": "application/json" };
    init.body = JSON.stringify(body ?? {});
  }
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch {
    throw new ApiError(0, "Cannot reach the receiver");
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const obj = (data ?? {}) as { error?: string; errors?: Record<string, string> };
    const fields = obj.errors ?? {};
    const msg =
      obj.error ||
      (Object.keys(fields).length ? Object.values(fields).join("; ") : "") ||
      `${res.status} ${res.statusText}`.trim();
    throw new ApiError(res.status, msg, fields);
  }
  return data as T;
}

function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === "" || v === false) continue;
    sp.set(k, v === true ? "1" : String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

export type StatsWindow = "1h" | "6h" | "24h";

export const api = {
  status: () => request<Status>("GET", "/api/status"),
  stats: (window: StatsWindow) => request<StatsResponse>("GET", `/api/stats${qs({ window })}`),
  pages: (p: { limit?: number; before?: number; filtered?: boolean; q?: string }) =>
    request<PagesResponse>("GET", `/api/pages${qs(p)}`),
  logs: (p: { limit?: number; level?: string }) =>
    request<{ logs: LogEntry[] | null }>("GET", `/api/logs${qs(p)}`),
  connections: () => request<ConnectionsResponse>("GET", "/api/connections"),
  settings: () => request<Settings>("GET", "/api/settings"),
  webhooks: () => request<{ webhooks: Webhook[] | null }>("GET", "/api/webhooks"),
  createWebhook: (w: WebhookInput) => request<Webhook>("POST", "/api/webhooks", w),
  updateWebhook: (id: string, w: WebhookInput) =>
    request<Webhook>("PUT", `/api/webhooks/${encodeURIComponent(id)}`, w),
  deleteWebhook: (id: string) => request<void>("DELETE", `/api/webhooks/${encodeURIComponent(id)}`),
  testWebhook: (id: string) =>
    request<WebhookTestResult>("POST", `/api/webhooks/${encodeURIComponent(id)}/test`),
};

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message;
  return String(e);
}
