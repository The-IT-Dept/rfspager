import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router";

import { Field, Notice } from "~/components/auth/auth-shell";
import { ago } from "~/components/pager/time";
import {
  CustomerApiError,
  customerApi,
  getCustomerToken,
  type CreatedSystem,
  type CustomerUser,
  type MySystem,
} from "~/lib/customer-api";
import { HOSTING_TERMS_SECTIONS, HOSTING_TERMS_VERSION } from "~/lib/hosting-terms";

const heading = "font-[family-name:var(--font-heading)]";
const LOGIN_HREF = "/login?next=%2Fhost";
const REGISTER_HREF = "/register?next=%2Fhost";

const primaryBtn =
  "inline-flex h-10 items-center justify-center rounded-lg bg-[#F7241D] px-4 text-sm font-semibold text-white outline-none transition-colors hover:bg-[#d91f19] focus-visible:ring-2 focus-visible:ring-[#F7241D]/40 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-[#F7241D]";
const secondaryBtn =
  "inline-flex h-10 items-center justify-center rounded-lg border border-gray-200 bg-white px-4 text-sm font-semibold text-gray-800 outline-none transition-colors hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40";

export function meta() {
  const description =
    "Host an RFSPager receiver: a Raspberry Pi and an RTL-SDR dongle that listens to the NSW RFS paging network and fills a gap in our coverage.";
  return [
    { title: "Host a receiver — RFSPager" },
    { name: "description", content: description },
    { property: "og:title", content: "Host an RFSPager receiver" },
    { property: "og:description", content: description },
  ];
}

const NEEDS: Array<{ k: string; v: string }> = [
  { k: "Raspberry Pi 4 or 5", v: "With its own power supply and an SD card." },
  { k: "RTL-SDR or compatible USB dongle", v: "The radio. An RTL-SDR Blog V4 is a good choice." },
  { k: "VHF antenna", v: "The paging network is around 148 MHz. Higher and clearer of obstructions hears further." },
  { k: "Power, around the clock", v: "A receiver that is off can't hear the page that matters." },
  { k: "Internet", v: "Wired or reliable Wi-Fi. It sends text, so it uses very little data." },
];

const STEPS: Array<{ k: string; v: string }> = [
  { k: "Apply", v: "Tell us where the receiver will be. It takes two minutes." },
  { k: "We review", v: "A person reads every application, and we email you back." },
  { k: "Install", v: "Once approved, one command on the Pi sets up the receiver service." },
  { k: "On the map", v: "Pages it decodes show up in the feed, its area fills in on the coverage map, and Pro is free while it stays online." },
];

type AuthState =
  | { kind: "checking" }
  | { kind: "signed-out" }
  | { kind: "error" }
  | { kind: "signed-in"; user: CustomerUser };

export default function Host() {
  const [auth, setAuth] = useState<AuthState>({ kind: "checking" });
  const [systems, setSystems] = useState<MySystem[] | null>(null);
  const [systemsError, setSystemsError] = useState(false);
  const [submitted, setSubmitted] = useState<CreatedSystem | null>(null);
  const [authTick, setAuthTick] = useState(0);

  // Follow sign-in / sign-out from the header without a reload.
  useEffect(() => {
    const onAuth = () => setAuthTick((n) => n + 1);
    window.addEventListener("rfspager:auth", onAuth);
    return () => window.removeEventListener("rfspager:auth", onAuth);
  }, []);

  const loadSystems = useCallback(() => {
    setSystemsError(false);
    return customerApi
      .mySystems()
      .then((rows) => setSystems(Array.isArray(rows) ? rows : []))
      .catch((err: unknown) => {
        if (!(err instanceof CustomerApiError && err.status === 401)) setSystemsError(true);
      });
  }, []);

  useEffect(() => {
    if (!getCustomerToken()) {
      setAuth({ kind: "signed-out" });
      setSystems(null);
      return;
    }
    let cancelled = false;
    customerApi
      .me()
      .then((user) => {
        if (cancelled) return;
        setAuth({ kind: "signed-in", user });
        void loadSystems();
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        // A 401 clears the token, which fires rfspager:auth and lands here
        // again as signed-out.
        setAuth(err instanceof CustomerApiError && err.status === 401 ? { kind: "signed-out" } : { kind: "error" });
      });
    return () => {
      cancelled = true;
    };
  }, [authTick, loadSystems]);

  const onCreated = (created: CreatedSystem) => {
    setSubmitted(created);
    void loadSystems();
  };

  return (
    <div className="flex-1 bg-gray-50">
      <div className="mx-auto max-w-[1400px] px-4 py-12 sm:px-6 md:py-16">
        <div className="grid items-start gap-10 lg:grid-cols-12 lg:gap-14">
          <Intro />

          <div className="space-y-6 lg:col-span-7">
            {auth.kind === "checking" && (
              <div aria-busy className="h-72 animate-pulse rounded-xl border border-gray-200 bg-white motion-reduce:animate-none" />
            )}

            {auth.kind === "error" && (
              <Panel title="Apply to host">
                <Notice tone="error">Couldn't reach the server. Check your connection and try again.</Notice>
                <button type="button" onClick={() => setAuthTick((n) => n + 1)} className={`${secondaryBtn} mt-4`}>
                  Try again
                </button>
              </Panel>
            )}

            {auth.kind === "signed-out" && <SignedOut />}

            {auth.kind === "signed-in" && (
              <>
                {/* The confirmation leads; the refreshed list loads in under it. */}
                {submitted && <Success system={submitted} email={auth.user.email} onAnother={() => setSubmitted(null)} />}
                {(systemsError || (systems && systems.length > 0)) && (
                  <YourReceivers systems={systems ?? []} error={systemsError} onRetry={loadSystems} />
                )}
                {!submitted && <Wizard another={Boolean(systems && systems.length > 0)} onCreated={onCreated} />}
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function Intro() {
  return (
    <div className="lg:sticky lg:top-24 lg:col-span-5">
      <h1 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight text-gray-900 md:text-5xl`}>
        Host a receiver.
      </h1>
      <p className="mt-5 text-lg leading-relaxed text-gray-500">
        We don't scrape websites. We listen to the radio, through receivers hosted by people like you: a small
        computer and a radio dongle on a shelf, decoding the RFS paging network and passing every page to our
        server. Each one fills in a patch of the{" "}
        <Link to="/coverage" className="font-semibold text-[#F7241D] hover:underline">
          coverage map
        </Link>
        .
      </p>

      <div className="mt-6 rounded-xl border border-[#F7241D]/20 bg-[#F7241D]/[0.04] px-5 py-4">
        <p className={`${heading} text-sm font-bold text-gray-900`}>Pro is on us while you host.</p>
        <p className="mt-1 text-sm leading-relaxed text-gray-600">
          Keep your receiver online and decoding and your account gets RFSPager Pro free: push alerts, alert
          zones and filters. It starts once the receiver has been steadily online for a day, and only lapses if it
          stays offline for more than a week.
        </p>
      </div>

      <h2 className={`${heading} mt-10 text-base font-bold text-gray-900`}>What you need</h2>
      <ul className="mt-3 divide-y divide-gray-200 border-y border-gray-200">
        {NEEDS.map((n) => (
          <li key={n.k} className="flex items-start gap-3 py-3">
            <Tick />
            <p className="text-sm leading-relaxed text-gray-600">
              <span className="font-semibold text-gray-900">{n.k}.</span> {n.v}
            </p>
          </li>
        ))}
      </ul>
      <p className="mt-3 text-xs leading-relaxed text-gray-500">
        The hardware is yours: we don't supply or pay for it, and hosting is free.
      </p>

      <h2 className={`${heading} mt-10 text-base font-bold text-gray-900`}>How it works</h2>
      <ol className="mt-4 grid gap-5 sm:grid-cols-2">
        {STEPS.map((s, i) => (
          <li key={s.k}>
            <p className={`${heading} text-sm font-bold text-gray-900`}>
              <span className="mr-2 text-gray-400">{i + 1}</span>
              {s.k}
            </p>
            <p className="mt-1 text-sm leading-relaxed text-gray-500">{s.v}</p>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Panel({ title, intro, children }: { title: string; intro?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="rounded-xl border border-gray-200 bg-white p-6 sm:p-8">
      <h2 className={`${heading} text-xl font-extrabold tracking-tight text-gray-900`}>{title}</h2>
      {intro && <p className="mt-2 text-sm leading-relaxed text-gray-500">{intro}</p>}
      <div className="mt-6">{children}</div>
    </section>
  );
}

function SignedOut() {
  return (
    <Panel
      title="Apply to host"
      intro="You need an RFSPager account to apply. It's the same account as the app, and it's where we send the install command once you're approved."
    >
      <div className="flex flex-col gap-3 sm:flex-row">
        <Link to={LOGIN_HREF} className={primaryBtn}>
          Sign in to apply
        </Link>
        <Link to={REGISTER_HREF} className={secondaryBtn}>
          Create an account
        </Link>
      </div>
    </Panel>
  );
}

type Step = "hardware" | "terms" | "details";
const STEP_ORDER: Step[] = ["hardware", "terms", "details"];
const STEP_LABELS: Record<Step, string> = { hardware: "Hardware", terms: "Terms", details: "Details" };

function Stepper({ step }: { step: Step }) {
  const idx = STEP_ORDER.indexOf(step);
  return (
    <ol className="mb-7 grid grid-cols-3 gap-2" aria-label="Application steps">
      {STEP_ORDER.map((s, i) => (
        <li key={s} aria-current={i === idx ? "step" : undefined}>
          <span className={`block h-1 rounded-full ${i <= idx ? "bg-[#F7241D]" : "bg-gray-200"}`} />
          <span className={`mt-2 block text-xs font-semibold ${i === idx ? "text-gray-900" : "text-gray-400"}`}>
            {i + 1}. {STEP_LABELS[s]}
          </span>
        </li>
      ))}
    </ol>
  );
}

function CheckRow({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label
      className={`flex cursor-pointer items-start gap-3 rounded-lg border px-4 py-3 text-sm transition-colors ${
        checked ? "border-[#F7241D]/40 bg-red-50/60 text-gray-900" : "border-gray-200 bg-white text-gray-700 hover:bg-gray-50"
      }`}
    >
      <input type="checkbox" className="mt-0.5 size-4 shrink-0 accent-[#F7241D]" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span className="font-medium">{label}</span>
    </label>
  );
}

function Wizard({ another, onCreated }: { another: boolean; onCreated: (s: CreatedSystem) => void }) {
  const [step, setStep] = useState<Step>("hardware");
  const [hasPi, setHasPi] = useState(false);
  const [hasSdr, setHasSdr] = useState(false);
  const [accepted, setAccepted] = useState(false);
  const [name, setName] = useState("");
  const [suburb, setSuburb] = useState("");
  const [notes, setNotes] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const topRef = useRef<HTMLDivElement>(null);

  const go = (s: Step) => {
    setStep(s);
    setError("");
    // Keep the start of the step in view, mostly for phones after the long terms.
    requestAnimationFrame(() => {
      const el = topRef.current;
      if (el && el.getBoundingClientRect().top < 64) el.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    if (!name.trim() || !suburb.trim()) {
      setError("Name and suburb are both needed.");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const created = await customerApi.createSystem({
        name: name.trim(),
        suburb: suburb.trim(),
        notes: notes.trim() || undefined,
        accepted_terms: true,
      });
      onCreated(created);
    } catch (err: unknown) {
      if (err instanceof CustomerApiError && err.status === 401) {
        setError("Your session has expired. Sign in again to send the application.");
      } else if (err instanceof CustomerApiError && err.status !== 500) {
        setError(err.message);
      } else {
        setError("Couldn't send the application. Check your connection and try again.");
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div ref={topRef} className="scroll-mt-24">
      <Panel
        title={another ? "Apply for another receiver" : "Apply to host"}
        intro="Three short steps. We review every application by hand."
      >
        <Stepper step={step} />

        {step === "hardware" && (
          <div>
            <h3 className={`${heading} text-base font-bold text-gray-900`}>Do you have the hardware?</h3>
            <p className="mt-1.5 text-sm leading-relaxed text-gray-500">
              A receiver is a Raspberry Pi 4 or 5 and an RTL-SDR USB dongle with a VHF antenna, hosted wherever you want
              coverage. We don't provide hardware.
            </p>
            <div className="mt-5 space-y-2.5">
              <CheckRow label="I have a Raspberry Pi 4 or 5" checked={hasPi} onChange={setHasPi} />
              <CheckRow label="I have an RTL-SDR (or compatible) dongle and an antenna" checked={hasSdr} onChange={setHasSdr} />
            </div>
            <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-xs leading-relaxed text-gray-500">
                {hasPi && hasSdr ? "" : "You'll need both before we can approve a receiver. Come back when you've got them."}
              </p>
              <button type="button" disabled={!(hasPi && hasSdr)} onClick={() => go("terms")} className={`${primaryBtn} shrink-0`}>
                Continue
              </button>
            </div>
          </div>
        )}

        {step === "terms" && (
          <div>
            <h3 className={`${heading} text-base font-bold text-gray-900`}>Hosting terms</h3>
            <p className="mt-1.5 text-sm leading-relaxed text-gray-500">
              These are the rules every hosted receiver runs under. They're short, so please read them.
            </p>
            <div className="mt-5 space-y-4 rounded-lg border border-gray-200 bg-gray-50 p-4 sm:max-h-[22rem] sm:overflow-y-auto sm:p-5">
              {HOSTING_TERMS_SECTIONS.map((s) => (
                <div key={s.title}>
                  <p className="text-sm font-semibold text-gray-900">{s.title}</p>
                  <p className="mt-1 text-sm leading-relaxed text-gray-600">{s.body}</p>
                </div>
              ))}
              <p className="text-xs text-gray-400">Version {HOSTING_TERMS_VERSION}</p>
            </div>
            <div className="mt-4">
              <CheckRow label="I agree to these hosting terms" checked={accepted} onChange={setAccepted} />
            </div>
            <div className="mt-6 flex items-center justify-between gap-3">
              <button type="button" onClick={() => go("hardware")} className={secondaryBtn}>
                Back
              </button>
              <button type="button" disabled={!accepted} onClick={() => go("details")} className={primaryBtn}>
                Continue
              </button>
            </div>
          </div>
        )}

        {step === "details" && (
          <form onSubmit={submit}>
            <h3 className={`${heading} text-base font-bold text-gray-900`}>About the receiver</h3>
            <p className="mt-1.5 text-sm leading-relaxed text-gray-500">We read this when we review your application.</p>
            <div className="mt-5 space-y-5">
              {error && <Notice tone="error">{error}</Notice>}
              <Field
                id="host-name"
                label="Name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                maxLength={100}
                placeholder="e.g. Central Coast 03"
                hint="Appears on the public coverage map once approved, so don't use your street address."
              />
              <Field
                id="host-suburb"
                label="Suburb"
                value={suburb}
                onChange={(e) => setSuburb(e.target.value)}
                required
                maxLength={100}
                autoComplete="address-level2"
                placeholder="e.g. Gosford NSW"
                hint="Where the receiver will be. Suburb or town is enough."
              />
              <div>
                <label htmlFor="host-notes" className="mb-1.5 block text-sm font-semibold text-gray-800">
                  Notes <span className="font-normal text-gray-400">(optional)</span>
                </label>
                <textarea
                  id="host-notes"
                  value={notes}
                  onChange={(e) => setNotes(e.target.value)}
                  rows={4}
                  maxLength={2000}
                  placeholder="Why you want to host, which area it should cover, your antenna setup, anything we should know."
                  className="w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-2.5 text-sm text-gray-900 outline-none transition-colors placeholder:text-gray-400 focus:border-[#F7241D] focus:bg-white focus:ring-2 focus:ring-[#F7241D]/15"
                />
              </div>
            </div>
            <div className="mt-6 flex items-center justify-between gap-3">
              <button type="button" onClick={() => go("terms")} className={secondaryBtn} disabled={submitting}>
                Back
              </button>
              <button type="submit" disabled={submitting || !name.trim() || !suburb.trim()} className={primaryBtn}>
                {submitting ? "Sending…" : "Send application"}
              </button>
            </div>
          </form>
        )}
      </Panel>
    </div>
  );
}

function Success({ system, email, onAnother }: { system: CreatedSystem; email: string; onAnother: () => void }) {
  const ref = useRef<HTMLElement>(null);
  // It replaces a long form, so bring it into view (mostly matters on a phone).
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    if (r.top < 64 || r.top > window.innerHeight - 120) el.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);
  return (
    <section ref={ref} role="status" className="scroll-mt-24 rounded-xl border border-gray-200 bg-white p-6 sm:p-8">
      <span className="inline-flex size-10 items-center justify-center rounded-full bg-emerald-50 text-emerald-600" aria-hidden>
        <svg className="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
        </svg>
      </span>
      <h2 className={`${heading} mt-4 text-xl font-extrabold tracking-tight text-gray-900`}>Thanks — check your email.</h2>
      <p className="mt-2 text-sm leading-relaxed text-gray-600">
        We've got your application for <span className="font-semibold text-gray-900">{system.name}</span> in{" "}
        {system.suburb}, and we've sent a note to <span className="font-semibold text-gray-900">{email}</span>. We'll review
        it and reply in that same thread with the install command. Reply to it any time if you have questions.
      </p>
      <p className="mt-3 text-sm leading-relaxed text-gray-500">
        Nothing there in a few minutes? Check your spam folder, or write to{" "}
        <a href="mailto:hello@rfspager.app" className="font-semibold text-[#F7241D] hover:underline">
          hello@rfspager.app
        </a>
        .
      </p>
      <div className="mt-6 flex flex-col gap-3 sm:flex-row">
        <Link to="/coverage" className={secondaryBtn}>
          See the coverage map
        </Link>
        <button type="button" onClick={onAnother} className={secondaryBtn}>
          Apply for another receiver
        </button>
      </div>
    </section>
  );
}

function fmtDate(iso: string | null): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleDateString("en-AU", { day: "numeric", month: "short", year: "numeric", timeZone: "Australia/Sydney" });
}

function YourReceivers({ systems, error, onRetry }: { systems: MySystem[]; error: boolean; onRetry: () => void }) {
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => setNow(Date.now()), []);

  return (
    <section className="rounded-xl border border-gray-200 bg-white">
      <div className="border-b border-gray-200 px-6 py-5 sm:px-8">
        <h2 className={`${heading} text-xl font-extrabold tracking-tight text-gray-900`}>Your receivers</h2>
      </div>
      {error ? (
        <div className="px-6 py-5 sm:px-8">
          <Notice tone="error">Couldn't load your receivers.</Notice>
          <button type="button" onClick={onRetry} className={`${secondaryBtn} mt-4`}>
            Try again
          </button>
        </div>
      ) : (
        <ul className="divide-y divide-gray-200">
          {systems.map((s) => {
            const applied = fmtDate(s.created_at);
            const approved = fmtDate(s.approved_at);
            return (
              <li key={s.uuid} className="px-6 py-5 sm:px-8">
                <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
                  <div className="min-w-0">
                    <p className="truncate text-base font-semibold text-gray-900">{s.name}</p>
                    <p className="text-sm text-gray-500">
                      {s.suburb}
                      {s.suburb && (applied || approved) ? " · " : ""}
                      {s.is_pending ? (applied ? `Applied ${applied}` : "") : approved ? `Approved ${approved}` : ""}
                    </p>
                  </div>
                  <StatusPill pending={s.is_pending} />
                </div>

                {s.is_pending ? (
                  <p className="mt-3 text-sm leading-relaxed text-gray-500">
                    We review applications by hand. When it's approved we'll email you the install command, and it will
                    show here too.
                  </p>
                ) : (
                  <>
                    <p className="mt-2 text-xs text-gray-500" suppressHydrationWarning>
                      {s.last_message_at
                        ? now != null
                          ? `Last page decoded ${ago(s.last_message_at, now)}`
                          : ""
                        : "No pages decoded yet."}
                    </p>
                    {s.install_command ? (
                      <InstallCommand command={s.install_command} />
                    ) : (
                      <p className="mt-3 text-sm text-gray-500">
                        The install command isn't available right now. Write to{" "}
                        <a href="mailto:hello@rfspager.app" className="font-semibold text-[#F7241D] hover:underline">
                          hello@rfspager.app
                        </a>{" "}
                        and we'll sort it out.
                      </p>
                    )}
                  </>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function StatusPill({ pending }: { pending: boolean }) {
  return pending ? (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-amber-50 px-2.5 py-1 text-xs font-semibold text-amber-800 ring-1 ring-amber-200">
      <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-amber-500" />
      Pending review
    </span>
  ) : (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-800 ring-1 ring-emerald-200">
      <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
      Approved
    </span>
  );
}

function InstallCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // No clipboard permission: select the text so a manual copy is one keystroke.
      const el = preRef.current;
      const sel = window.getSelection();
      if (el && sel) {
        const range = document.createRange();
        range.selectNodeContents(el);
        sel.removeAllRanges();
        sel.addRange(range);
      }
    }
  };

  return (
    <div className="mt-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm font-semibold text-gray-800">Install on the Raspberry Pi</p>
        <button
          type="button"
          onClick={copy}
          className="inline-flex h-8 items-center rounded-md border border-gray-200 bg-white px-3 text-xs font-semibold text-gray-800 outline-none transition-colors hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre
        ref={preRef}
        className="mt-2 overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-gray-950 px-4 py-3 font-mono text-[13px] leading-relaxed text-gray-100"
      >
        {command}
      </pre>
      <p className="mt-2 text-xs leading-relaxed text-gray-500">
        Run it on the Pi with the RTL-SDR plugged in. It installs the receiver service and takes a few minutes. The
        command carries your receiver's key, so keep it to yourself.
      </p>
    </div>
  );
}

function Tick() {
  return (
    <svg className="mt-0.5 size-4 shrink-0 text-[#F7241D]" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden>
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
    </svg>
  );
}
