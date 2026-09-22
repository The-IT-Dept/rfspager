import { Link } from "react-router";
import type { Route } from "./+types/home";
import { LiveTicker } from "~/components/home/live-ticker";
import { SpectrumStrip } from "~/components/home/spectrum-strip";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const ONLINE_THRESHOLD_MS = 30 * 60_000;

interface SystemSummary {
  uuid: string;
  name: string;
  last_message_at: string | null;
}

export async function loader() {
  const [stats, messages, systems] = await Promise.all([
    fetch(`${API_BASE}/api/public/stats`).then((r) => (r.ok ? r.json() : null)).catch(() => null),
    fetch(`${API_BASE}/api/public/messages?page=1&per_page=12`).then((r) => (r.ok ? r.json() : null)).catch(() => null),
    fetch(`${API_BASE}/api/public/systems`).then((r) => (r.ok ? r.json() : [])).catch(() => []),
  ]);

  const sys: SystemSummary[] = Array.isArray(systems) ? systems : [];
  const online = sys.filter((s) => s.last_message_at && Date.now() - new Date(s.last_message_at).getTime() <= ONLINE_THRESHOLD_MS).length;

  return {
    stats,
    initialMessages: messages?.data ?? [],
    systemsTotal: sys.length,
    systemsOnline: online,
  };
}

export function meta() {
  return [
    { title: "RFSPager — Live NSW Emergency Pager Feed" },
    {
      name: "description",
      content:
        "We listen to the NSW Public Safety Network with our own SDR receivers and deliver every pager message to your phone in real time. Free for iOS and Android.",
    },
    { property: "og:title", content: "RFSPager — Live NSW Emergency Pager Feed" },
    {
      property: "og:description",
      content:
        "Real-time access to NSW PSN pager messages. Run on our own receiver network. Built for firefighters, volunteers, and communities.",
    },
    { property: "og:type", content: "website" },
    { property: "og:image", content: "/og-image.png" },
    { property: "og:image:width", content: "1200" },
    { property: "og:image:height", content: "630" },
    { name: "twitter:card", content: "summary_large_image" },
    { name: "twitter:image", content: "/og-image.png" },
  ];
}

const heading = "font-[family-name:var(--font-heading)]";

function fmt(n: number | null | undefined): string {
  if (n == null) return "—";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000).toLocaleString()}k`;
  return n.toLocaleString();
}

const PANORAMA_FRAMES = [
  { label: "Pager Feed", caption: "Every call-out, ranked.", from: "#F7241D", to: "#7a0e09" },
  { label: "Tap to Respond", caption: "Mark yourself rolling.", from: "#ff6b3a", to: "#7a2a10" },
  { label: "Live Map", caption: "Where it's happening.", from: "#1f7ad3", to: "#0a3461" },
  { label: "Incident Detail", caption: "Size, agency, status.", from: "#FF9500", to: "#7a4500" },
  { label: "Major Incidents", caption: "Watch / Act / Emergency.", from: "#dc2626", to: "#5a1010" },
  { label: "Alert Zones", caption: "Geofence your patch.", from: "#a855f7", to: "#3b1660" },
  { label: "Receiver Spectrum", caption: "Our SDR, your screen.", from: "#34d399", to: "#063b2c" },
];

export default function Home({ loaderData }: Route.ComponentProps) {
  const { stats, initialMessages, systemsTotal, systemsOnline } = loaderData;

  return (
    <>
      {/* ——— HERO ——— */}
      <section className="relative overflow-hidden bg-white">
        {/* Background flourishes */}
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_85%_15%,rgba(247,36,29,0.06),transparent_55%)]" />
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_15%_85%,rgba(31,122,211,0.05),transparent_55%)]" />

        <div className="max-w-[1400px] mx-auto px-6 pt-14 md:pt-20 lg:pt-24 pb-20 relative">
          <div className="grid lg:grid-cols-12 gap-10 lg:gap-16 items-center">
            {/* Copy */}
            <div className="lg:col-span-6 xl:col-span-7">
              <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-red-50 border border-red-100 mb-7">
                <span className="relative flex w-2 h-2">
                  <span className="absolute inline-flex h-full w-full rounded-full bg-[#F7241D] opacity-70 animate-ping" />
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-[#F7241D]" />
                </span>
                <span className="text-[11px] font-bold text-[#F7241D] uppercase tracking-[0.12em]">
                  Live · {systemsOnline}/{systemsTotal} receivers online
                </span>
              </div>

              <h1 className={`${heading} text-[2.75rem] sm:text-5xl md:text-6xl lg:text-[4.25rem] xl:text-[4.75rem] font-extrabold text-gray-900 leading-[1.02] tracking-[-0.02em] mb-6`}>
                Every NSW emergency
                <br />
                pager message.
                <br />
                <span className="text-[#F7241D]">As it happens.</span>
              </h1>

              <p className="text-lg md:text-xl text-gray-500 leading-relaxed max-w-xl mb-9">
                We listen to the NSW Public Safety Network with our own
                software-defined-radio receivers and stream every page to your
                phone — fires, ambulance, police, SES, hazmat. No scraping. No
                middlemen.
              </p>

              <div className="flex flex-col sm:flex-row gap-3 mb-7">
                <a
                  href="https://apps.apple.com/au/app/rfspager/id1568913265"
                  className="hover:scale-[1.03] transition-transform"
                >
                  <img src="/images/app_store.svg" alt="Download on App Store" className="h-14" />
                </a>
                <a
                  href="https://play.google.com/store/apps/details?id=io.devlan.rfspager"
                  className="hover:scale-[1.03] transition-transform"
                >
                  <img src="/images/play_store.svg" alt="Get it on Google Play" className="h-14" />
                </a>
              </div>

              <div className="flex flex-wrap items-center gap-x-6 gap-y-3 text-sm">
                <Link
                  to="/pager"
                  className="inline-flex items-center gap-1.5 font-semibold text-[#F7241D] hover:text-[#d91f19] transition-colors"
                >
                  Open the live feed
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M17 8l4 4m0 0l-4 4m4-4H3" />
                  </svg>
                </Link>
                <span className="text-gray-300">·</span>
                <Link to="/map" className="font-semibold text-gray-500 hover:text-gray-900 transition-colors">
                  View the map
                </Link>
              </div>
            </div>

            {/* Live ticker */}
            <div className="lg:col-span-6 xl:col-span-5">
              <LiveTicker initial={initialMessages} />
            </div>
          </div>
        </div>
      </section>

      {/* ——— STATS BAND ——— */}
      <section className="bg-[#F7241D] relative overflow-hidden">
        <div className="absolute inset-0 bg-[linear-gradient(115deg,rgba(0,0,0,0.18)_0%,transparent_40%,transparent_60%,rgba(0,0,0,0.18)_100%)]" />
        <div className="max-w-[1400px] mx-auto px-6 py-12 md:py-14 relative">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-x-6 gap-y-8">
            {[
              { value: fmt(stats?.capcode_count), label: "Capcodes monitored" },
              { value: fmt(stats?.message_count_30d), label: "Messages this month" },
              { value: fmt(stats?.incident_count), label: "Active major incidents" },
              { value: `${systemsOnline}/${systemsTotal}`, label: "Receivers online now" },
            ].map((s) => (
              <div key={s.label} className="text-center md:text-left">
                <p className={`${heading} text-4xl md:text-5xl font-extrabold text-white leading-none mb-2 tabular-nums`}>
                  {s.value}
                </p>
                <p className="text-xs md:text-sm text-red-100/90 font-medium uppercase tracking-wider">{s.label}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ——— RECEIVER NETWORK ——— */}
      <section className="bg-[#0a0a0c] text-white relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_70%_30%,rgba(255,60,60,0.08),transparent_60%)]" />
        <div className="max-w-[1400px] mx-auto px-6 py-24 md:py-32 relative">
          <div className="grid lg:grid-cols-12 gap-12 items-center">
            <div className="lg:col-span-6">
              <p className="text-xs font-bold text-[#F7241D] uppercase tracking-[0.2em] mb-4">
                The receiver network
              </p>
              <h2 className={`${heading} text-4xl md:text-5xl lg:text-6xl font-extrabold leading-[1.05] tracking-tight mb-6`}>
                We don't scrape <br />websites.
                <br />
                <span className="text-[#F7241D]">We listen to the radio.</span>
              </h2>
              <p className="text-lg text-white/60 leading-relaxed max-w-xl mb-8">
                Every message in RFSPager came off a real RF channel and through
                our own decoder stack. That's why the feed is seconds-old, not
                minutes-old, and why we know exactly when the network is up.
              </p>

              <div className="grid sm:grid-cols-3 gap-6 mb-10">
                {[
                  { k: "SDR", v: "Software-defined radio dongles tuned to the NSW PSN POCSAG carriers." },
                  { k: "Decoder", v: "Bare-metal Go services decode, dedupe and tag every page." },
                  { k: "Realtime", v: "Server-sent events to the app and this site, end-to-end < 1s." },
                ].map((b) => (
                  <div key={b.k}>
                    <p className={`${heading} text-sm font-bold text-[#F7241D] uppercase tracking-wider mb-2`}>
                      {b.k}
                    </p>
                    <p className="text-sm text-white/55 leading-relaxed">{b.v}</p>
                  </div>
                ))}
              </div>

              <Link
                to="/pager"
                className="inline-flex items-center gap-2 px-5 py-3 rounded-lg bg-white/10 hover:bg-white/15 border border-white/10 text-sm font-semibold transition-colors"
              >
                Watch the live feed
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M17 8l4 4m0 0l-4 4m4-4H3" />
                </svg>
              </Link>
            </div>

            <div className="lg:col-span-6">
              <div className="relative">
                <div className="absolute -inset-8 bg-[radial-gradient(ellipse_at_center,rgba(247,36,29,0.18),transparent_70%)] blur-3xl pointer-events-none" />
                <div className="relative bg-black/40 rounded-2xl ring-1 ring-white/10 p-5 shadow-2xl">
                  <div className="flex items-center justify-between mb-3 text-[11px] font-mono">
                    <span className="text-white/40">centre 148.625 MHz · span 12.5 MHz</span>
                    <span className="text-emerald-400 flex items-center gap-1.5">
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                      LIVE
                    </span>
                  </div>
                  <SpectrumStrip className="h-[260px] md:h-[320px]" />
                  <div className="flex items-center justify-between mt-3 text-[11px] font-mono text-white/40">
                    <span>−80 dB</span>
                    <span>frequency →</span>
                    <span>−40 dB</span>
                  </div>
                </div>

                <p className="text-xs text-white/40 mt-4 text-center font-mono tracking-wider">
                  Real RF waterfall, decoded into the feed above.
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ——— FEATURES ——— */}
      <section className="bg-white py-24 md:py-28">
        <div className="max-w-[1400px] mx-auto px-6">
          <div className="max-w-2xl mb-14">
            <p className="text-xs font-bold text-[#F7241D] uppercase tracking-[0.2em] mb-4">
              Built for action
            </p>
            <h2 className={`${heading} text-4xl md:text-5xl lg:text-6xl font-extrabold text-gray-900 leading-[1.05] tracking-tight mb-5`}>
              Everything a responder needs in one app.
            </h2>
            <p className="text-lg text-gray-500 leading-relaxed">
              From the first page to "in attendance", RFSPager keeps you ahead
              of the call.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-px bg-gray-100 rounded-2xl overflow-hidden border border-gray-100">
            {[
              {
                title: "Live pager feed",
                desc: "Every NSW PSN page in real time. Filter by agency, search by text, pause to read.",
                tag: "Free",
              },
              {
                title: "Map view",
                desc: "See incidents, hazard-reduction polygons and major fire perimeters on one map.",
                tag: "Free",
              },
              {
                title: "Major incidents",
                desc: "Watch / Act / Emergency tracking pulled live from the RFS feed. Get the size and status before you arrive.",
                tag: "Free",
              },
              {
                title: "Push notifications",
                desc: "Subscribe to specific capcodes — your station, your brigade, your truck — and get instant push.",
                tag: "Pro",
              },
              {
                title: "Alert zones",
                desc: "Drop a 1–100 km radius around home, work or family. Any incident with coordinates triggers an alert.",
                tag: "Pro",
              },
              {
                title: "Social response",
                desc: "Tap Respond on a page and your fellow members see you're rolling. No more guessing who's coming.",
                tag: "Free",
              },
            ].map((f) => (
              <div key={f.title} className="group bg-white p-8 hover:bg-gray-50 transition-colors relative">
                <div className="flex items-start justify-between mb-3">
                  <h3 className={`${heading} text-lg font-bold text-gray-900`}>{f.title}</h3>
                  <span
                    className={`text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full shrink-0 ml-3 ${
                      f.tag === "Pro"
                        ? "bg-[#F7241D] text-white"
                        : "bg-gray-100 text-gray-500"
                    }`}
                  >
                    {f.tag}
                  </span>
                </div>
                <p className="text-sm text-gray-500 leading-relaxed">{f.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ——— PANORAMA STRIP (App Store screenshot preview) ——— */}
      <section className="bg-gradient-to-b from-gray-50 via-white to-gray-50 py-24 md:py-28 overflow-hidden">
        <div className="max-w-[1400px] mx-auto px-6 mb-14">
          <p className="text-xs font-bold text-[#F7241D] uppercase tracking-[0.2em] mb-4">
            On your phone
          </p>
          <h2 className={`${heading} text-4xl md:text-5xl lg:text-6xl font-extrabold text-gray-900 leading-[1.05] tracking-tight max-w-3xl`}>
            From the first page to the spectrum it came from.
          </h2>
        </div>

        <div className="relative">
          {/* Edge gradient masks */}
          <div className="absolute inset-y-0 left-0 w-12 md:w-32 bg-gradient-to-r from-white via-white/80 to-transparent z-10 pointer-events-none" />
          <div className="absolute inset-y-0 right-0 w-12 md:w-32 bg-gradient-to-l from-gray-50 via-gray-50/80 to-transparent z-10 pointer-events-none" />

          <div className="overflow-x-auto pb-4 scrollbar-none">
            <div className="flex items-end gap-5 md:gap-7 px-6 md:px-20 min-w-max">
              {PANORAMA_FRAMES.map((f, i) => (
                <div
                  key={f.label}
                  className="flex-shrink-0 group"
                  style={{ transform: `translateY(${(i % 2 === 0 ? 0 : -12)}px)` }}
                >
                  <div className="relative w-[180px] md:w-[220px] h-[380px] md:h-[460px] rounded-[2.25rem] overflow-hidden ring-[6px] ring-gray-900 shadow-2xl bg-gray-900">
                    {/* Notch */}
                    <div className="absolute top-0 inset-x-0 h-7 z-10 flex justify-center items-end pb-1 bg-gray-900">
                      <div className="w-16 md:w-20 h-4 bg-black rounded-b-2xl" />
                    </div>
                    {/* Screen */}
                    <div
                      className="absolute inset-0"
                      style={{
                        background: `linear-gradient(160deg, ${f.from} 0%, ${f.to} 100%)`,
                      }}
                    />
                    <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,rgba(255,255,255,0.18),transparent_60%)]" />
                    <div className="absolute inset-0 flex flex-col items-center justify-end p-6 text-center">
                      <span className="text-[10px] font-mono text-white/50 uppercase tracking-[0.2em] mb-2">
                        Screen {i + 1}
                      </span>
                      <p className={`${heading} text-xl md:text-2xl font-extrabold text-white leading-tight mb-1`}>
                        {f.label}
                      </p>
                      <p className="text-xs text-white/70 leading-snug">
                        {f.caption}
                      </p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <p className="text-center text-xs text-gray-400 mt-8 max-w-md mx-auto px-6">
          Screenshots refreshed for v2 — coming this release.
        </p>
      </section>

      {/* ——— PRICING ——— */}
      <section className="bg-white py-24 md:py-28">
        <div className="max-w-5xl mx-auto px-6">
          <div className="text-center mb-14">
            <p className="text-xs font-bold text-[#F7241D] uppercase tracking-[0.2em] mb-4">Pricing</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900 leading-tight tracking-tight`}>
              Free to read. Pro to react.
            </h2>
          </div>

          <div className="grid md:grid-cols-2 gap-5">
            {/* Free */}
            <div className="rounded-2xl border border-gray-200 p-8 md:p-10">
              <p className="text-sm font-bold uppercase tracking-wider text-gray-400 mb-3">Free</p>
              <p className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900 mb-1`}>$0</p>
              <p className="text-sm text-gray-400 mb-8">Forever. No card needed.</p>
              <ul className="space-y-3 text-sm text-gray-700">
                {[
                  "Full live pager feed",
                  "Interactive map with major incidents",
                  "Hazard-reduction overlays",
                  "Mark yourself responding",
                ].map((x) => (
                  <li key={x} className="flex items-start gap-3">
                    <svg className="w-5 h-5 text-gray-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                    </svg>
                    {x}
                  </li>
                ))}
              </ul>
            </div>

            {/* Pro */}
            <div className="rounded-2xl bg-gray-950 text-white p-8 md:p-10 relative overflow-hidden ring-1 ring-[#F7241D]/40 shadow-2xl">
              <div className="absolute -top-12 -right-12 w-48 h-48 bg-[#F7241D]/20 rounded-full blur-3xl" />
              <p className="text-sm font-bold uppercase tracking-wider text-[#F7241D] mb-3">Pro</p>
              <div className="flex items-baseline gap-2 mb-1">
                <p className={`${heading} text-4xl md:text-5xl font-extrabold text-white`}>$4.99</p>
                <span className="text-sm text-white/40">/ month</span>
              </div>
              <p className="text-sm text-white/40 mb-8">Or annual. Cancel anytime.</p>
              <ul className="space-y-3 text-sm">
                {[
                  "Everything in Free",
                  "Push notifications for your capcodes",
                  "Alert zones (geofenced push)",
                  "Filter the feed by what matters",
                  "Show / hide unknown capcodes",
                  "Priority support",
                ].map((x) => (
                  <li key={x} className="flex items-start gap-3">
                    <svg className="w-5 h-5 text-[#F7241D] shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                    </svg>
                    {x}
                  </li>
                ))}
              </ul>
            </div>
          </div>

          <p className="text-center text-xs text-gray-400 mt-8">
            Subscriptions through the App Store and Google Play. Run the
            receiver network on your end? <Link to="/account" className="text-[#F7241D] font-semibold hover:underline">Contributor accounts available.</Link>
          </p>
        </div>
      </section>

      {/* ——— FAQ ——— */}
      <section className="bg-gray-50 py-24 md:py-28">
        <div className="max-w-3xl mx-auto px-6">
          <div className="mb-12">
            <p className="text-xs font-bold text-[#F7241D] uppercase tracking-[0.2em] mb-4">FAQ</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900 leading-tight tracking-tight`}>
              Things people ask.
            </h2>
          </div>

          <div className="space-y-3">
            {[
              {
                q: "Is RFSPager affiliated with NSW RFS?",
                a: "No. RFSPager is an independent app run by The IT Dept Pty Ltd. We receive the same public-safety pager broadcasts anyone with the right radio can — we just decode and surface them well.",
              },
              {
                q: "How fresh is the feed?",
                a: "Sub-second from receiver to your screen. Our SDR receivers across NSW decode pages in real time, hand them to a Go backend that dedupes and tags them, then push to your phone over SSE or APNs / FCM.",
              },
              {
                q: "What's the difference between Free and Pro?",
                a: "Free gives you the full live feed, the map, and major-incident tracking. Pro adds push notifications for the capcodes you care about, geographic alert zones, advanced filtering and priority support.",
              },
              {
                q: "Does it work outside NSW?",
                a: "Right now we cover the NSW Public Safety Network only. We're keeping receiver coverage tight rather than going wide — quality over quantity.",
              },
              {
                q: "How do Alert Zones work?",
                a: "Drop a point on the map, choose a radius from 1 to 100 km, and any incident with coordinates inside that zone triggers a push to your phone — perfect for home, work or family.",
              },
              {
                q: "What about my data?",
                a: "Encrypted in transit and at rest. We never share personal info with third parties. Push tokens are tied to your account and deleted on logout.",
              },
            ].map((faq) => (
              <details
                key={faq.q}
                className="group bg-white rounded-xl border border-gray-100 overflow-hidden hover:border-gray-200 transition-colors"
              >
                <summary className="flex items-center justify-between px-6 py-5 cursor-pointer text-[15px] font-semibold text-gray-900 list-none">
                  {faq.q}
                  <svg
                    className="w-5 h-5 text-gray-400 group-open:rotate-180 group-open:text-[#F7241D] transition-all shrink-0 ml-4"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 9l-7 7-7-7" />
                  </svg>
                </summary>
                <div className="px-6 pb-5 text-sm text-gray-500 leading-relaxed">{faq.a}</div>
              </details>
            ))}
          </div>

          <div className="text-center mt-12">
            <p className="text-gray-400 mb-1 text-sm">Still curious?</p>
            <a href="mailto:hello@rfspager.app" className="text-[#F7241D] font-semibold hover:underline">
              hello@rfspager.app
            </a>
          </div>
        </div>
      </section>

      {/* ——— CTA ——— */}
      <section className="bg-[#0a0a0c] text-white py-24 relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_50%_0%,rgba(247,36,29,0.2),transparent_60%)]" />
        <div className="max-w-3xl mx-auto px-6 text-center relative">
          <h2 className={`${heading} text-4xl md:text-5xl font-extrabold mb-5 leading-tight tracking-tight`}>
            Be the first to know.
          </h2>
          <p className="text-white/60 mb-9 text-lg leading-relaxed">
            Free on the App Store and Google Play. Pro unlocks push, zones and filters.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center mb-6">
            <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="hover:scale-[1.03] transition-transform">
              <img src="/images/app_store.svg" alt="App Store" className="h-14" />
            </a>
            <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="hover:scale-[1.03] transition-transform">
              <img src="/images/play_store.svg" alt="Google Play" className="h-14" />
            </a>
          </div>
          <Link
            to="/pager"
            className="inline-flex items-center gap-1.5 text-sm font-semibold text-white/70 hover:text-white transition-colors"
          >
            Or try the live feed in your browser first
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M17 8l4 4m0 0l-4 4m4-4H3" />
            </svg>
          </Link>
        </div>
      </section>
    </>
  );
}
