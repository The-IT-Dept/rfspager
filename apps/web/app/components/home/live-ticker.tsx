import { useEffect, useState } from "react";

interface Message {
  id: number;
  address: string;
  message: string;
  created_at: string;
  capcode_alias: string | null;
  capcode_colour: string | null;
  agency_name: string | null;
}

interface Props {
  initial: Message[];
}

function ago(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

export function LiveTicker({ initial }: Props) {
  const [messages, setMessages] = useState<Message[]>(initial);
  const [connected, setConnected] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.onmessage = (e) => {
      try {
        const msg: Message = JSON.parse(e.data);
        setMessages((prev) => [msg, ...prev].slice(0, 12));
      } catch {}
    };
    return () => es.close();
  }, []);

  return (
    <div className="relative">
      <div className="absolute -inset-6 bg-[radial-gradient(ellipse_at_center,rgba(247,36,29,0.18),transparent_70%)] blur-2xl pointer-events-none" />

      <div className="relative bg-[#0b0b0d] rounded-2xl shadow-2xl ring-1 ring-white/10 overflow-hidden">
        {/* Chrome */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-white/5 bg-black/40">
          <div className="flex gap-1.5">
            <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]" />
            <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]" />
            <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]" />
          </div>
          <div className="flex-1 text-center">
            <span className="text-[11px] font-mono text-white/40 tracking-wider">
              rfspager.app/pager
            </span>
          </div>
          <div className={`flex items-center gap-1.5 text-[10px] font-bold tracking-widest ${connected ? "text-emerald-400" : "text-white/30"}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${connected ? "bg-emerald-400 animate-pulse" : "bg-white/30"}`} />
            {connected ? "LIVE" : "CONNECTING"}
          </div>
        </div>

        {/* Feed */}
        <div className="relative h-[420px] md:h-[460px] overflow-hidden">
          <ul className="divide-y divide-white/5">
            {messages.length === 0 ? (
              <li className="px-5 py-20 text-center text-white/30 text-sm">
                Waiting for messages…
              </li>
            ) : (
              messages.map((msg, i) => (
                <li
                  key={msg.id}
                  className={`px-5 py-3 ${i === 0 ? "bg-white/[0.02] animate-in slide-in-from-top-2 fade-in-0 duration-500" : ""}`}
                  style={{ opacity: Math.max(0.35, 1 - i * 0.07) }}
                >
                  <div className="flex items-center gap-3 mb-1">
                    <span
                      className="inline-block px-2 py-0.5 rounded text-[10px] font-bold text-white uppercase tracking-wide truncate max-w-[140px]"
                      style={{ backgroundColor: msg.capcode_colour || "#6b7280" }}
                    >
                      {msg.capcode_alias || msg.address}
                    </span>
                    {msg.agency_name && (
                      <span className="text-[10px] text-white/40 truncate">{msg.agency_name}</span>
                    )}
                    <span className="ml-auto text-[10px] font-mono text-white/30 tabular-nums">{ago(msg.created_at)} ago</span>
                  </div>
                  <p className="text-[12px] font-mono text-white/80 leading-relaxed line-clamp-2 whitespace-pre-wrap">
                    {msg.message}
                  </p>
                </li>
              ))
            )}
          </ul>
          {/* Fade-out gradient at bottom */}
          <div className="absolute inset-x-0 bottom-0 h-32 bg-gradient-to-t from-[#0b0b0d] via-[#0b0b0d]/80 to-transparent pointer-events-none" />
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-4 py-2.5 border-t border-white/5 bg-black/40 text-[10px] font-mono text-white/40 tracking-wider">
          <span>{messages.length} recent · streaming</span>
          <span className="tabular-nums">{tick % 2 === 0 ? "▮" : "▯"}</span>
        </div>
      </div>
    </div>
  );
}
