import { Link } from "react-router";

import { CoverageCard } from "~/components/home/coverage-card";
import { LiveFeedPreview } from "~/components/home/live-feed-preview";
import { HEARD_RECENTLY_MS, type ReceiverSummary } from "~/components/home/receiver-network";
import type { Message } from "~/components/pager/types";
import { DEFAULT_COVERAGE_DAYS, fetchCoverageSSR } from "~/lib/public-coverage";
import type { Route } from "./+types/home";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

interface Stats {
  capcode_count: number;
  message_count_30d: number;
  incident_count: number;
}

export async function loader() {
  const [stats, messages, systems, coverage] = await Promise.all([
    fetch(`${API_BASE}/api/public/stats`).then((r) => (r.ok ? r.json() : null)).catch(() => null),
    fetch(`${API_BASE}/api/public/messages?per_page=8`).then((r) => (r.ok ? r.json() : null)).catch(() => null),
    fetch(`${API_BASE}/api/public/systems`).then((r) => (r.ok ? r.json() : [])).catch(() => []),
    fetchCoverageSSR(API_BASE, DEFAULT_COVERAGE_DAYS),
  ]);

  const receivers: ReceiverSummary[] = (Array.isArray(systems) ? systems : []).filter(
    (s: ReceiverSummary) => !s.is_pending,
  );
  const heardRecently = receivers.filter(
    (s) => s.last_message_at && Date.now() - new Date(s.last_message_at).getTime() <= HEARD_RECENTLY_MS,
  ).length;

  return {
    stats: (stats ?? null) as Stats | null,
    initialMessages: (messages?.data ?? []) as Message[],
    receivers,
    heardRecently,
    coverage,
  };
}

export function meta() {
  const description =
    "RFSPager runs its own radio receivers across NSW and streams every Public Safety Network pager message to your phone as it is sent. Free for iOS and Android.";
  return [
    { title: "RFSPager — NSW emergency pager messages, live" },
    { name: "description", content: description },
    { property: "og:title", content: "RFSPager — NSW emergency pager messages, live" },
    { property: "og:description", content: description },
    { property: "og:type", content: "website" },
    { property: "og:image", content: "/og-image.png" },
    { property: "og:image:width", content: "1200" },
    { property: "og:image:height", content: "630" },
    { name: "twitter:card", content: "summary_large_image" },
    { name: "twitter:image", content: "/og-image.png" },
  ];
}

function fmt(n: number | null | undefined): string {
  if (n == null) return "—";
  return n.toLocaleString("en-AU");
}

const FEATURES: Array<{ title: string; desc: string; tier: "Free" | "Pro" }> = [
  {
    title: "Live pager feed",
    desc: "Every page as it is decoded. Filter by agency, search the text, pause to read.",
    tier: "Free",
  },
  {
    title: "Map",
    desc: "Pages with a location, major incidents and hazard-reduction burns on one map.",
    tier: "Free",
  },
  {
    title: "Major incidents",
    desc: "Advice, Watch and Act and Emergency warnings from the RFS feed, refreshed every few minutes.",
    tier: "Free",
  },
  {
    title: "Push notifications",
    desc: "Follow the capcodes that matter to you and get a push the moment they are paged.",
    tier: "Pro",
  },
  {
    title: "Alert zones",
    desc: "Draw a 5 to 100 km circle around home or work and get a push when an incident with coordinates lands inside it.",
    tier: "Pro",
  },
  {
    title: "Respond",
    desc: "Tap Respond on a page and the rest of your brigade sees that you are on your way.",
    tier: "Free",
  },
];

const FREE_INCLUDES = ["The full live feed", "Map, major incidents and hazard reductions", "Respond to a page"];
// Mirrors apps/mobile/src/config/premium-features.ts, plus the ad removal the
// free tier's interstitials imply.
const PRO_INCLUDES = [
  "Push notifications for your capcodes",
  "Alert zones",
  "Filter the feed to only your capcodes",
  "See pages from unidentified capcodes",
  "No ads",
  "Priority support",
];

const FAQ: Array<{ q: string; a: string }> = [
  {
    q: "Is RFSPager affiliated with the NSW RFS?",
    a: "No. RFSPager is an independent app run by The IT Dept Pty Ltd. Pager traffic on the NSW Public Safety Network is broadcast over the air; we receive it with our own radios, decode it and present it well.",
  },
  {
    q: "Where do the messages come from?",
    a: "From our receivers. Each one is a small computer with a software-defined radio, hosted by a volunteer, tuned to the network's POCSAG carriers. It decodes each page and posts it to our server, which stores it and pushes it to the app and to this site. The coverage map shows where they are and which RFS districts they hear.",
  },
  {
    q: "How current is the feed?",
    a: "A page appears here after it has been transmitted, decoded by a receiver and stored by our server. That usually takes seconds. If a receiver goes quiet you will see it in the receiver list, because the list is driven by the pages we actually decoded.",
  },
  {
    q: "What does Pro add?",
    a: "Push notifications for the capcodes you follow, alert zones, filtering the feed to just your capcodes, pages from capcodes we have not identified yet, no ads, and priority support. Reading the live feed, the map and major incidents is free.",
  },
  {
    q: "Does it cover anywhere outside NSW?",
    a: "Not yet. Coverage follows where our receivers are, and today those are in NSW. If you would like to host one, see Host a receiver at rfspager.app/host.",
  },
  {
    q: "What happens to my data?",
    a: "Your account holds your email, name and the capcodes and zones you follow. Traffic to our servers is encrypted in transit. Signing out invalidates the session on that device. The privacy policy has the full detail.",
  },
];

export default function Home({ loaderData }: Route.ComponentProps) {
  const { stats, initialMessages, receivers, heardRecently, coverage } = loaderData;

  return (
    <>
      {/* Hero: the real feed beside the claim it proves. */}
      <section className="bg-white">
        <div className="mx-auto max-w-[1400px] px-4 pb-16 pt-12 sm:px-6 md:pt-16 lg:pb-24 lg:pt-20">
          <div className="grid items-start gap-10 lg:grid-cols-12 lg:gap-14">
            <div className="lg:col-span-6 xl:col-span-6">
              <p className="mb-6 flex items-center gap-2 text-sm text-gray-500">
                <span className="relative flex h-2 w-2" aria-hidden>
                  {heardRecently > 0 && (
                    <span className="absolute inline-flex h-full w-full rounded-full bg-[#F7241D] opacity-60 motion-safe:animate-ping" />
                  )}
                  <span className={`relative inline-flex h-2 w-2 rounded-full ${heardRecently > 0 ? "bg-[#F7241D]" : "bg-gray-300"}`} />
                </span>
                <span suppressHydrationWarning>
                  {heardRecently} of {receivers.length} receivers heard in the last 30 minutes
                </span>
              </p>

              <h1
                className={`${heading} text-[2.6rem] font-extrabold leading-[1.02] tracking-[-0.02em] text-gray-900 sm:text-5xl md:text-6xl lg:text-[4.25rem]`}
              >
                NSW emergency pages, straight off the air.
              </h1>

              <p className="mt-7 max-w-xl text-lg leading-relaxed text-gray-500 md:text-xl">
                RFSPager runs its own radio receivers across NSW. They decode Public Safety Network pager traffic —
                fire, rescue, SES — and hand every page to your phone and to this site as it is sent. Nothing is
                scraped from another website.
              </p>

              <div className="mt-9 flex flex-col gap-3 sm:flex-row">
                <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="transition-opacity hover:opacity-85">
                  <img src="/images/app_store.svg" alt="Download on the App Store" className="h-14" />
                </a>
                <a
                  href="https://play.google.com/store/apps/details?id=io.devlan.rfspager"
                  className="transition-opacity hover:opacity-85"
                >
                  <img src="/images/play_store.svg" alt="Get it on Google Play" className="h-14" />
                </a>
              </div>

              <div className="mt-7 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
                <Link to="/pager" className="font-semibold text-[#F7241D] hover:underline">
                  Open the live feed
                </Link>
                <Link to="/map" className="font-semibold text-gray-600 hover:text-gray-900">
                  See the map
                </Link>
                <Link to="/capcodes" className="font-semibold text-gray-600 hover:text-gray-900">
                  Browse capcodes
                </Link>
              </div>
            </div>

            <div className="lg:col-span-6">
              <LiveFeedPreview initial={initialMessages} />
            </div>
          </div>
        </div>
      </section>

      {/* Four figures we can stand behind, each labelled with what it counts. */}
      <section className="border-y border-gray-200 bg-white">
        <div className="mx-auto grid max-w-[1400px] grid-cols-2 divide-gray-200 px-4 sm:px-6 lg:grid-cols-4 lg:divide-x">
          {[
            { value: fmt(stats?.capcode_count), label: "Capcodes catalogued" },
            { value: fmt(stats?.message_count_30d), label: "Pages decoded in the last 30 days" },
            { value: fmt(stats?.incident_count), label: "Major incidents on record" },
            { value: `${heardRecently} of ${receivers.length}`, label: "Receivers heard in the last 30 minutes" },
          ].map((s) => (
            <div key={s.label} className="py-7 lg:px-8 lg:first:pl-0 lg:last:pr-0">
              <p className={`${heading} text-3xl font-extrabold tabular-nums leading-none text-gray-900 md:text-4xl`} suppressHydrationWarning>
                {s.value}
              </p>
              <p className="mt-2 text-sm text-gray-500">{s.label}</p>
            </div>
          ))}
        </div>
      </section>

      {/* The receiver network: the differentiator, shown rather than staged. */}
      <section className="bg-gray-950 text-white">
        <div className="mx-auto max-w-[1400px] px-4 py-20 sm:px-6 md:py-28">
          <div className="grid items-start gap-12 lg:grid-cols-12 lg:gap-16">
            <div className="lg:col-span-6">
              <h2 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight md:text-5xl`}>
                We don't scrape websites. We listen to the radio.
              </h2>
              <p className="mt-6 max-w-xl text-lg leading-relaxed text-white/60">
                Every message in RFSPager came off a real RF channel through a receiver we run. That is why the feed
                shows the page itself, why we can tell you which receiver decoded it, and why the coverage map
                is honest about where we can hear and where we can't yet.
              </p>

              <ol className="mt-10 grid gap-6 sm:grid-cols-3">
                {[
                  {
                    k: "Receive",
                    v: "A software-defined radio on a small computer, hosted by a volunteer, tuned to the network's POCSAG carriers.",
                  },
                  {
                    k: "Decode",
                    v: "The receiver turns the bursts into text and posts each page to our server, which stores it against the capcode it was sent to.",
                  },
                  {
                    k: "Deliver",
                    v: "The server streams the page to the app and to this site, and sends a push to anyone following that capcode.",
                  },
                ].map((step, i) => (
                  <li key={step.k}>
                    <p className={`${heading} text-sm font-bold text-white`}>
                      <span className="mr-2 text-white/40">{i + 1}</span>
                      {step.k}
                    </p>
                    <p className="mt-2 text-sm leading-relaxed text-white/55">{step.v}</p>
                  </li>
                ))}
              </ol>

              <div className="mt-10 flex flex-wrap items-center gap-x-6 gap-y-3 text-sm">
                <Link
                  to="/host"
                  className="inline-flex items-center rounded-lg border border-white/15 bg-white/10 px-4 py-2.5 font-semibold transition-colors hover:bg-white/15"
                >
                  Host a receiver
                </Link>
                <Link to="/pager" className="font-semibold text-white/70 hover:text-white">
                  Watch the live feed
                </Link>
              </div>
            </div>

            <div className="lg:col-span-6">
              <CoverageCard data={coverage} />
              <p className="mt-4 text-xs leading-relaxed text-white/40">
                A paging site counts as heard when our receivers decoded pages for its district in the window. Grey
                sites are ones we can't hear yet; a receiver nearby would fill them in.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* What the app does, tagged with the tier it is actually in. */}
      <section className="bg-white py-20 md:py-28">
        <div className="mx-auto max-w-[1400px] px-4 sm:px-6">
          <div className="max-w-2xl">
            <h2 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight text-gray-900 md:text-5xl`}>
              What the app does.
            </h2>
            <p className="mt-5 text-lg leading-relaxed text-gray-500">
              Reading the feed is free. Pro adds the alerting — push for the capcodes you follow, and the tools
              for people who watch a lot of traffic.
            </p>
          </div>

          <div className="mt-12 grid grid-cols-1 gap-px overflow-hidden rounded-xl border border-gray-200 bg-gray-200 md:grid-cols-2 lg:grid-cols-3">
            {FEATURES.map((f) => (
              <div key={f.title} className="bg-white p-7">
                <div className="flex items-start justify-between gap-3">
                  <h3 className={`${heading} text-lg font-bold text-gray-900`}>{f.title}</h3>
                  <span
                    className={`shrink-0 rounded-md px-2 py-0.5 text-xs font-semibold ${
                      f.tier === "Pro" ? "bg-[#F7241D] text-white" : "bg-gray-100 text-gray-600"
                    }`}
                  >
                    {f.tier}
                  </span>
                </div>
                <p className="mt-2 text-sm leading-relaxed text-gray-500">{f.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Pricing */}
      <section className="border-t border-gray-200 bg-gray-50 py-20 md:py-28">
        <div className="mx-auto max-w-5xl px-4 sm:px-6">
          <div className="max-w-2xl">
            <h2 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight text-gray-900 md:text-5xl`}>
              Free to read. Pro to tune out the noise.
            </h2>
          </div>

          <div className="mt-12 grid gap-5 md:grid-cols-2">
            <div className="rounded-xl border border-gray-200 bg-white p-8">
              <p className={`${heading} text-lg font-bold text-gray-900`}>Free</p>
              <p className={`${heading} mt-3 text-4xl font-extrabold text-gray-900`}>$0</p>
              <p className="mt-1 text-sm text-gray-500">No account needed to read the feed.</p>
              <ul className="mt-8 space-y-3 text-sm text-gray-700">
                {FREE_INCLUDES.map((x) => (
                  <li key={x} className="flex items-start gap-3">
                    <Tick className="text-gray-400" />
                    {x}
                  </li>
                ))}
              </ul>
            </div>

            <div className="rounded-xl bg-gray-950 p-8 text-white">
              <p className={`${heading} text-lg font-bold`}>Pro</p>
              <p className={`${heading} mt-3 text-4xl font-extrabold`}>
                $4.99 <span className="text-base font-semibold text-white/50">a month</span>
              </p>
              <p className="mt-1 text-sm text-white/50">or $29.99 a year. Cancel any time.</p>
              <ul className="mt-8 space-y-3 text-sm text-white/85">
                {PRO_INCLUDES.map((x) => (
                  <li key={x} className="flex items-start gap-3">
                    <Tick className="text-[#F7241D]" />
                    {x}
                  </li>
                ))}
              </ul>
              <Link
                to="/subscribe"
                className="mt-8 inline-flex h-10 items-center rounded-lg bg-[#F7241D] px-4 text-sm font-semibold text-white transition-colors hover:bg-[#d91f19]"
              >
                Go Pro
              </Link>
            </div>
          </div>

          <p className="mt-6 text-sm text-gray-500">
            Subscribe in the app through the App Store or Google Play, or on this site by card.
          </p>
        </div>
      </section>

      {/* FAQ */}
      <section className="bg-white py-20 md:py-28">
        <div className="mx-auto max-w-3xl px-4 sm:px-6">
          <h2 className={`${heading} text-4xl font-extrabold leading-tight tracking-tight text-gray-900 md:text-5xl`}>
            Questions people ask.
          </h2>

          <div className="mt-10 divide-y divide-gray-200 border-y border-gray-200">
            {FAQ.map((item) => (
              <details key={item.q} className="group">
                <summary className="flex cursor-pointer list-none items-center justify-between gap-4 py-5 text-[15px] font-semibold text-gray-900 outline-none focus-visible:text-[#F7241D] [&::-webkit-details-marker]:hidden">
                  {item.q}
                  <svg
                    className="size-5 shrink-0 text-gray-400 transition-transform group-open:rotate-180"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                    aria-hidden
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </summary>
                <p className="pb-6 text-[15px] leading-relaxed text-gray-600">{item.a}</p>
              </details>
            ))}
          </div>

          <p className="mt-10 text-sm text-gray-500">
            Something else?{" "}
            <a href="mailto:hello@rfspager.app" className="font-semibold text-[#F7241D] hover:underline">
              hello@rfspager.app
            </a>
          </p>
        </div>
      </section>

      {/* Closing */}
      <section className="border-t border-gray-200 bg-gray-50 py-20">
        <div className="mx-auto max-w-3xl px-4 text-center sm:px-6">
          <h2 className={`${heading} text-3xl font-extrabold leading-tight tracking-tight text-gray-900 md:text-4xl`}>
            Get the page before the siren.
          </h2>
          <p className="mx-auto mt-4 max-w-xl text-lg leading-relaxed text-gray-500">
            Free on iOS and Android. Or keep the live feed open in a browser tab.
          </p>
          <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="transition-opacity hover:opacity-85">
              <img src="/images/app_store.svg" alt="Download on the App Store" className="h-14" />
            </a>
            <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="transition-opacity hover:opacity-85">
              <img src="/images/play_store.svg" alt="Get it on Google Play" className="h-14" />
            </a>
          </div>
          <Link to="/pager" className="mt-6 inline-block text-sm font-semibold text-[#F7241D] hover:underline">
            Open the live feed
          </Link>
        </div>
      </section>
    </>
  );
}

function Tick({ className = "" }: { className?: string }) {
  return (
    <svg className={`mt-0.5 size-4 shrink-0 ${className}`} fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden>
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
    </svg>
  );
}
