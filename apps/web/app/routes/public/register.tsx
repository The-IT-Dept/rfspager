import { useState } from "react";
import { BrandLockup } from "~/components/brand";
import { Link, useNavigate } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [{ title: "Create Account — RFSPager" }];
}

export default function Register() {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirmation, setPasswordConfirmation] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password !== passwordConfirmation) {
      setError("Passwords don't match");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await fetch("/api/v2/mobile/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, email, password, password_confirmation: passwordConfirmation }),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || "Registration failed");
        return;
      }
      if (typeof window !== "undefined") {
        localStorage.setItem("rfspager_token", data.token);
        window.dispatchEvent(new Event("rfspager:auth"));
      }
      navigate("/subscribe");
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
            <BrandLockup className="justify-center" markClassName="h-12 w-auto text-[#ff1003]" wordmarkClassName="h-10 w-auto" />
          </Link>
          <h1 className={`${heading} text-2xl font-extrabold text-gray-900 mb-2`}>Create your account</h1>
          <p className="text-sm text-gray-500">Get started with RFSPager — it's free</p>
        </div>

        <form onSubmit={handleSubmit} className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 space-y-4">
          {error && (
            <div className="bg-red-50 text-red-700 text-sm px-4 py-3 rounded-xl font-medium">{error}</div>
          )}

          <div>
            <label htmlFor="name" className="block text-sm font-semibold text-gray-700 mb-1.5">Name</label>
            <input
              id="name" type="text" value={name} onChange={(e) => setName(e.target.value)}
              required autoComplete="name"
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="Your name"
            />
          </div>

          <div>
            <label htmlFor="email" className="block text-sm font-semibold text-gray-700 mb-1.5">Email</label>
            <input
              id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
              required autoComplete="email"
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="you@example.com"
            />
          </div>

          <div>
            <label htmlFor="password" className="block text-sm font-semibold text-gray-700 mb-1.5">Password</label>
            <input
              id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              required autoComplete="new-password" minLength={8}
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="Min. 8 characters"
            />
          </div>

          <div>
            <label htmlFor="confirm" className="block text-sm font-semibold text-gray-700 mb-1.5">Confirm Password</label>
            <input
              id="confirm" type="password" value={passwordConfirmation} onChange={(e) => setPasswordConfirmation(e.target.value)}
              required autoComplete="new-password"
              className="w-full px-4 py-2.5 border border-gray-200 rounded-xl text-sm focus:ring-2 focus:ring-[#F7241D]/20 focus:border-[#F7241D] outline-none transition-all"
              placeholder="Confirm your password"
            />
          </div>

          <button
            type="submit" disabled={loading}
            className="w-full bg-[#F7241D] text-white font-bold py-3 rounded-xl hover:bg-[#d91f19] transition-all shadow-sm hover:shadow-md disabled:opacity-50 mt-2"
          >
            {loading ? "Creating account..." : "Create Account"}
          </button>
        </form>

        <p className="text-sm text-center text-gray-400 mt-6">
          Already have an account? <Link to="/login" className="text-[#F7241D] font-semibold hover:underline">Sign in</Link>
        </p>
      </div>
    </div>
  );
}
