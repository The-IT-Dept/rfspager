import type { Route } from "./+types/systems";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const ONLINE_THRESHOLD_MINUTES = 30;

interface System {
  uuid: string;
  name: string;
  last_message_at: string | null;
}

export async function loader() {
  try {
    const res = await fetch(`${API_BASE}/api/public/systems`);
    return { systems: res.ok ? ((await res.json()) as System[]) : [] };
  } catch {
    return { systems: [] };
  }
}

export function meta() {
  return [
    { title: "Systems — RFSPager" },
    {
      name: "description",
      content:
        "Status of pager systems feeding RFSPager. Online/offline state and time since the last message per system.",
    },
  ];
}

function isOnline(lastMessageAt: string | null): boolean {
  if (!lastMessageAt) return false;
  const diffMs = Date.now() - new Date(lastMessageAt).getTime();
  return diffMs <= ONLINE_THRESHOLD_MINUTES * 60_000;
}

function timeAgo(dateStr: string | null): string {
  if (!dateStr) return "No messages yet";
  const mins = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  return `${days}d ago`;
}

export default function SystemsPage({ loaderData }: Route.ComponentProps) {
  const systems: System[] = loaderData?.systems || [];
  const onlineCount = systems.filter((s) => isOnline(s.last_message_at)).length;

  return (
    <div className="max-w-[1400px] mx-auto w-full px-6 py-10">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900">Systems</h1>
        <p className="mt-2 text-sm text-gray-500">
          {systems.length} total · {onlineCount} online · {systems.length - onlineCount} offline
        </p>
      </div>

      <div className="bg-white border border-gray-200 rounded-2xl shadow-sm overflow-hidden">
        <div className="grid grid-cols-12 px-6 py-3 text-xs font-semibold uppercase tracking-wider text-gray-500 bg-gray-50 border-b border-gray-200">
          <div className="col-span-5">Name</div>
          <div className="col-span-5">UUID</div>
          <div className="col-span-2 text-right">Last message</div>
        </div>

        {systems.length === 0 ? (
          <div className="px-6 py-16 text-center">
            <p className="text-base font-semibold text-gray-900">No systems yet</p>
            <p className="mt-1 text-sm text-gray-500">Systems will appear here once they're added.</p>
          </div>
        ) : (
          <ul>
            {systems.map((s, idx) => {
              const online = isOnline(s.last_message_at);
              return (
                <li
                  key={s.uuid}
                  className={`grid grid-cols-12 items-center px-6 py-4 ${
                    idx === systems.length - 1 ? "" : "border-b border-gray-100"
                  } hover:bg-gray-50 transition-colors`}
                >
                  <div className="col-span-5 flex items-center gap-3 min-w-0">
                    <span
                      className={`inline-block w-2.5 h-2.5 rounded-full flex-shrink-0 ${
                        online ? "bg-emerald-500" : "bg-gray-300"
                      }`}
                      aria-label={online ? "Online" : "Offline"}
                    />
                    <span className="text-sm font-semibold text-gray-900 truncate">
                      {s.name || "Unnamed system"}
                    </span>
                    <span
                      className={`text-[10px] font-bold uppercase tracking-wide px-2 py-0.5 rounded-full ${
                        online ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
                      }`}
                    >
                      {online ? "Online" : "Offline"}
                    </span>
                  </div>
                  <div className="col-span-5 text-xs font-mono text-gray-500 truncate">{s.uuid}</div>
                  <div className="col-span-2 text-sm text-gray-700 text-right">
                    {timeAgo(s.last_message_at)}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
