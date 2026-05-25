import { Link } from "react-router";
import type { Route } from "./+types/home";

const API_BASE = process.env.API_URL || "http://localhost:8080";

export async function loader() {
  try {
    const res = await fetch(`${API_BASE}/api/public/stats`);
    if (!res.ok) return { stats: null };
    return { stats: await res.json() };
  } catch {
    return { stats: null };
  }
}

export function meta() {
  return [
    { title: "RFSPager — Real-time NSW Emergency Services Pager Messages" },
    { name: "description", content: "Monitor NSW PSN Emergency Services pager messages in real-time. Push notifications, alert zones, major incident tracking. Free for iOS and Android." },
    { property: "og:title", content: "RFSPager — Real-time Emergency Alerts for NSW" },
    { property: "og:description", content: "Real-time access to NSW PSN pager messages. Built for firefighters, volunteers, and communities." },
  ];
}

const heading = "font-[family-name:var(--font-heading)]";

function fmt(n: number | null | undefined): string {
  if (!n) return "—";
  if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000).toLocaleString()}k+`;
  return n.toLocaleString();
}

export default function Home({ loaderData }: Route.ComponentProps) {
  const s = loaderData?.stats;

  return (
    <>
      {/* ——— HERO ——— */}
      <section className="relative overflow-hidden bg-gradient-to-br from-gray-50 via-white to-red-50/30">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,rgba(247,36,29,0.04),transparent_50%)]" />
        <div className="max-w-[1400px] mx-auto px-6 py-20 md:py-28 lg:py-36 relative">
          <div className="flex flex-col lg:flex-row items-center gap-16">
            {/* Copy */}
            <div className="flex-1 text-center lg:text-left">
              <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-red-50 border border-red-100 mb-6">
                <span className="w-2 h-2 rounded-full bg-[#F7241D] animate-pulse" />
                <span className="text-xs font-semibold text-[#F7241D] uppercase tracking-wider">Live Monitoring</span>
              </div>

              <h1 className={`${heading} text-5xl md:text-6xl lg:text-7xl font-extrabold text-gray-900 leading-[1.08] tracking-tight mb-6`}>
                Real-time emergency
                <br />
                <span className="text-[#F7241D]">alerts for NSW</span>
              </h1>

              <p className="text-lg md:text-xl text-gray-500 leading-relaxed max-w-lg mx-auto lg:mx-0 mb-10">
                Monitor NSW PSN Emergency Services pager messages as they happen.
                Built for firefighters, volunteers, and communities who need to stay informed.
              </p>

              <div className="flex flex-col sm:flex-row gap-4 justify-center lg:justify-start mb-8">
                <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="hover:scale-105 transition-transform">
                  <img src="/images/app_store.svg" alt="Download on App Store" className="h-14 mx-auto lg:mx-0" />
                </a>
                <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="hover:scale-105 transition-transform">
                  <img src="/images/play_store.svg" alt="Get it on Google Play" className="h-14 mx-auto lg:mx-0" />
                </a>
              </div>

              <Link
                to="/pager"
                className="inline-flex items-center gap-2 text-sm font-semibold text-[#F7241D] hover:text-[#d91f19] transition-colors"
              >
                View the live feed in your browser
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 8l4 4m0 0l-4 4m4-4H3" /></svg>
              </Link>
            </div>

            {/* Phone */}
            <div className="flex-shrink-0 w-[280px] md:w-[320px]">
              <div className="relative">
                <div className="rounded-[2.5rem] border-[6px] border-gray-900 shadow-2xl overflow-hidden bg-gray-900">
                  <div className="absolute top-0 inset-x-0 h-7 bg-gray-900 z-10 flex justify-center items-end pb-1">
                    <div className="w-20 h-5 bg-gray-800 rounded-b-2xl" />
                  </div>
                  <img src="/images/screenshot.png" alt="RFSPager App" className="w-full relative" />
                </div>
                <div className="absolute -top-6 -right-6 w-32 h-32 bg-[#F7241D]/10 rounded-full blur-2xl" />
                <div className="absolute -bottom-8 -left-8 w-40 h-40 bg-[#F7241D]/5 rounded-full blur-3xl" />
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ——— STATS ——— */}
      <section className="bg-[#F7241D] relative overflow-hidden">
        <div className="absolute inset-0 bg-[linear-gradient(135deg,rgba(0,0,0,0.1)_0%,transparent_50%)]" />
        <div className="max-w-[1400px] mx-auto px-6 py-16 relative">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-6 lg:gap-8">
            {[
              { value: s?.capcode_count, label: "Monitored Capcodes" },
              { value: s?.message_count_30d, label: "Messages This Month" },
              { value: s?.incident_count, label: "Active Incidents" },
              { value: 500, label: "Monthly Downloads" },
            ].map((stat) => (
              <div key={stat.label} className="text-center py-4">
                <p className={`${heading} text-4xl md:text-5xl font-extrabold text-white mb-1`}>{fmt(stat.value)}</p>
                <p className="text-sm text-red-200 font-medium">{stat.label}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ——— FEATURES ——— */}
      <section className="py-24 bg-white">
        <div className="max-w-[1400px] mx-auto px-6">
          <div className="text-center mb-16">
            <p className="text-sm font-bold text-[#F7241D] uppercase tracking-widest mb-3">Features</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900 mb-4`}>
              Everything you need to stay safe
            </h2>
            <p className="text-lg text-gray-500 max-w-2xl mx-auto">
              From real-time alerts to interactive mapping, RFSPager provides the tools
              emergency services professionals and communities rely on.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {[
              { icon: "🔔", title: "Push Notifications", desc: "Instant alerts for your subscribed capcodes. Never miss a call-out.", pro: true },
              { icon: "📍", title: "Alert Zones", desc: "Set geographic zones up to 100km. Get notified for any incident in your area.", pro: true },
              { icon: "🔥", title: "Major Incidents", desc: "Live tracking of NSW RFS major fires, floods, and hazard reduction burns." },
              { icon: "📻", title: "NSW PSN Radio", desc: "Live stream the NSW Public Safety Network radio with real-time call details." },
              { icon: "🤝", title: "Social Response", desc: "Mark yourself as responding so fellow users know you're on the way." },
              { icon: "📤", title: "Share Incidents", desc: "Quickly share incident details with your brigade, family, or community." },
            ].map((f) => (
              <div key={f.title} className="group relative bg-white border border-gray-100 rounded-2xl p-8 hover:border-[#F7241D]/20 hover:shadow-lg transition-all duration-300">
                <div className="text-3xl mb-4">{f.icon}</div>
                <h3 className={`${heading} text-lg font-bold text-gray-900 mb-2`}>{f.title}</h3>
                <p className="text-sm text-gray-500 leading-relaxed">{f.desc}</p>
                {f.pro && (
                  <span className="absolute top-6 right-6 text-[10px] font-bold text-[#F7241D] bg-red-50 px-2 py-0.5 rounded-full uppercase tracking-wider">Pro</span>
                )}
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ——— HOW IT WORKS ——— */}
      <section className="py-24 bg-gray-50">
        <div className="max-w-[1400px] mx-auto px-6">
          <div className="text-center mb-16">
            <p className="text-sm font-bold text-[#F7241D] uppercase tracking-widest mb-3">Get Started</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900`}>
              Up and running in minutes
            </h2>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-8 max-w-4xl mx-auto">
            {[
              { n: "01", title: "Download", desc: "Get RFSPager free from the App Store or Google Play." },
              { n: "02", title: "Personalise", desc: "Create your account and set up your notification preferences." },
              { n: "03", title: "Stay Informed", desc: "Receive real-time alerts and monitor incidents as they unfold." },
            ].map((step) => (
              <div key={step.n} className="text-center">
                <div className="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-[#F7241D] text-white text-lg font-extrabold mb-5">
                  {step.n}
                </div>
                <h3 className={`${heading} text-xl font-bold text-gray-900 mb-2`}>{step.title}</h3>
                <p className="text-sm text-gray-500">{step.desc}</p>
              </div>
            ))}
          </div>

          <div className="text-center mt-12">
            <div className="flex flex-col sm:flex-row gap-4 justify-center">
              <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="hover:scale-105 transition-transform">
                <img src="/images/app_store.svg" alt="App Store" className="h-12 mx-auto" />
              </a>
              <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="hover:scale-105 transition-transform">
                <img src="/images/play_store.svg" alt="Google Play" className="h-12 mx-auto" />
              </a>
            </div>
          </div>
        </div>
      </section>

      {/* ——— TESTIMONIALS ——— */}
      <section className="py-24 bg-white">
        <div className="max-w-[1400px] mx-auto px-6">
          <div className="text-center mb-16">
            <p className="text-sm font-bold text-[#F7241D] uppercase tracking-widest mb-3">Testimonials</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900`}>
              Trusted by emergency services
            </h2>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
            {[
              { quote: "RFSPager has completely changed how I stay informed about emergencies. The real-time notifications are invaluable for our volunteer brigade.", name: "Michael T.", role: "RFS Volunteer, Sydney" },
              { quote: "The Alert Zones feature is a game-changer. I can set up notifications for my home, workplace, and family locations all in one app.", name: "Sarah J.", role: "Community Member, Newcastle" },
              { quote: "As a station officer, I rely on RFSPager to track incidents across our district. The map view makes it easy to monitor multiple areas.", name: "David W.", role: "Station Officer, Wollongong" },
            ].map((t) => (
              <div key={t.name} className="bg-gray-50 rounded-2xl p-8 border border-gray-100">
                <svg className="w-8 h-8 text-[#F7241D]/20 mb-4" fill="currentColor" viewBox="0 0 24 24"><path d="M14.017 21v-7.391c0-5.704 3.731-9.57 8.983-10.609l.995 2.151c-2.432.917-3.995 3.638-3.995 5.849h4v10h-9.983zm-14.017 0v-7.391c0-5.704 3.748-9.57 9-10.609l.996 2.151c-2.433.917-3.996 3.638-3.996 5.849h3.983v10h-9.983z" /></svg>
                <p className="text-gray-600 leading-relaxed mb-6">{t.quote}</p>
                <div>
                  <p className="font-bold text-gray-900 text-sm">{t.name}</p>
                  <p className="text-xs text-gray-400">{t.role}</p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ——— FAQ ——— */}
      <section className="py-24 bg-gray-50">
        <div className="max-w-3xl mx-auto px-6">
          <div className="text-center mb-16">
            <p className="text-sm font-bold text-[#F7241D] uppercase tracking-widest mb-3">FAQ</p>
            <h2 className={`${heading} text-4xl md:text-5xl font-extrabold text-gray-900`}>
              Common questions
            </h2>
          </div>

          <div className="space-y-4">
            {[
              { q: "What is RFSPager?", a: "RFSPager monitors the NSW Public Safety Network and delivers real-time pager messages to your phone. It's built for emergency service personnel, volunteers, and anyone who wants to stay informed about incidents in their area." },
              { q: "What's included in RFSPager Pro?", a: "Pro unlocks push notifications for your subscribed capcodes, geographic alert zones (up to 100km radius), advanced filtering, and priority support. The free version gives you full access to the live pager feed." },
              { q: "How do Alert Zones work?", a: "Pick a point on the map and set a radius. You'll get a push notification any time an incident with coordinates falls within your zone — perfect for monitoring your home, workplace, or family locations." },
              { q: "Is my data secure?", a: "Yes. All data is encrypted in transit and at rest. We never share personal information with third parties. Push tokens are stored securely and deleted when you log out." },
            ].map((faq) => (
              <details key={faq.q} className="group bg-white rounded-xl border border-gray-100 overflow-hidden">
                <summary className="flex items-center justify-between px-6 py-5 cursor-pointer font-semibold text-gray-900 hover:bg-gray-50 transition-colors">
                  {faq.q}
                  <svg className="w-5 h-5 text-gray-400 group-open:rotate-180 transition-transform shrink-0 ml-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" /></svg>
                </summary>
                <div className="px-6 pb-5 text-sm text-gray-500 leading-relaxed">{faq.a}</div>
              </details>
            ))}
          </div>

          <div className="text-center mt-12">
            <p className="text-gray-500 mb-2">Still have questions?</p>
            <a href="mailto:hello@rfspager.app" className="text-[#F7241D] font-semibold hover:underline">
              hello@rfspager.app
            </a>
          </div>
        </div>
      </section>

      {/* ——— CTA ——— */}
      <section className="py-20 bg-gray-900">
        <div className="max-w-3xl mx-auto px-6 text-center">
          <h2 className={`${heading} text-3xl md:text-4xl font-extrabold text-white mb-4`}>
            Ready to stay informed?
          </h2>
          <p className="text-gray-400 mb-8 text-lg">
            Download RFSPager now and never miss an emergency alert again.
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <a href="https://apps.apple.com/au/app/rfspager/id1568913265" className="hover:scale-105 transition-transform">
              <img src="/images/app_store.svg" alt="App Store" className="h-14 mx-auto" />
            </a>
            <a href="https://play.google.com/store/apps/details?id=io.devlan.rfspager" className="hover:scale-105 transition-transform">
              <img src="/images/play_store.svg" alt="Google Play" className="h-14 mx-auto" />
            </a>
          </div>
        </div>
      </section>
    </>
  );
}
