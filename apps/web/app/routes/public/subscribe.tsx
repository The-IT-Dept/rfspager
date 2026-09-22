import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { Notice } from "~/components/auth/auth-shell";
import { customerApi, type CustomerUser } from "~/lib/customer-api";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [
    { title: "RFSPager Pro" },
    {
      name: "description",
      content: "RFSPager Pro adds alert zones, feed filtering, unidentified capcodes, no ads and priority support.",
    },
  ];
}

interface Prices {
  monthly: string;
  yearly: string;
}

// Mirrors apps/mobile/src/config/premium-features.ts, plus the ad removal the
// free tier's interstitials imply.
const PRO_INCLUDES = [
  "Push notifications for the capcodes you follow",
  "Alert zones: a 5 to 100 km circle that pushes when an incident lands inside it",
  "Filter the feed to only the capcodes you follow",
  "See pages from capcodes we have not identified yet",
  "No ads",
  "Priority support",
];

type PriceState = { kind: "loading" } | { kind: "ready"; prices: Prices } | { kind: "unavailable" };

export default function Subscribe() {
  const [searchParams] = useSearchParams();
  const success = searchParams.get("success") === "true";
  const canceled = searchParams.get("canceled") === "true";

  const [prices, setPrices] = useState<PriceState>({ kind: "loading" });
  const [busy, setBusy] = useState<string | null>(null);
  const [user, setUser] = useState<CustomerUser | null>(null);
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    fetch("/api/public/stripe/prices")
      .then((r) => (r.ok ? r.json() : null))
      .then((p: Prices | null) =>
        setPrices(p && p.monthly && p.yearly ? { kind: "ready", prices: p } : { kind: "unavailable" }),
      )
      .catch(() => setPrices({ kind: "unavailable" }));

    const token = localStorage.getItem("rfspager_token");
    setSignedIn(Boolean(token));
    if (token) {
      customerApi
        .me()
        .then(setUser)
        .catch(() => setSignedIn(false));
    }
  }, []);

  const post = async (path: string, body?: unknown) => {
    const token = localStorage.getItem("rfspager_token");
    if (!token) {
      window.location.href = "/login?next=/subscribe";
      return;
    }
    setError("");
    try {
      const res = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: body ? JSON.stringify(body) : undefined,
      });
      const data = await res.json().catch(() => ({}));
      if (data.url) window.location.href = data.url;
      else setError(data.error || "Checkout didn't start. Try again in a moment.");
    } catch {
      setError("Couldn't reach the server. Check your connection and try again.");
    } finally {
      setBusy(null);
    }
  };

  const checkout = (priceId: string) => {
    setBusy(priceId);
    void post("/api/v2/mobile/stripe/create-checkout-session", { price_id: priceId });
  };

  const manage = () => {
    setBusy("portal");
    void post("/api/v2/mobile/stripe/create-portal-session");
  };

  const isPro = Boolean(user?.subscription_valid);

  return (
    <div className="flex-1 bg-gray-50 py-14 md:py-20">
      <div className="mx-auto max-w-4xl px-4 sm:px-6">
        {success && <Notice tone="success">You're on Pro. Thanks for backing the receiver network.</Notice>}
        {canceled && (
          <p className="rounded-lg bg-amber-50 px-3.5 py-2.5 text-sm text-amber-900">
            Checkout was cancelled. Nothing was charged.
          </p>
        )}

        <div className={`${success || canceled ? "mt-8" : ""} max-w-2xl`}>
          <h1 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight text-gray-900 md:text-5xl`}>
            RFSPager Pro
          </h1>
          <p className="mt-5 text-lg leading-relaxed text-gray-500">
            Reading the feed and the map is free. Pro turns it into a pager — push for the capcodes you follow,
            and the tools to tune out everything else.
          </p>
        </div>

        {error && (
          <div className="mt-6">
            <Notice tone="error">{error}</Notice>
          </div>
        )}

        <div className="mt-10 grid gap-5 md:grid-cols-5">
          <div className="rounded-xl border border-gray-200 bg-white p-7 md:col-span-3">
            <h2 className={`${heading} text-lg font-bold text-gray-900`}>What Pro adds</h2>
            <ul className="mt-5 space-y-3 text-[15px] text-gray-700">
              {PRO_INCLUDES.map((x) => (
                <li key={x} className="flex items-start gap-3">
                  <svg className="mt-0.5 size-4 shrink-0 text-[#F7241D]" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden>
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                  </svg>
                  {x}
                </li>
              ))}
            </ul>
          </div>

          <div className="md:col-span-2">
            {isPro ? (
              <div className="rounded-xl bg-gray-950 p-7 text-white">
                <p className={`${heading} text-lg font-bold`}>You're on Pro</p>
                <p className="mt-2 text-sm leading-relaxed text-white/60">
                  {user?.subscription_platform === "stripe"
                    ? "Billing is handled here by card. Manage or cancel it any time."
                    : user?.subscription_platform === "apple"
                      ? "Billing is handled by the App Store. Manage it in your Apple subscriptions."
                      : user?.subscription_platform === "google"
                        ? "Billing is handled by Google Play. Manage it in your Play subscriptions."
                        : "Manage it wherever you subscribed."}
                </p>
                {user?.subscription_platform === "stripe" && (
                  <button
                    type="button"
                    onClick={manage}
                    disabled={busy === "portal"}
                    className="mt-6 h-10 w-full rounded-lg bg-white px-4 text-sm font-semibold text-gray-900 transition-colors hover:bg-gray-100 disabled:opacity-60"
                  >
                    {busy === "portal" ? "Opening…" : "Manage billing"}
                  </button>
                )}
                <Link to="/account" className="mt-4 inline-block text-sm font-semibold text-white/70 hover:text-white">
                  Account details
                </Link>
              </div>
            ) : prices.kind === "ready" ? (
              <div className="space-y-3">
                <PlanButton
                  title="Yearly"
                  price="$29.99"
                  per="a year"
                  note="Half the price of paying monthly."
                  busy={busy === prices.prices.yearly}
                  onClick={() => checkout(prices.prices.yearly)}
                  featured
                />
                <PlanButton
                  title="Monthly"
                  price="$4.99"
                  per="a month"
                  note="Cancel any time."
                  busy={busy === prices.prices.monthly}
                  onClick={() => checkout(prices.prices.monthly)}
                />
                {signedIn === false && (
                  <p className="text-xs leading-relaxed text-gray-500">
                    You'll be asked to{" "}
                    <Link to="/login?next=/subscribe" className="font-semibold text-[#F7241D] hover:underline">
                      sign in
                    </Link>{" "}
                    first so the subscription lands on your account.
                  </p>
                )}
              </div>
            ) : prices.kind === "loading" ? (
              <div className="h-40 animate-pulse rounded-xl border border-gray-200 bg-white motion-reduce:animate-none" aria-hidden />
            ) : (
              <div className="rounded-xl border border-gray-200 bg-white p-7">
                <p className={`${heading} text-lg font-bold text-gray-900`}>Subscribe in the app</p>
                <p className="mt-2 text-sm leading-relaxed text-gray-500">
                  Card checkout on this site isn't available right now. Pro is $4.99 a month or $29.99 a year in the
                  app, billed by the App Store or Google Play.
                </p>
                <div className="mt-5 flex flex-col gap-3">
                  <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="transition-opacity hover:opacity-85">
                    <img src="/images/app_store.svg" alt="Download on the App Store" className="h-12" />
                  </a>
                  <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="transition-opacity hover:opacity-85">
                    <img src="/images/play_store.svg" alt="Get it on Google Play" className="h-12" />
                  </a>
                </div>
              </div>
            )}
          </div>
        </div>

        <p className="mt-8 text-sm text-gray-500">
          Already subscribed in the app? Sign in with the same account and Pro carries over here.
        </p>
      </div>
    </div>
  );
}

function PlanButton({
  title,
  price,
  per,
  note,
  busy,
  onClick,
  featured = false,
}: {
  title: string;
  price: string;
  per: string;
  note: string;
  busy: boolean;
  onClick: () => void;
  featured?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      className={`w-full rounded-xl border p-5 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 disabled:opacity-60 ${
        featured
          ? "border-gray-950 bg-gray-950 text-white hover:bg-gray-900"
          : "border-gray-200 bg-white text-gray-900 hover:border-gray-300"
      }`}
    >
      <div className="flex items-baseline justify-between gap-3">
        <span className={`${heading} text-base font-bold`}>{title}</span>
        <span className={`${heading} text-xl font-extrabold tabular-nums`}>
          {price} <span className={`text-sm font-semibold ${featured ? "text-white/50" : "text-gray-500"}`}>{per}</span>
        </span>
      </div>
      <p className={`mt-1 text-sm ${featured ? "text-white/60" : "text-gray-500"}`}>{busy ? "Opening checkout…" : note}</p>
    </button>
  );
}
