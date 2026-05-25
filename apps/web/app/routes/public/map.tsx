import { useState, useEffect, useRef } from "react";
import type { Route } from "./+types/map";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const MAPBOX_TOKEN = "pk.eyJ1Ijoibmlja28xNzAiLCJhIjoiY2t2NGg4YTBqMTB1cjJucXBiYzdiZXR5aSJ9.zN0Y_TAwyaRqj3HptCDIkg";
const heading = "font-[family-name:var(--font-heading)]";

export async function loader() {
  try {
    const [incRes, msgRes] = await Promise.all([
      fetch(`${API_BASE}/api/public/major-incidents`),
      fetch(`${API_BASE}/api/public/messages/map?limit=200`),
    ]);
    return {
      incidents: incRes.ok ? await incRes.json() : [],
      messages: msgRes.ok ? await msgRes.json() : [],
    };
  } catch {
    return { incidents: [], messages: [] };
  }
}

export function meta() {
  return [
    { title: "Emergency Map — RFSPager" },
    { name: "description", content: "Interactive map of NSW emergency incidents, pager messages, and major fire events." },
  ];
}

interface Incident {
  id: number;
  title: string;
  category: string | null;
  geometry: string | null;
  data: string | null;
  created_at: string;
}

interface MapMessage {
  id: number;
  address: string;
  message: string;
  lat: string;
  lng: string;
  created_at: string;
  capcode_alias: string | null;
  capcode_colour: string | null;
  agency_name: string | null;
}

function categoryColor(cat: string | null): string {
  if (!cat) return "#FF3B30";
  const c = cat.toLowerCase();
  if (c.includes("emergency") || c.includes("immediate")) return "#FF3B30";
  if (c.includes("watch") || c.includes("act")) return "#FF9500";
  if (c.includes("advice")) return "#FFCC00";
  if (c.includes("control") || c.includes("contained")) return "#34C759";
  return "#FF3B30";
}

function MapComponent({ messages, incidents, onSelectIncident }: {
  messages: MapMessage[];
  incidents: Incident[];
  onSelectIncident: (inc: Incident | null) => void;
}) {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<any>(null);

  useEffect(() => {
    if (!mapContainer.current || mapRef.current) return;

    // Dynamic import to avoid SSR issues
    import("mapbox-gl").then((mapboxgl) => {
      mapboxgl.default.accessToken = MAPBOX_TOKEN;

      const map = new mapboxgl.default.Map({
        container: mapContainer.current!,
        style: "mapbox://styles/mapbox/streets-v11",
        center: [150.34, -33.85], // Sydney area
        zoom: 5,
      });

      map.addControl(new mapboxgl.default.NavigationControl(), "top-right");
      mapRef.current = map;

      map.on("load", () => {
        // Add message markers
        for (const msg of messages) {
          const lat = parseFloat(msg.lat);
          const lng = parseFloat(msg.lng);
          if (isNaN(lat) || isNaN(lng)) continue;

          const el = document.createElement("div");
          el.className = "msg-marker";
          el.style.width = "12px";
          el.style.height = "12px";
          el.style.borderRadius = "50%";
          el.style.backgroundColor = msg.capcode_colour || "#F7241D";
          el.style.border = "2px solid white";
          el.style.boxShadow = "0 1px 3px rgba(0,0,0,0.3)";
          el.style.cursor = "pointer";

          new mapboxgl.default.Marker(el)
            .setLngLat([lng, lat])
            .setPopup(
              new mapboxgl.default.Popup({ offset: 10, maxWidth: "400px" })
                .setHTML(`
                  <div style="font-family: system-ui, sans-serif;">
                    <div style="display:flex;justify-content:space-between;align-items:start;gap:8px;margin-bottom:4px;">
                      <strong style="font-size:13px;">${msg.capcode_alias || msg.address}</strong>
                      <span style="font-size:11px;color:#888;white-space:nowrap;">${new Date(msg.created_at).toLocaleTimeString("en-AU", { hour: "2-digit", minute: "2-digit" })}</span>
                    </div>
                    ${msg.agency_name ? `<div style="font-size:11px;color:#666;margin-bottom:4px;">${msg.agency_name}</div>` : ""}
                    <div style="font-size:12px;color:#333;font-family:monospace;line-height:1.4;">${msg.message}</div>
                  </div>
                `)
            )
            .addTo(map);
        }

        // Add incident markers (larger)
        for (const inc of incidents) {
          let geo: any = null;
          try {
            geo = typeof inc.geometry === "string" ? JSON.parse(inc.geometry) : inc.geometry;
          } catch {}
          if (!geo) continue;

          // Try to get a centroid from the geometry
          let coords: [number, number] | null = null;
          if (geo.type === "Point") {
            coords = geo.coordinates;
          } else if (geo.type === "Polygon" && geo.coordinates?.[0]?.[0]) {
            // Simple centroid: average of first ring
            const ring = geo.coordinates[0];
            const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
            const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
            coords = [lng, lat];
          } else if (geo.type === "MultiPolygon" && geo.coordinates?.[0]?.[0]?.[0]) {
            const ring = geo.coordinates[0][0];
            const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
            const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
            coords = [lng, lat];
          }

          if (!coords) continue;

          const color = categoryColor(inc.category);
          const el = document.createElement("div");
          el.style.width = "20px";
          el.style.height = "20px";
          el.style.borderRadius = "50%";
          el.style.backgroundColor = color;
          el.style.border = "3px solid white";
          el.style.boxShadow = "0 2px 6px rgba(0,0,0,0.3)";
          el.style.cursor = "pointer";

          const marker = new mapboxgl.default.Marker(el)
            .setLngLat(coords)
            .setPopup(
              new mapboxgl.default.Popup({ offset: 12, maxWidth: "350px" })
                .setHTML(`
                  <div style="font-family: system-ui, sans-serif;">
                    <strong style="font-size:14px;">${inc.title}</strong>
                    <div style="margin-top:4px;">
                      <span style="display:inline-block;padding:2px 8px;border-radius:99px;font-size:11px;font-weight:bold;color:white;background:${color};">${inc.category || "Unknown"}</span>
                    </div>
                    <div style="font-size:11px;color:#888;margin-top:6px;">Updated ${new Date(inc.created_at).toLocaleString("en-AU")}</div>
                  </div>
                `)
            )
            .addTo(map);

          el.addEventListener("click", () => onSelectIncident(inc));

          // Also add the polygon geometry if it exists
          if (geo.type === "Polygon" || geo.type === "MultiPolygon") {
            const sourceId = `incident-${inc.id}`;
            map.addSource(sourceId, { type: "geojson", data: { type: "Feature", geometry: geo, properties: {} } });
            map.addLayer({
              id: `${sourceId}-fill`,
              type: "fill",
              source: sourceId,
              paint: { "fill-color": color, "fill-opacity": 0.15 },
            });
            map.addLayer({
              id: `${sourceId}-line`,
              type: "line",
              source: sourceId,
              paint: { "line-color": color, "line-width": 2, "line-opacity": 0.6 },
            });
          }
        }
      });
    });

    return () => {
      mapRef.current?.remove();
      mapRef.current = null;
    };
  }, [messages, incidents]);

  return <div ref={mapContainer} className="absolute inset-0" />;
}

export default function MapPage({ loaderData }: Route.ComponentProps) {
  const { incidents, messages } = loaderData;
  const [selected, setSelected] = useState<Incident | null>(null);

  return (
    <div className="flex flex-1 overflow-hidden">
      {/* Sidebar */}
      <aside className="w-80 bg-white border-r border-gray-200 flex flex-col shrink-0 hidden md:flex">
        <div className="px-4 py-4 border-b border-gray-100">
          <h2 className={`${heading} text-base font-bold text-gray-900`}>Major Incidents</h2>
          <p className="text-xs text-gray-400 mt-0.5">{incidents.length} active &middot; {messages.length} messages with coords</p>
        </div>
        <div className="flex-1 overflow-y-auto">
          {incidents.length === 0 && (
            <div className="px-4 py-12 text-center text-sm text-gray-400">No active incidents</div>
          )}
          {incidents.map((inc: Incident) => {
            let d: any = {};
            try { d = typeof inc.data === "string" ? JSON.parse(inc.data) : inc.data || {}; } catch {}
            const active = selected?.id === inc.id;
            return (
              <button
                key={inc.id}
                onClick={() => setSelected(active ? null : inc)}
                className={`w-full text-left px-4 py-3 border-b border-gray-50 hover:bg-gray-50 transition-colors ${active ? "bg-red-50 border-l-2 border-l-[#F7241D]" : ""}`}
              >
                <div className="flex items-start gap-2.5">
                  <div className="w-2 h-2 rounded-full mt-1.5 shrink-0" style={{ backgroundColor: categoryColor(inc.category) }} />
                  <div className="min-w-0">
                    <p className="text-sm font-semibold text-gray-900 leading-tight truncate">{inc.title}</p>
                    <p className="text-xs text-gray-400 mt-0.5 truncate">{inc.category || "Unknown"}{d?.location ? ` \u2022 ${d.location}` : ""}</p>
                  </div>
                </div>
              </button>
            );
          })}
        </div>
      </aside>

      {/* Map */}
      <div className="flex-1 relative">
        <MapComponent messages={messages} incidents={incidents} onSelectIncident={setSelected} />

        {/* Detail card */}
        {selected && (
          <div className="absolute bottom-4 left-4 right-4 md:left-auto md:right-4 md:w-[380px] bg-white rounded-xl shadow-xl border border-gray-100 p-5 z-10">
            <div className="flex items-start justify-between mb-3">
              <h3 className={`${heading} font-bold text-gray-900 leading-tight pr-4`}>{selected.title}</h3>
              <button onClick={() => setSelected(null)} className="text-gray-400 hover:text-gray-600 shrink-0 text-lg leading-none">&times;</button>
            </div>
            <span
              className="inline-block px-2.5 py-0.5 rounded-full text-[11px] font-bold text-white mb-3"
              style={{ backgroundColor: categoryColor(selected.category) }}
            >
              {selected.category || "Unknown"}
            </span>
            <p className="text-xs text-gray-400">
              Updated {new Date(selected.created_at).toLocaleString("en-AU")}
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
