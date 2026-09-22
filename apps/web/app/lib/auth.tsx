import { createContext, useContext, useState, useEffect, type ReactNode } from "react";
import { api, setToken, clearToken, isAuthenticated } from "./api";

interface AdminUser {
  id: number;
  email: string;
  name: string;
  role: string;
}

interface AuthContextValue {
  user: AdminUser | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AdminUser | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (isAuthenticated()) {
      api.get<AdminUser>("/auth/me").then(setUser).catch(() => clearToken()).finally(() => setLoading(false));
    } else {
      setLoading(false);
    }
  }, []);

  const login = async (email: string, password: string) => {
    const res = await api.post<{ token: string; user: AdminUser }>("/auth/login", { email, password });
    setToken(res.token);
    setUser(res.user);
  };

  const logout = async () => {
    const token = localStorage.getItem("admin_token");
    clearToken();
    setUser(null);
    if (!token) return;
    // Do not use api.post here: its 401 redirect handler would race local
    // logout and can recursively redirect an already-cleared session.
    try {
      await fetch("/api/v1/admin/auth/logout", {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
    } catch {
      // Local credentials are already cleared; a later expiry cannot restore
      // this session, so a network failure needs no retry UI.
    }
  };

  return <AuthContext.Provider value={{ user, loading, login, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
