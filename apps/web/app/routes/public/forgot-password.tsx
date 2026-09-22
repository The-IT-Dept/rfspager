import { useState } from "react";
import { BrandLockup } from "~/components/brand";
import { Link } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [{ title: "Reset password — RFSPager" }];
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
        setError(data.error || "We could not start password recovery. Please try again.");
        return;
      }
      setSent(true);
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
          <Link to="/" className="inline-block mb-6"><BrandLockup className="justify-center" markClassName="h-12 w-auto text-[#ff1003]" wordmarkClassName="h-10 w-auto" /></Link>
          <h1 className={`${heading} text-2xl font-extrabold text-gray-900 mb-2`}>Reset your password</h1>
          <p className="text-sm text-gray-500">Enter your account email and we’ll send a reset link.</p>
        </div>
        <form onSubmit={handleSubmit} className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 space-y-5">
          {sent ? (
            <div className="bg-green-50 text-green-800 text-sm px-4 py-3 rounded-xl font-medium">
              If that email exists, a reset link has been sent. Check your inbox.
            </div>
          ) : (
            <>
              {error && <div className="bg-red-50 text-red-700 text-sm px-4 py-3 rounded-xl font-medium">{error}</div>}
              <div>
                <label htmlFor="email" className="block text-sm font-semibold text-gray-700 mb-1.5">Email</label>
                <input id="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required autoComplete="email" placeholder="you@example.com" className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all" />
              </div>
              <button type="submit" disabled={loading} className="w-full bg-[#F7241D] text-white font-bold py-3 rounded-xl hover:bg-[#d91f19] transition-all disabled:opacity-50">
                {loading ? "Sending…" : "Send reset link"}
              </button>
            </>
          )}
        </form>
        <p className="text-sm text-center text-gray-400 mt-6"><Link to="/login" className="text-[#F7241D] font-semibold hover:underline">Back to sign in</Link></p>
      </div>
    </div>
  );
}
