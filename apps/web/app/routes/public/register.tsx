import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

import { AuthShell, Field, Notice, SubmitButton, linkClass, safeNext } from "~/components/auth/auth-shell";

export function meta() {
  return [{ title: "Create an account — RFSPager" }];
}

export default function Register() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  // Passed through raw to the sign-in link; sanitised by safeNext wherever it
  // is followed.
  const rawNext = searchParams.get("next");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const mismatch = confirm.length > 0 && password !== confirm;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password !== confirm) {
      setError("The two passwords don't match.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/v2/mobile/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, email, password, password_confirmation: confirm }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setError(data.error || "We couldn't create that account.");
        return;
      }
      localStorage.setItem("rfspager_token", data.token);
      window.dispatchEvent(new Event("rfspager:auth"));
      navigate(safeNext(rawNext, "/subscribe"));
    } catch {
      setError("Couldn't reach the server. Check your connection and try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell
      title="Create an account"
      intro="Free. It works in the app and on this site."
      footer={
        <>
          Already have one?{" "}
          <Link to={rawNext ? `/login?next=${encodeURIComponent(rawNext)}` : "/login"} className={linkClass}>
            Sign in
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-5">
        {error && <Notice tone="error">{error}</Notice>}
        <Field
          id="name"
          label="Name"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          autoComplete="name"
          hint="Shown to other members when you mark yourself responding."
        />
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
          minLength={8}
          autoComplete="new-password"
          hint="At least 8 characters."
        />
        <Field
          id="confirm"
          label="Confirm password"
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          required
          autoComplete="new-password"
          aria-invalid={mismatch || undefined}
          hint={mismatch ? "These don't match yet." : undefined}
        />
        <SubmitButton busy={loading} busyLabel="Creating account…">
          Create account
        </SubmitButton>
        <p className="text-center text-xs leading-relaxed text-gray-500">
          By continuing you agree to the{" "}
          <Link to="/terms-of-service" className="underline hover:text-gray-700">
            terms of service
          </Link>{" "}
          and{" "}
          <Link to="/privacy-policy" className="underline hover:text-gray-700">
            privacy policy
          </Link>
          .
        </p>
      </form>
    </AuthShell>
  );
}
