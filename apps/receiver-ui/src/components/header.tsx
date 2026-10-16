import { useStreamState } from "../lib/events";
import { cn, fmtDuration, fmtVersion } from "../lib/format";
import { useNow } from "../lib/hooks";
import { useStatus } from "../lib/status";
import type { Status } from "../lib/types";
import { Pill, type Tone } from "./ui";

export function Header() {
  const { status, error } = useStatus();
  const stream = useStreamState();
  const now = useNow();

  const title = status?.system?.name ? `${status.system.name} receiver` : "RFSPager receiver";
  const uptimeS = status ? (now - status.receiver.started_at) / 1000 : null;

  return (
    <header className="border-b border-border bg-white">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-3.5 gap-y-2 px-4 py-3">
        <a href="#/" className="flex shrink-0 items-center" aria-label="Overview">
          <img src="/logo.svg" alt="RFSPager" className="h-6 w-auto" width={121} height={24} />
        </a>
        <div className="h-7 w-px shrink-0 bg-border" aria-hidden />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-[14px] font-bold leading-tight">{title}</h1>
          <p className="text-[11px] text-muted-foreground sm:truncate">
            {status ? (
              <>
                {status.receiver.hostname} · {fmtVersion(status.receiver.version)} ·{" "}
                <span title={`Host up ${fmtDuration(status.receiver.host_uptime_s)}`}>up {fmtDuration(uptimeS)}</span>
              </>
            ) : error ? (
              <span className="text-red-700">{error}</span>
            ) : (
              "Loading…"
            )}
          </p>
        </div>
        <div className="flex w-full flex-wrap items-center gap-1.5 sm:ml-auto sm:w-auto sm:justify-end">
          {stream === "down" && (
            <Pill tone="muted" pulse title="The live connection to this receiver dropped; retrying">
              Reconnecting…
            </Pill>
          )}
          {status && <StatusPills status={status} stale={stream !== "live"} />}
        </div>
      </div>
    </header>
  );
}

function StatusPills({ status, stale }: { status: Status; stale: boolean }) {
  const pills: { key: string; tone: Tone; label: string; title: string }[] = [];

  pills.push(
    status.dongle.open
      ? { key: "dongle", tone: "ok", label: "Dongle", title: "The RTL-SDR dongle is open and delivering samples" }
      : { key: "dongle", tone: "bad", label: "Dongle closed", title: "The RTL-SDR dongle is not open; the receiver keeps retrying" },
  );

  if (status.upstream) {
    const u = status.upstream;
    // The host people recognise ("rfspager.app"), not the endpoint's config name.
    let host = u.name;
    try {
      host = new URL(u.url).host || u.name;
    } catch {
      /* keep the name */
    }
    pills.push(
      u.connected
        ? { key: "up", tone: "ok", label: host, title: `Connected to ${host}${u.rtt_ms ? ` · ${u.rtt_ms} ms` : ""}` }
        : { key: "up", tone: "bad", label: `${host} down`, title: `Not connected to ${host}` },
    );
  }

  const w = status.webhooks;
  if (w.total > 0) {
    if (w.failing > 0) {
      pills.push({
        key: "wh",
        tone: "warn",
        label: `${w.failing} webhook${w.failing === 1 ? "" : "s"} failing`,
        title: "The newest delivery to these webhooks failed after retries",
      });
    } else if (w.enabled > 0) {
      pills.push({
        key: "wh",
        tone: "ok",
        label: `${w.enabled} webhook${w.enabled === 1 ? "" : "s"}`,
        title: "Webhooks enabled and delivering",
      });
    } else {
      pills.push({ key: "wh", tone: "muted", label: "Webhooks off", title: "All webhooks are disabled" });
    }
  }

  if (status.undervoltage_now) {
    pills.push({ key: "uv", tone: "bad", label: "Undervoltage", title: "The Pi reports undervoltage right now; check the power supply" });
  }

  return (
    <>
      {pills.map((p) => (
        <a key={p.key} href={p.key === "dongle" || p.key === "uv" ? "#/" : "#/connections"} className={cn(stale && "opacity-60")}>
          <Pill tone={p.tone} title={p.title}>
            {p.label}
          </Pill>
        </a>
      ))}
    </>
  );
}
