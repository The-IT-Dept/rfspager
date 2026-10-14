// Client for the customer-facing mobile API at /api/v2/mobile/*.
//
// Distinct from `./api` (which targets the admin API at /api/v1/admin) because
// the JWT, storage key, and base path are all different. Both can be in use on
// the same page in theory, but in practice the web app never mixes them.

const API_BASE = "/api/v2/mobile";
const TOKEN_KEY = "rfspager_token";

export function getCustomerToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setCustomerToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
  window.dispatchEvent(new Event("rfspager:auth"));
}

export function clearCustomerToken() {
  localStorage.removeItem(TOKEN_KEY);
  window.dispatchEvent(new Event("rfspager:auth"));
}

export interface CustomerUser {
  id: number;
  user_id: string;
  name: string | null;
  email: string;
  subscription_valid: boolean;
  subscription_product_id: string | null;
  subscription_expiry: string | null;
  will_renew: boolean | null;
  subscription_platform: string | null;
  /** Free Pro for hosting a receiver; rolls forward while the receiver stays online. */
  host_comp_until?: string | null;
  notifications_count?: number;
  unread_notifications_count?: number;
}

export class CustomerApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function request<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const token = getCustomerToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "application/json",
  };
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    clearCustomerToken();
    throw new CustomerApiError("Unauthorized", 401);
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new CustomerApiError(err.error || `Request failed: ${res.status}`, res.status);
  }

  if (res.status === 204) return {} as T;
  return res.json();
}

export const customerApi = {
  me: () => request<CustomerUser>("GET", "/me"),

  updateProfile: (input: { name?: string; email?: string; current_password?: string }) =>
    request<CustomerUser>("PATCH", "/me", input),

  deleteAccount: (password: string) =>
    request<{ status: string }>("DELETE", "/me", { confirm: "DELETE", password }),

  updatePassword: (current_password: string, password: string, password_confirmation: string) =>
    request<{ status: string }>("POST", "/auth/update-password", {
      current_password,
      password,
      password_confirmation,
    }),

  logout: () => request<{ status: string }>("POST", "/auth/logout").catch(() => null),

  /** Apply to host a receiver. 422 `{error}` on validation. */
  createSystem: (input: CreateSystemInput) => request<CreatedSystem>("POST", "/systems", input),

  /** The signed-in user's receivers, pending and approved. */
  mySystems: () => request<MySystem[]>("GET", "/my-systems"),
};

/** "own": has a Pi, dongle and antenna. "needs": would host one if we help with the gear. */
export type HostHardware = "own" | "needs";

export interface CreateSystemInput {
  name: string;
  suburb: string;
  notes?: string;
  hardware: HostHardware;
  accepted_terms: true;
}

export interface CreatedSystem {
  id: number;
  uuid: string;
  name: string;
  suburb: string;
  is_pending: boolean;
  is_owner: boolean;
}

/** Row from GET /my-systems (internal/mobile/handlers.go MySystems). */
export interface MySystem {
  uuid: string;
  name: string;
  suburb: string | null;
  request_notes: string | null;
  hardware: HostHardware | null;
  /** Only set once approved. */
  api_key: string | null;
  approved_at: string | null;
  created_at: string | null;
  is_pending: boolean;
  last_message_at: string | null;
  /** Present when the system has an api_key. */
  install_command?: string;
}

// Friendly label for a product ID. Mirrors apps/mobile's useProductName so the
// two surfaces stay aligned.
export function productLabel(productId: string | null | undefined): string {
  if (!productId) return "Not subscribed";
  if (productId.endsWith(".monthly")) return "Monthly";
  if (productId.endsWith(".yearly")) return "Yearly";
  if (productId.endsWith(".gifted")) return "Gifted";
  if (productId.endsWith(".host")) return "Free while hosting";
  return productId;
}
