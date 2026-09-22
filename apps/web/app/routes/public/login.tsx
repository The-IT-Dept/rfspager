import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [{ title: "Sign In — RFSPager" }];
}

export default function Login() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
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
      const data = await res.json();
      if (!res.ok) { setError(data.error || "Invalid email or password"); return; }
      if (typeof window !== "undefined") {
        localStorage.setItem("rfspager_token", data.token);
        window.dispatchEvent(new Event("rfspager:auth"));
      }
      navigate("/pager");
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
          <Link to="/" className="inline-block mb-6">
            <img src="/logo.svg" alt="RFSPager" className="h-14 mx-auto" />
          </Link>
          <h1 className={`${heading} text-2xl font-extrabold text-gray-900 mb-2`}>Welcome back</h1>
          <p className="text-sm text-gray-500">Sign in to access your RFSPager account</p>
        </div>

        <form onSubmit={handleSubmit} className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 space-y-5">
          {searchParams.get("reset") === "1" && (
            <div className="bg-green-50 text-green-800 text-sm px-4 py-3 rounded-xl font-medium">
              Your password has been reset. You can sign in now.
            </div>
          )}
          {error && (
            <div className="bg-red-50 text-red-700 text-sm px-4 py-3 rounded-xl font-medium">{error}</div>
          )}

          <div>
            <label htmlFor="email" className="block text-sm font-semibold text-gray-700 mb-1.5">Email</label>
            <input
              id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
              required autoComplete="email"
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="you@example.com"
            />
          </div>

          <div className="text-right -mt-2">
            <Link to="/forgot-password" className="text-sm text-[#F7241D] font-semibold hover:underline">Forgot your password?</Link>
          </div>

          <div>
            <label htmlFor="password" className="block text-sm font-semibold text-gray-700 mb-1.5">Password</label>
            <input
              id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              required autoComplete="current-password"
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="Your password"
            />
          </div>

          <button
            type="submit" disabled={loading}
            className="w-full bg-[#F7241D] text-white font-bold py-3 rounded-xl hover:bg-[#d91f19] transition-all shadow-sm hover:shadow-md disabled:opacity-50"
          >
            {loading ? "Signing in..." : "Sign In"}
          </button>
        </form>

        <p className="text-sm text-center text-gray-400 mt-6">
          Don't have an account? <Link to="/register" className="text-[#F7241D] font-semibold hover:underline">Create one</Link>
        </p>
      </div>
    </div>
  );
}
