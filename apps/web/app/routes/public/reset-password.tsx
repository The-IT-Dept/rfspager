import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [{ title: "Choose a new password — RFSPager" }];
}

export default function ResetPassword() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") || "";
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (password !== confirmation) {
      setError("Passwords do not match.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/v2/mobile/auth/reset-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, password, password_confirmation: confirmation }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        setError(data.error || "This reset link is invalid or has expired.");
        return;
      }
      navigate("/login?reset=1");
    } catch {
      setError("Network error. Please try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex-1 flex items-center justify-center py-16 px-4 bg-gray-50">
      <div className="w-full max-w-[400px]">
        <div className="text-center mb-8">
          <Link to="/" className="inline-block mb-6"><img src="/logo.svg" alt="RFSPager" className="h-14 mx-auto" /></Link>
          <h1 className={`${heading} text-2xl font-extrabold text-gray-900 mb-2`}>Choose a new password</h1>
          <p className="text-sm text-gray-500">Use a new password to secure your RFSPager account.</p>
        </div>
        {!token ? (
          <div className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 text-center text-sm text-gray-600">This reset link is missing its token. Request a new password reset below.</div>
        ) : (
          <form onSubmit={handleSubmit} className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 space-y-5">
            {error && <div className="bg-red-50 text-red-700 text-sm px-4 py-3 rounded-xl font-medium">{error}</div>}
            <div>
              <label htmlFor="password" className="block text-sm font-semibold text-gray-700 mb-1.5">New password</label>
              <input id="password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required minLength={1} maxLength={72} autoComplete="new-password" className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all" />
            </div>
            <div>
              <label htmlFor="confirmation" className="block text-sm font-semibold text-gray-700 mb-1.5">Confirm new password</label>
              <input id="confirmation" type="password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required minLength={1} maxLength={72} autoComplete="new-password" className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all" />
            </div>
            <button type="submit" disabled={loading} className="w-full bg-[#F7241D] text-white font-bold py-3 rounded-xl hover:bg-[#d91f19] transition-all disabled:opacity-50">{loading ? "Resetting…" : "Reset password"}</button>
          </form>
        )}
        <p className="text-sm text-center text-gray-400 mt-6"><Link to="/forgot-password" className="text-[#F7241D] font-semibold hover:underline">Request a new reset link</Link></p>
      </div>
    </div>
  );
}

// Recovery URLs contain bearer credentials; keep them out of caches/referrers.
export function headers() {
  return { "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" };
}
