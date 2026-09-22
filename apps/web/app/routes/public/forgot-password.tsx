import { useState } from "react";
import { Link } from "react-router";

import { AuthShell, Field, Notice, SubmitButton, linkClass } from "~/components/auth/auth-shell";

export function meta() {
  return [{ title: "Reset your password — RFSPager" }];
}

export default function ForgotPassword() {
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [sent, setSent] = useState(false);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/v2/mobile/auth/forgot-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        setError(data.error || "We couldn't start a reset. Try again in a moment.");
        return;
      }
      setSent(true);
    } catch {
      setError("Couldn't reach the server. Check your connection and try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell
      title="Reset your password"
      intro="Enter your account email and we'll send a link to choose a new one."
      footer={
        <Link to="/login" className={linkClass}>
          Back to sign in
        </Link>
      }
    >
      {sent ? (
        <div className="space-y-3">
          <Notice tone="success">If an account exists for {email}, a reset link is on its way.</Notice>
          <p className="text-sm leading-relaxed text-gray-500">
            The link expires after a short while. Nothing arrived? Check spam, or try again with the address you signed up
            with.
          </p>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="space-y-5">
          {error && <Notice tone="error">{error}</Notice>}
          <Field
            id="email"
            label="Email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
            autoComplete="email"
            placeholder="you@example.com"
          />
          <SubmitButton busy={loading} busyLabel="Sending…">
            Send reset link
          </SubmitButton>
        </form>
      )}
    </AuthShell>
  );
}
