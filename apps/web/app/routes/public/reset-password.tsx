import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

import { AuthShell, Field, Notice, SubmitButton, linkClass } from "~/components/auth/auth-shell";

export function meta() {
  return [{ title: "Choose a new password — RFSPager" }];
}

export default function ResetPassword() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") || "";
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const mismatch = confirm.length > 0 && password !== confirm;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (password !== confirm) {
      setError("The two passwords don't match.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/v2/mobile/auth/reset-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, password, password_confirmation: confirm }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        setError(data.error || "This reset link is invalid or has expired.");
        return;
      }
      navigate("/login?reset=1");
    } catch {
      setError("Couldn't reach the server. Check your connection and try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell
      title="Choose a new password"
      footer={
        <Link to="/forgot-password" className={linkClass}>
          Request a new reset link
        </Link>
      }
    >
      {!token ? (
        <p className="text-sm leading-relaxed text-gray-600">
          This link is missing its token, so we can't tell which account it belongs to. Request a new reset link and open it
          from the email.
        </p>
      ) : (
        <form onSubmit={handleSubmit} className="space-y-5">
          {error && <Notice tone="error">{error}</Notice>}
          <Field
            id="password"
            label="New password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
            minLength={8}
            maxLength={72}
            autoComplete="new-password"
            hint="At least 8 characters."
          />
          <Field
            id="confirmation"
            label="Confirm new password"
            type="password"
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
            required
            maxLength={72}
            autoComplete="new-password"
            aria-invalid={mismatch || undefined}
            hint={mismatch ? "These don't match yet." : undefined}
          />
          <SubmitButton busy={loading} busyLabel="Saving…">
            Save new password
          </SubmitButton>
        </form>
      )}
    </AuthShell>
  );
}

// Recovery URLs contain bearer credentials; keep them out of caches/referrers.
export function headers() {
  return { "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" };
}
