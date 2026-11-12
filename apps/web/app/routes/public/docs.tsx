import { useState, type ReactNode } from "react";
import { Link } from "react-router";

const heading = "font-[family-name:var(--font-heading)]";
const BASE = "https://rfspager.app/api/public";
const textLink = "font-semibold text-[#F7241D] hover:underline";

export function meta() {
  const description =
    "A free JSON API for the NSW emergency pager messages, capcodes, receivers and coverage behind RFSPager. No key needed.";
  return [
    { title: "Public API — RFSPager" },
    { name: "description", content: description },
    { property: "og:title", content: "RFSPager public API" },
    { property: "og:description", content: description },
  ];
}

const json = (v: unknown) => JSON.stringify(v, null, 2);

function C({ children }: { children: ReactNode }) {
  return <code className="rounded bg-gray-100 px-1 py-0.5 font-mono text-[0.85em] text-gray-900">{children}</code>;
}

function To({ id, children }: { id: string; children: ReactNode }) {
  return (
    <a href={`#${id}`} className={textLink}>
      {children}
    </a>
  );
}

interface Row {
  name: string;
  type: string;
  text: ReactNode;
  required?: boolean;
}

interface Endpoint {
  id: string;
  title: string;
  path: string;
  summary: ReactNode;
  params?: Row[];
  notes?: ReactNode[];
  fields?: Row[];
  requests: Array<{ label?: string; cmd: string }>;
  response: string;
}

// Example rows are real responses from production, trimmed. Key order is
// rearranged for reading; the API itself sends keys alphabetically.
const MESSAGE = {
  id: 2485982,
  address: "0120211",
  message: "FRINC TYPE: MOTORCYCLE FIRE TURNOUT: 202 INC: 207482-19102026",
  created_at: "2026-10-19T09:17:29Z",
  capcode_alias: "202 Abermain",
  capcode_icon: "fas fa-fire-extinguisher",
  capcode_colour: "indianred",
  agency_name: "FRNSW",
  system_uuid: "3eceecaf-1c47-49b8-955d-50d3c8b23ee6",
  system_name: "Figtree",
};

const QUICK_START = `curl "${BASE}/messages?per_page=2"`;
const QUICK_START_RESPONSE = json({
  data: [
    MESSAGE,
    {
      id: 2485981,
      address: "0107211",
      message: "FRINC TYPE: ALARM OPERATING TURNOUT: 72 INC: 207476-19102026",
      created_at: "2026-10-19T09:06:46Z",
      capcode_alias: "72 Merrylands",
      capcode_icon: "fas fa-fire-extinguisher",
      capcode_colour: "indianred",
      agency_name: "FRNSW",
      system_uuid: "03769005-f5fe-492b-b5f7-1eea8b8c656b",
      system_name: "Newcastle",
    },
  ],
  page: 1,
  per_page: 2,
  has_more: true,
});

const GROUPS: Array<{ title: string; endpoints: Endpoint[] }> = [
  {
    title: "Messages",
    endpoints: [
      {
        id: "messages",
        title: "Message feed",
        path: "/messages",
        summary: (
          <>
            Messages newest first, with the capcode, agency and receiver for each. Only messages to capcodes we've
            identified are included.
          </>
        ),
        params: [
          { name: "search", type: "string", text: "Only messages whose text contains this, ignoring case." },
          {
            name: "agency_id",
            type: "integer",
            text: (
              <>
                Only this agency's messages. Get IDs from <To id="agencies">agencies</To>.
              </>
            ),
          },
          {
            name: "per_page",
            type: "integer",
            text: "1 to 100. Default 50, which is also what you get for anything outside that range.",
          },
          { name: "page", type: "integer", text: "Which page of results, from 1." },
          {
            name: "before_created_at",
            type: "timestamp",
            text: (
              <>
                With <C>before_id</C>, returns messages older than this one. Pass the <C>created_at</C> of the last
                message you have, exactly as you received it.
              </>
            ),
          },
          {
            name: "before_id",
            type: "integer",
            text: "The id of that same message. Send both: with only one, it's ignored and you get the newest messages.",
          },
        ],
        notes: [
          <>
            Responses include <C>page</C>, <C>per_page</C> and <C>has_more</C>. Keep going while <C>has_more</C> is
            true.
          </>,
          <>
            To scroll back through history, use <C>before_created_at</C> and <C>before_id</C> rather than{" "}
            <C>page</C>. New messages arrive at the top all the time, so counting pages from the top skips or repeats
            messages as you go.
          </>,
        ],
        fields: [
          { name: "id", type: "integer", text: "Message ID." },
          { name: "address", type: "string", text: "The capcode the message was sent to." },
          { name: "message", type: "string", text: "The message text, as decoded." },
          { name: "created_at", type: "timestamp", text: "When the receiver decoded it." },
          { name: "capcode_alias", type: "string", text: "Who the capcode belongs to, such as a brigade or station." },
          {
            name: "capcode_icon",
            type: "string",
            text: (
              <>
                A Font Awesome class for the capcode, such as <C>fas fa-fire</C>.
              </>
            ),
          },
          { name: "capcode_colour", type: "string", text: "A CSS colour name for the capcode." },
          { name: "agency_name", type: "string", text: "The agency or RFS district the capcode belongs to." },
          {
            name: "system_uuid",
            type: "string",
            text: (
              <>
                The receiver that decoded it. See <To id="receivers">receivers</To>.
              </>
            ),
          },
          { name: "system_name", type: "string", text: "That receiver's name." },
        ],
        requests: [
          { cmd: `curl "${BASE}/messages?per_page=1"` },
          { label: "FRNSW messages that mention “alarm”:", cmd: `curl "${BASE}/messages?agency_id=18&search=alarm"` },
          {
            label: "The 50 messages before one you already have:",
            cmd: `curl "${BASE}/messages?before_created_at=2026-10-19T09:06:46Z&before_id=2485981"`,
          },
        ],
        response: json({ data: [MESSAGE], page: 1, per_page: 1, has_more: true }),
      },
      {
        id: "live-stream",
        title: "Live stream",
        path: "/events",
        summary: (
          <>
            A Server-Sent Events stream. Each new message is pushed as soon as it's decoded, in the same shape as a{" "}
            <To id="messages">message feed</To> row. Like the feed, it only carries messages to identified capcodes,
            and it has no filters of its own.
          </>
        ),
        notes: [
          "Lines starting with a colon are comments: one when you connect, then one every 30 seconds to keep the connection open.",
          <>
            Events have no IDs and nothing is replayed. After a reconnect, fill the gap from the{" "}
            <To id="messages">message feed</To>.
          </>,
          "Unlike the JSON endpoints, the stream allows any origin, so a web page on another site can open it.",
        ],
        requests: [
          { cmd: `curl -N ${BASE}/events` },
          {
            label: "In a browser:",
            cmd: `const events = new EventSource("${BASE}/events");
events.onmessage = (e) => {
  const message = JSON.parse(e.data);
  console.log(message.capcode_alias, message.message);
};`,
          },
        ],
        response: `: connected

data: ${JSON.stringify(MESSAGE)}

: keepalive`,
      },
      {
        id: "messages-map",
        title: "Messages with a location",
        path: "/messages/map",
        summary: (
          <>
            Messages we could place on a map, newest first. Unlike the feed, this includes messages to capcodes we
            have no record of, and their capcode fields are null.
          </>
        ),
        params: [
          {
            name: "limit",
            type: "integer",
            text: "1 to 500. Default 100, which is also what you get for anything outside that range.",
          },
          {
            name: "created_after",
            type: "timestamp",
            text: (
              <>
                Only messages after this time, such as <C>2026-10-19T00:00:00Z</C>. Default, and what you get if it
                doesn't parse: the last 24 hours.
              </>
            ),
          },
        ],
        notes: [
          <>
            <C>lat</C> and <C>lng</C> are strings.
          </>,
        ],
        requests: [
          { cmd: `curl "${BASE}/messages/map?limit=1"` },
          { label: "Everything since midnight UTC:", cmd: `curl "${BASE}/messages/map?created_after=2026-10-19T00:00:00Z&limit=500"` },
        ],
        response: json([
          {
            id: 2485959,
            address: "0210312",
            message:
              "19 October 2026 19:32:03 BMBLAXL1 - 26-131758 - Unknown fire - FIRECALL - GREAT WESTERN HWY,BLAXLAND,BLUE MOUNTAINS CITY (NSW),2774 - [150.608347,-33.745102]",
            lat: "-33.745102",
            lng: "150.608347",
            created_at: "2026-10-19T08:32:30Z",
            capcode_alias: "Blaxland Brigade",
            capcode_icon: "fas fa-fire",
            capcode_colour: "blue",
            agency_name: "Blue Mountains",
          },
        ]),
      },
      {
        id: "capcode-messages",
        title: "Messages for a capcode",
        path: "/capcodes/messages",
        summary: "The latest messages to one capcode, newest first, whether or not we've identified it.",
        params: [
          {
            name: "address",
            type: "string",
            required: true,
            text: (
              <>
                The capcode, such as <C>0120211</C>. It has to match exactly.
              </>
            ),
          },
          { name: "limit", type: "integer", text: "1 to 50. Default 10." },
        ],
        requests: [{ cmd: `curl "${BASE}/capcodes/messages?address=0120211&limit=1"` }],
        response: json({
          data: [
            {
              id: MESSAGE.id,
              address: MESSAGE.address,
              message: MESSAGE.message,
              created_at: MESSAGE.created_at,
              system_name: MESSAGE.system_name,
            },
          ],
        }),
      },
    ],
  },
  {
    title: "Capcodes",
    endpoints: [
      {
        id: "agencies",
        title: "Agencies",
        path: "/agencies",
        summary: (
          <>
            Every agency that has at least one capcode, alphabetically. Agencies are the services and RFS districts
            capcodes belong to. Use the <C>id</C> to filter the <To id="messages">message feed</To> and{" "}
            <To id="capcodes">capcodes</To>.
          </>
        ),
        requests: [{ cmd: `curl ${BASE}/agencies` }],
        response: json([
          { id: 10, name: "Blue Mountains", capcode_count: 69 },
          { id: 18, name: "FRNSW", capcode_count: 297 },
        ]),
      },
      {
        id: "capcodes",
        title: "Capcodes",
        path: "/capcodes",
        summary: "Every capcode we have on record: named ones first, alphabetically, then the rest by capcode.",
        params: [
          { name: "search", type: "string", text: "Matches the name, the capcode or the agency, ignoring case." },
          { name: "agency_id", type: "integer", text: "Only this agency's capcodes." },
          { name: "per_page", type: "integer", text: "1 to 2500. Default 50; anything larger gets 2500." },
          { name: "page", type: "integer", text: "Which page of results, from 1." },
        ],
        notes: [
          <>
            Responses include <C>page</C>, <C>per_page</C> and <C>has_more</C>, as in the{" "}
            <To id="messages">message feed</To>.
          </>,
        ],
        requests: [
          { cmd: `curl "${BASE}/capcodes?per_page=2"` },
          { label: "Search:", cmd: `curl "${BASE}/capcodes?search=kelso"` },
        ],
        response: json({
          data: [
            {
              id: 155349,
              address: "0100111",
              alias: "001 City of Sydney",
              icon: "fas fa-fire-extinguisher",
              colour: "indianred",
              agency_id: 18,
              agency_name: "FRNSW",
            },
            {
              id: 155608,
              address: "0110511",
              alias: "105 Kelso",
              icon: "fas fa-fire-extinguisher",
              colour: "indianred",
              agency_id: 18,
              agency_name: "FRNSW",
            },
          ],
          page: 1,
          per_page: 2,
          has_more: true,
        }),
      },
      {
        id: "unidentified-capcodes",
        title: "Unidentified capcodes",
        path: "/capcodes/unknown",
        summary: (
          <>
            Capcodes that received messages recently but that we haven't named yet, busiest first.{" "}
            <C>capcode_id</C> is null when we have no record of the capcode at all.
          </>
        ),
        params: [
          { name: "days", type: "integer", text: "How far back to look, 1 to 365. Default 30." },
          { name: "limit", type: "integer", text: "1 to 1000. Default 200." },
        ],
        notes: [
          <>
            If you know who one belongs to, use <span className="font-semibold text-gray-900">Help identify</span> on
            the{" "}
            <Link to="/capcodes" className={textLink}>
              Capcodes page
            </Link>
            .
          </>,
        ],
        requests: [{ cmd: `curl "${BASE}/capcodes/unknown?limit=2"` }],
        response: json({
          data: [
            {
              capcode_id: null,
              address: "0180111",
              alias: null,
              agency_name: null,
              message_count: 948,
              last_seen_at: "2026-09-28T01:29:57Z",
            },
            {
              capcode_id: null,
              address: "0290940",
              alias: null,
              agency_name: null,
              message_count: 26,
              last_seen_at: "2026-10-19T08:31:14Z",
            },
          ],
          meta: { window_days: 30, limit: 2 },
        }),
      },
    ],
  },
  {
    title: "Receivers and coverage",
    endpoints: [
      {
        id: "receivers",
        title: "Receivers",
        path: "/systems",
        summary: "Every receiver in the network: the ones waiting for approval first, then the rest by name.",
        notes: [
          <>
            Connected receivers ping every 30 seconds. Treat one whose <C>last_ping_at</C> is more than 90 seconds old
            as offline. <C>last_message_at</C> is when it last decoded a message, so a receiver can be online and
            quiet.
          </>,
          <>
            Only approved receivers are listed; applications waiting for approval are private, so{" "}
            <C>is_pending</C> is always false here. <C>suburb</C> can be null.
          </>,
        ],
        requests: [{ cmd: `curl ${BASE}/systems` }],
        response: json([
          {
            uuid: "02bb3839-46e6-4fee-bb31-376cd8f548a4",
            name: "Hornsby",
            suburb: "Hornsby",
            is_pending: false,
            connected_at: "2026-10-18T22:52:42.690833Z",
            last_ping_at: "2026-10-19T09:43:43.119803Z",
            last_message_at: "2026-10-18T21:09:59Z",
          },
        ]),
      },
      {
        id: "receiver-uptime",
        title: "Receiver uptime",
        path: "/systems/{uuid}/uptime",
        summary: (
          <>
            One approved receiver's uptime over the last 7 days, with each outage. Use the <C>uuid</C> from{" "}
            <To id="receivers">receivers</To>. A pending or unknown receiver gets a 404.
          </>
        ),
        notes: [
          <>
            An outage that's still going has a <C>to</C> of null, and <C>currently_offline</C> is true.
          </>,
        ],
        requests: [{ cmd: `curl ${BASE}/systems/3eceecaf-1c47-49b8-955d-50d3c8b23ee6/uptime` }],
        response: json({
          uuid: "3eceecaf-1c47-49b8-955d-50d3c8b23ee6",
          window_days: 7,
          window_start: "2026-10-12T09:41:30.073275246Z",
          uptime_percent: 99.23479302199094,
          downtime_seconds: 4627,
          downtime_intervals: [{ from: "2026-10-13T14:25:49.819205Z", to: "2026-10-13T15:42:57.791008Z" }],
          currently_offline: false,
        }),
      },
      {
        id: "coverage",
        title: "Coverage",
        path: "/coverage",
        summary: (
          <>
            What the receiver network heard over a window: the receivers, the RFS paging transmitter sites, RFS
            districts and a heat map of where paged incidents were. It's the data behind the{" "}
            <Link to="/coverage" className={textLink}>
              coverage map
            </Link>
            .
          </>
        ),
        params: [{ name: "days", type: "integer", text: "30 or 90. Anything else gives 90." }],
        notes: [
          "Receiver positions are rounded to about 1 km.",
          <>
            A transmitter is <C>heard</C> when a receiver probably or possibly hears it, or its district had messages
            in the window.
          </>,
          <>
            <C>heat</C> is a list of <C>[lng, lat, weight]</C>, one for each cell of about 1 km, where the weight is
            the number of incidents in it.
          </>,
          "Responses are cached for 15 minutes.",
        ],
        requests: [{ cmd: `curl "${BASE}/coverage?days=30"` }],
        response: json({
          window: {
            days: 30,
            since: "2026-09-19T09:41:30.156684556Z",
            until: "2026-10-19T09:41:30.156684556Z",
          },
          generated_at: "2026-10-19T09:41:30.156684556Z",
          total_pages: 4396,
          receivers: [{ name: "Budgewoi", lat: -33.23, lng: 151.56, last_heard_at: "2026-10-19T08:41:39Z" }],
          transmitters: [{ lat: -35.983587, lng: 149.026049, heard: false, district: "Monaro" }],
          districts: [{ name: "Lower Hunter", heard: true, pages: 1084, last_heard_at: "2026-10-19T08:00:57Z" }],
          heat: [[147.26, -33.71, 1]],
        }),
      },
    ],
  },
  {
    title: "Incidents, stations and totals",
    endpoints: [
      {
        id: "major-incidents",
        title: "Major incidents",
        path: "/major-incidents",
        summary:
          "The 100 most recent incidents from the NSW RFS major incidents feed, newest first. We refresh it every 5 minutes.",
        notes: [
          <>
            <C>geometry</C> (GeoJSON) and <C>data</C> are JSON inside strings, so parse them a second time.
          </>,
        ],
        requests: [{ cmd: `curl ${BASE}/major-incidents` }],
        response: json([
          {
            id: 241934,
            title: "ALBION PL, ENGADINE",
            category: "Not Applicable",
            geometry: JSON.stringify({ coordinates: [151.021153, -34.043138], type: "Point" }),
            data: JSON.stringify({
              alertLevel: "Not Applicable",
              councilArea: "Sutherland",
              fire: "No",
              location: "ALBION PL, ENGADINE 2233",
              responsibleAgency: "Fire and Rescue NSW",
              size: "0 ha",
              status: "Being controlled",
              type: "Assist Other Agency",
              updated: "16 Oct 2026 19:14",
            }),
            created_at: "2026-10-15T21:14:00Z",
            updated_at: "2026-10-19T09:41:56.539309Z",
          },
        ]),
      },
      {
        id: "stations",
        title: "Stations",
        path: "/stations",
        summary:
          "NSW emergency service stations as a GeoJSON FeatureCollection, ready to use as a map source. All of them come to about 2,800 stations and 950 KB.",
        params: [
          {
            name: "categories",
            type: "string",
            text: (
              <>
                Comma-separated, from <C>rfs</C>, <C>frnsw</C>, <C>ambulance</C>, <C>police</C> and <C>ses</C>. Default:
                all of them.
              </>
            ),
          },
        ],
        notes: [
          <>
            The data comes from Geoscience Australia (CC BY 4.0) and Fire and Rescue NSW. Show the text in{" "}
            <C>meta.attribution</C> wherever you use it.
          </>,
        ],
        requests: [{ cmd: `curl "${BASE}/stations?categories=rfs"` }],
        response: json({
          type: "FeatureCollection",
          features: [
            {
              type: "Feature",
              id: 4,
              geometry: { type: "Point", coordinates: [148.7729435, -35.9956063] },
              properties: {
                id: 4,
                source: "ga_emfd",
                category: "rfs",
                name: "ADAMINABY",
                operator: "NSW Rural Fire Service",
                address: "20 YORK STREET",
                suburb: "ADAMINABY",
                postcode: "2629",
                status: "OPERATIONAL",
                revised_at: "2018-09-14T00:00:00Z",
              },
            },
          ],
          meta: {
            attribution:
              "Stations sourced from Geoscience Australia Emergency Management Facilities Database (CC-BY 4.0) and Fire and Rescue NSW.",
            count: 1558,
          },
        }),
      },
      {
        id: "totals",
        title: "Totals",
        path: "/stats",
        summary: "The headline numbers on the home page.",
        fields: [
          { name: "capcode_count", type: "integer", text: "Capcodes on record." },
          {
            name: "message_count_30d",
            type: "integer",
            text: "Messages in the last 30 days, including ones to unidentified capcodes.",
          },
          { name: "incident_count", type: "integer", text: "Major incidents on record." },
        ],
        requests: [{ cmd: `curl ${BASE}/stats` }],
        response: json({ capcode_count: 1638, message_count_30d: 10980, incident_count: 173313 }),
      },
    ],
  },
];

export default function Docs() {
  return (
    <div className="flex-1 bg-white">
      <div className="border-b border-gray-200 bg-gray-50">
        <div className="mx-auto grid max-w-[1400px] items-start gap-10 px-4 py-12 sm:px-6 md:py-16 lg:grid-cols-12 lg:gap-14">
          <div className="lg:col-span-5">
            <h1 className={`${heading} text-4xl font-extrabold leading-[1.05] tracking-tight text-gray-900 md:text-5xl`}>
              Public API
            </h1>
            <p className="mt-5 text-lg leading-relaxed text-gray-500">
              The pager messages, capcodes, receivers and coverage behind rfspager.app, as JSON over HTTPS. There's no
              key and nothing to sign up for: every endpoint is a plain GET under
            </p>
            <p className="mt-3 break-all font-mono text-base font-semibold text-gray-900">{BASE}</p>
          </div>
          <div id="quick-start" className="min-w-0 scroll-mt-24 lg:col-span-7">
            <QuickStart />
          </div>
        </div>
      </div>

      <div className="mx-auto grid max-w-[1400px] gap-10 px-4 py-12 sm:px-6 md:py-16 lg:grid-cols-12 lg:gap-14">
        <aside className="hidden lg:col-span-3 lg:block">
          <nav aria-label="On this page" className="sticky top-24 max-h-[calc(100vh-7rem)] overflow-y-auto pb-4">
            <Contents />
          </nav>
        </aside>

        <div className="min-w-0 max-w-4xl lg:col-span-9">
          <details className="mb-10 rounded-xl border border-gray-200 px-4 py-3 lg:hidden">
            <summary className="cursor-pointer text-sm font-semibold text-gray-900">On this page</summary>
            <nav aria-label="On this page" className="mt-3">
              <Contents />
            </nav>
          </details>

          <BeforeYouStart />

          {GROUPS.map((group) => (
            <section key={group.title} className="mt-16">
              <h2 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>{group.title}</h2>
              <div className="mt-6 space-y-14">
                {group.endpoints.map((endpoint) => (
                  <EndpointSection key={endpoint.id} endpoint={endpoint} />
                ))}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}

// The page is itself a client of the API: same origin, so no CORS question.
function QuickStart() {
  const [live, setLive] = useState<{ body: string; at: Date } | null>(null);
  const [status, setStatus] = useState<"idle" | "sending" | "failed">("idle");

  const send = () => {
    setStatus("sending");
    fetch("/api/public/messages?per_page=2")
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((body: unknown) => {
        setLive({ body: json(body), at: new Date() });
        setStatus("idle");
      })
      .catch(() => setStatus("failed"));
  };

  return (
    <div className="overflow-hidden rounded-xl bg-gray-950 text-gray-100 shadow-sm">
      <div className="flex items-start border-b border-white/10">
        <pre className="min-w-0 flex-1 overflow-x-auto p-4 font-mono text-[13px] leading-relaxed">
          <code>{QUICK_START}</code>
        </pre>
        <CopyButton text={QUICK_START} />
      </div>
      <div className="flex items-center justify-between gap-3 px-4 pt-3">
        <p aria-live="polite" className={`text-xs font-semibold ${status === "failed" ? "text-red-300" : "text-gray-400"}`}>
          {status === "failed"
            ? "Couldn't reach the API. Try again."
            : live
              ? `Live response at ${live.at.toLocaleTimeString()}`
              : "Example response"}
        </p>
        <button
          type="button"
          onClick={send}
          disabled={status === "sending"}
          className="shrink-0 rounded-md bg-[#F7241D] px-3 py-1.5 text-xs font-semibold text-white outline-none transition-colors hover:bg-[#d91f19] focus-visible:ring-2 focus-visible:ring-[#F7241D]/60 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-950 disabled:opacity-60"
        >
          {status === "sending" ? "Sending…" : live ? "Send again" : "Send request"}
        </button>
      </div>
      <pre className="max-h-96 overflow-auto p-4 font-mono text-[13px] leading-relaxed text-gray-300">
        <code>{live?.body ?? QUICK_START_RESPONSE}</code>
      </pre>
    </div>
  );
}

function Contents() {
  const link =
    "block rounded-md px-2 py-1 text-sm text-gray-600 outline-none transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40";
  return (
    <div className="space-y-5">
      <ul>
        <li>
          <a href="#quick-start" className={link}>
            Quick start
          </a>
        </li>
        <li>
          <a href="#before-you-start" className={link}>
            Before you start
          </a>
        </li>
      </ul>
      {GROUPS.map((group) => (
        <div key={group.title}>
          <p className={`${heading} px-2 text-sm font-bold text-gray-900`}>{group.title}</p>
          <ul className="mt-1">
            {group.endpoints.map((endpoint) => (
              <li key={endpoint.id}>
                <a href={`#${endpoint.id}`} className={link}>
                  {endpoint.title}
                </a>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function BeforeYouStart() {
  const items: Array<{ k: string; v: ReactNode }> = [
    {
      k: "No key",
      v: (
        <>
          Requests need no key or account, and there's no published rate limit. Please be gentle: rather than polling
          the <To id="messages">message feed</To> every few seconds, keep one connection open to the{" "}
          <To id="live-stream">live stream</To>.
        </>
      ),
    },
    {
      k: "Times",
      v: (
        <>
          Timestamps are ISO 8601 in UTC, like <C>2026-10-19T09:17:29Z</C>, sometimes with fractional seconds.
        </>
      ),
    },
    {
      k: "From a web page",
      v: "The JSON endpoints don't send CORS headers, so browsers block them on other sites: call them from your server. The live stream is the exception and allows any origin.",
    },
    {
      k: "Errors",
      v: (
        <>
          Endpoints report errors as JSON with a 4xx or 5xx status, like{" "}
          <C>{'{"error": "address is required"}'}</C>.
        </>
      ),
    },
    {
      k: "Questions",
      v: (
        <>
          Email{" "}
          <a href="mailto:hello@rfspager.app" className={textLink}>
            hello@rfspager.app
          </a>
          .
        </>
      ),
    },
  ];
  return (
    <section id="before-you-start" className="scroll-mt-24">
      <h2 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Before you start</h2>
      <dl className="mt-5 divide-y divide-gray-200 border-y border-gray-200">
        {items.map((item) => (
          <div key={item.k} className="grid gap-1 py-4 sm:grid-cols-[10rem_1fr] sm:gap-6">
            <dt className="text-sm font-semibold text-gray-900">{item.k}</dt>
            <dd className="max-w-[70ch] text-[15px] leading-relaxed text-gray-600">{item.v}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function EndpointSection({ endpoint: e }: { endpoint: Endpoint }) {
  return (
    <article id={e.id} className="scroll-mt-24 border-t border-gray-200 pt-8">
      <h3 className={`${heading} text-lg font-bold text-gray-900`}>{e.title}</h3>
      <p className="mt-1 break-all font-mono text-sm">
        <span className="text-gray-400">GET</span>{" "}
        <span className="font-semibold text-gray-900">/api/public{e.path}</span>
      </p>
      <p className="mt-4 max-w-[70ch] text-[15px] leading-relaxed text-gray-600">{e.summary}</p>
      {e.params && <Rows title="Parameters" rows={e.params} />}
      {e.notes && (
        <div className="mt-4 max-w-[70ch] space-y-3 text-[15px] leading-relaxed text-gray-600">
          {e.notes.map((note, i) => (
            <p key={i}>{note}</p>
          ))}
        </div>
      )}
      {e.fields && <Rows title="Fields" rows={e.fields} />}

      <h4 className="mt-6 text-sm font-semibold text-gray-900">{e.requests.length > 1 ? "Requests" : "Request"}</h4>
      <div className="mt-2 space-y-3">
        {e.requests.map((r) => (
          <div key={r.cmd}>
            {r.label && <p className="mb-1.5 text-sm text-gray-500">{r.label}</p>}
            <CodeBlock code={r.cmd} copy />
          </div>
        ))}
      </div>

      <h4 className="mt-6 text-sm font-semibold text-gray-900">Response</h4>
      <div className="mt-2">
        <CodeBlock code={e.response} />
      </div>
    </article>
  );
}

function Rows({ title, rows }: { title: string; rows: Row[] }) {
  return (
    <div className="mt-6">
      <h4 className="text-sm font-semibold text-gray-900">{title}</h4>
      <table className="mt-2 w-full border-y border-gray-200 text-sm">
        <thead className="sr-only">
          <tr>
            <th>Name</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {rows.map((r) => (
            <tr key={r.name}>
              <td className="w-40 py-3 pr-4 align-top sm:w-52">
                <code className="font-mono text-[13px] font-semibold text-gray-900 [overflow-wrap:anywhere]">
                  {r.name}
                </code>
                <span className="block text-xs text-gray-400">{r.required ? `${r.type}, required` : r.type}</span>
              </td>
              <td className="py-3 align-top leading-relaxed text-gray-600">{r.text}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// The copy button sits beside the code rather than over it, so a long line
// scrolling under it is never hidden.
function CodeBlock({ code, copy = false }: { code: string; copy?: boolean }) {
  return (
    <div className="flex items-start rounded-xl bg-gray-950">
      <pre className="max-h-[28rem] min-w-0 flex-1 overflow-auto p-4 font-mono text-[13px] leading-relaxed text-gray-100">
        <code>{code}</code>
      </pre>
      {copy && <CopyButton text={code} />}
    </div>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard
          .writeText(text)
          .then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          })
          .catch(() => {});
      }}
      className="m-2.5 shrink-0 rounded-md bg-white/10 px-2 py-1 text-xs font-semibold text-gray-200 outline-none transition-colors hover:bg-white/20 focus-visible:ring-2 focus-visible:ring-[#F7241D]/60"
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}
