import { useState, useEffect } from "react";
import { Link, useSearchParams } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [
    { title: "Subscribe to RFSPager Pro" },
    { name: "description", content: "Upgrade to RFSPager Pro for push notifications, alert zones, and advanced filtering." },
  ];
}

interface Prices {
  monthly: string;
  yearly: string;
}

export default function Subscribe() {
  const [searchParams] = useSearchParams();
  const success = searchParams.get("success") === "true";
  const canceled = searchParams.get("canceled") === "true";

  const [prices, setPrices] = useState<Prices | null>(null);
  const [loading, setLoading] = useState<string | null>(null);
  const [user, setUser] = useState<any>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    fetch("/api/public/stripe/prices").then(r => r.json()).then(setPrices).catch(() => {});

    const token = typeof window !== "undefined" ? localStorage.getItem("rfspager_token") : null;
    if (token) {
      fetch("/api/v2/mobile/me", { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.ok ? r.json() : null)
        .then(setUser)
        .catch(() => {});
    }
  }, []);

  const checkout = async (priceId: string) => {
    const token = localStorage.getItem("rfspager_token");
    if (!token) { window.location.href = "/login"; return; }

    setLoading(priceId);
    setError("");
    try {
      const res = await fetch("/api/v2/mobile/stripe/create-checkout-session", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify({ price_id: priceId }),
      });
      const data = await res.json();
      if (data.url) {
        window.location.href = data.url;
      } else {
        setError(data.error || "Failed to create checkout session");
      }
    } catch {
      setError("Network error");
    } finally {
      setLoading(null);
    }
  };

  const manageSubscription = async () => {
    const token = localStorage.getItem("rfspager_token");
    if (!token) return;

    setLoading("portal");
    try {
      const res = await fetch("/api/v2/mobile/stripe/create-portal-session", {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      });
      const data = await res.json();
      if (data.url) window.location.href = data.url;
      else setError(data.error || "Failed to open portal");
    } catch {
      setError("Network error");
    } finally {
      setLoading(null);
    }
  };

  const isSubscribed = user?.subscription_valid;

  return (
    <div className="flex-1 bg-gray-50 py-16">
      <div className="max-w-4xl mx-auto px-6">
        {/* Success/Cancel banners */}
        {success && (
          <div className="bg-green-50 border border-green-200 rounded-xl p-4 mb-8 text-center">
            <p className="text-green-800 font-semibold">Welcome to RFSPager Pro! Your subscription is now active.</p>
          </div>
        )}
        {canceled && (
          <div className="bg-yellow-50 border border-yellow-200 rounded-xl p-4 mb-8 text-center">
            <p className="text-yellow-800">Checkout was cancelled. You can try again anytime.</p>
          </div>
        )}

        <div className="text-center mb-12">
          <p className="text-sm font-bold text-[#F7241D] uppercase tracking-widest mb-3">Pricing</p>
          <h1 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900 mb-4`}>
            Upgrade to RFSPager Pro
          </h1>
          <p className="text-lg text-gray-500 max-w-xl mx-auto">
            Unlock push notifications, alert zones, advanced filtering, and more.
          </p>
        </div>

        {error && (
          <div className="bg-red-50 text-red-700 text-sm px-4 py-3 rounded-xl font-medium mb-6 text-center">{error}</div>
        )}

        {isSubscribed ? (
          <div className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 text-center max-w-md mx-auto">
            <div className="w-16 h-16 bg-green-50 rounded-2xl flex items-center justify-center mx-auto mb-4">
              <svg className="w-8 h-8 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" /></svg>
            </div>
            <h2 className={`${heading} text-xl font-bold text-gray-900 mb-2`}>You're subscribed!</h2>
            <p className="text-gray-500 mb-6">You have full access to all RFSPager Pro features.</p>
            <button
              onClick={manageSubscription}
              disabled={loading === "portal"}
              className="px-6 py-2.5 bg-gray-900 text-white font-semibold rounded-xl hover:bg-gray-800 transition-colors disabled:opacity-50"
            >
              {loading === "portal" ? "Loading..." : "Manage Subscription"}
            </button>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-2xl mx-auto">
            {/* Monthly */}
            <div className="bg-white rounded-2xl shadow-sm border border-gray-100 p-8 flex flex-col">
              <h3 className={`${heading} text-lg font-bold text-gray-900 mb-1`}>Monthly</h3>
              <p className="text-3xl font-extrabold text-gray-900 mb-1">
                $4.99<span className="text-base font-normal text-gray-400">/mo</span>
              </p>
              <p className="text-sm text-gray-400 mb-6">Cancel anytime</p>
              <ul className="space-y-3 text-sm text-gray-600 mb-8 flex-1">
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Push notifications</li>
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Alert zones (100km radius)</li>
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Advanced capcode filtering</li>
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Ad-free experience</li>
              </ul>
              <button
                onClick={() => prices?.monthly && checkout(prices.monthly)}
                disabled={!prices?.monthly || loading === prices?.monthly}
                className="w-full py-3 bg-[#F7241D] text-white font-bold rounded-xl hover:bg-[#d91f19] transition-colors disabled:opacity-50"
              >
                {loading === prices?.monthly ? "Loading..." : "Subscribe Monthly"}
              </button>
            </div>

            {/* Yearly */}
            <div className="bg-white rounded-2xl shadow-sm border-2 border-[#F7241D] p-8 flex flex-col relative">
              <span className="absolute -top-3 left-1/2 -translate-x-1/2 bg-[#F7241D] text-white text-xs font-bold px-3 py-1 rounded-full uppercase tracking-wider">Best Value</span>
              <h3 className={`${heading} text-lg font-bold text-gray-900 mb-1`}>Yearly</h3>
              <p className="text-3xl font-extrabold text-gray-900 mb-1">
                $29.99<span className="text-base font-normal text-gray-400">/yr</span>
              </p>
              <p className="text-sm text-green-600 font-medium mb-6">Save 50% vs monthly</p>
              <ul className="space-y-3 text-sm text-gray-600 mb-8 flex-1">
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Everything in Monthly</li>
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>Priority support</li>
                <li className="flex items-center gap-2"><svg className="w-4 h-4 text-green-500 shrink-0" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" /></svg>50% savings</li>
              </ul>
              <button
                onClick={() => prices?.yearly && checkout(prices.yearly)}
                disabled={!prices?.yearly || loading === prices?.yearly}
                className="w-full py-3 bg-[#F7241D] text-white font-bold rounded-xl hover:bg-[#d91f19] transition-colors disabled:opacity-50"
              >
                {loading === prices?.yearly ? "Loading..." : "Subscribe Yearly"}
              </button>
            </div>
          </div>
        )}

        {!user && (
          <p className="text-center text-sm text-gray-400 mt-8">
            You'll need to <Link to="/login" className="text-[#F7241D] font-semibold hover:underline">sign in</Link> or{" "}
            <Link to="/register" className="text-[#F7241D] font-semibold hover:underline">create an account</Link> before subscribing.
          </p>
        )}
      </div>
    </div>
  );
}
