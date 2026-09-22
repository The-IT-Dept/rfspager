import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

import { AuthShell, Field, Notice, SubmitButton, linkClass, safeNext } from "~/components/auth/auth-shell";

export function meta() {
  return [{ title: "Sign in — RFSPager" }];
}

export default function Login() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = safeNext(searchParams.get("next"), "/pager");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/v2/mobile/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setError(data.error || "That email and password don't match.");
        return;
      }
      localStorage.setItem("rfspager_token", data.token);
      window.dispatchEvent(new Event("rfspager:auth"));
      navigate(next);
    } catch {
      setError("Couldn't reach the server. Check your connection and try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell
      title="Sign in"
      intro="Use the same account as the RFSPager app."
      footer={
        <>
          New here?{" "}
          <Link to="/register" className={linkClass}>
            Create an account
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-5">
        {searchParams.get("reset") === "1" && <Notice tone="success">Your password is reset. Sign in with the new one.</Notice>}
        {error && <Notice tone="error">{error}</Notice>}
        <Field
          id="email"
          label="Email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          autoComplete="email"
          placeholder="you@example.com"
        />
        <Field
          id="password"
          label="Password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          autoComplete="current-password"
          trailing={
            <Link to="/forgot-password" className="text-xs font-semibold text-[#F7241D] hover:underline">
              Forgot it?
            </Link>
          }
        />
        <SubmitButton busy={loading} busyLabel="Signing in…">
          Sign in
        </SubmitButton>
      </form>
    </AuthShell>
  );
}
