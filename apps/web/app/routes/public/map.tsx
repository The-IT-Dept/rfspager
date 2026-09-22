import { useState, useEffect, useRef, useMemo } from "react";
import type { Route } from "./+types/map";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

const MAX_LIVE_MARKERS = 300;

// Free vector style from OpenFreeMap (built on OpenStreetMap data, no token).
// Swap to "bright", "liberty" or "dark" for a different look.
const MAP_STYLE_URL = "https://tiles.openfreemap.org/styles/positron";

// Camera constraints — keep the user inside Australia. Without these, low
// zoom + wheel panning sends you to the arctic and you can't find the data.
const MIN_ZOOM = 3;
const MAX_ZOOM = 18;
const MAX_BOUNDS: [[number, number], [number, number]] = [
  [102, -48], // SW corner — off WA, south of Tasmania (loose to allow full-AUS view)
  [162, -7],  // NE corner — east of Norfolk Island, tip of Cape York
];
// Initial view: NSW-focused bounds we fit on load if there's no data.
const NSW_BOUNDS: [[number, number], [number, number]] = [
  [140.9, -37.6],
  [153.7, -28.1],
];

export async function loader() {
  try {
    const [incRes, msgRes, stationsRes] = await Promise.all([
      fetch(`${API_BASE}/api/public/major-incidents`),
      fetch(`${API_BASE}/api/public/messages/map?limit=200`),
      fetch(`${API_BASE}/api/public/stations`),
    ]);
    return {
      incidents: incRes.ok ? await incRes.json() : [],
      messages: msgRes.ok ? await msgRes.json() : [],
      stations: stationsRes.ok ? await stationsRes.json() : { type: "FeatureCollection", features: [] },
    };
  } catch {
    return {
      incidents: [],
      messages: [],
      stations: { type: "FeatureCollection", features: [] },
    };
  }
}

export function meta() {
  return [
    { title: "Live Emergency Map — RFSPager" },
    {
      name: "description",
      content:
        "Interactive map of NSW emergency incidents, pager messages and major fire events — streaming in real time.",
    },
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
  lat: string | number;
  lng: string | number;
  created_at: string;
  capcode_alias: string | null;
  capcode_colour: string | null;
  agency_name: string | null;
}

const CATEGORY_LABELS: Array<{ label: string; color: string }> = [
  { label: "Emergency", color: "#FF3B30" },
  { label: "Watch & Act", color: "#FF9500" },
  { label: "Advice", color: "#FFCC00" },
  { label: "Under Control", color: "#34C759" },
];

// Emergency station categories. The `key` matches the `category` field served
// by /api/public/stations and the per-layer toggle id.
type StationCategoryKey = "rfs" | "frnsw" | "police" | "ambulance" | "ses";

interface StationCategory {
  key: StationCategoryKey;
  label: string;
  short: string;
  color: string;
  stroke: string;
}

const STATION_CATEGORIES: StationCategory[] = [
  { key: "rfs", label: "NSW RFS", short: "Rural Fire", color: "#D97706", stroke: "#78350F" },
  { key: "frnsw", label: "Fire & Rescue NSW", short: "Urban Fire", color: "#DC2626", stroke: "#7F1D1D" },
  { key: "police", label: "NSW Police", short: "Police", color: "#2563EB", stroke: "#1E3A8A" },
  { key: "ambulance", label: "NSW Ambulance", short: "Ambulance", color: "#16A34A", stroke: "#14532D" },
  { key: "ses", label: "NSW SES", short: "SES", color: "#EA580C", stroke: "#7C2D12" },
];

const STATION_LAYER_IDS: Record<StationCategoryKey, string> = {
  rfs: "stations-rfs",
  frnsw: "stations-frnsw",
  police: "stations-police",
  ambulance: "stations-ambulance",
  ses: "stations-ses",
};

const STATION_SOURCE_ID = "emergency-stations";

interface StationFeature {
  type: "Feature";
  id?: number;
  geometry: { type: "Point"; coordinates: [number, number] };
  properties: {
    id?: number;
    source?: string;
    category: StationCategoryKey | string;
    name: string;
    operator?: string;
    address?: string;
    suburb?: string;
    postcode?: string;
    status?: string;
    revised_at?: string;
  };
}

interface StationCollection {
  type: "FeatureCollection";
  features: StationFeature[];
  meta?: { attribution?: string; count?: number };
}

type StationVisibility = Record<StationCategoryKey, boolean>;

function categoryColor(cat: string | null): string {
  if (!cat) return "#FF3B30";
  const c = cat.toLowerCase();
  if (c.includes("emergency") || c.includes("immediate")) return "#FF3B30";
  if (c.includes("watch") || c.includes("act")) return "#FF9500";
  if (c.includes("advice")) return "#FFCC00";
  if (c.includes("control") || c.includes("contained")) return "#34C759";
  return "#FF3B30";
}

function popupHTMLMessage(msg: MapMessage): string {
  const time = new Date(msg.created_at).toLocaleTimeString("en-AU", {
    hour: "2-digit",
    minute: "2-digit",
  });
  return `
    <div style="font-family:system-ui,sans-serif;min-width:220px;">
      <div style="display:flex;justify-content:space-between;align-items:start;gap:8px;margin-bottom:4px;">
        <strong style="font-size:13px;color:#111;">${escapeHtml(msg.capcode_alias || msg.address)}</strong>
        <span style="font-size:11px;color:#888;white-space:nowrap;">${time}</span>
      </div>
      ${msg.agency_name ? `<div style="font-size:11px;color:#666;margin-bottom:4px;">${escapeHtml(msg.agency_name)}</div>` : ""}
      <div style="font-size:12px;color:#333;font-family:monospace;line-height:1.4;white-space:pre-wrap;">${escapeHtml(msg.message)}</div>
    </div>
  `;
}

function popupHTMLIncident(inc: Incident, color: string): string {
  return `
    <div style="font-family:system-ui,sans-serif;min-width:220px;">
      <strong style="font-size:13px;color:#111;">${escapeHtml(inc.title)}</strong>
      <div style="margin-top:6px;">
        <span style="display:inline-block;padding:2px 8px;border-radius:99px;font-size:11px;font-weight:700;color:white;background:${color};">${escapeHtml(inc.category || "Unknown")}</span>
      </div>
      <div style="font-size:11px;color:#888;margin-top:6px;">Updated ${new Date(inc.created_at).toLocaleString("en-AU")}</div>
    </div>
  `;
}

function incidentCentroid(inc: Incident): [number, number] | null {
  let geo: any = null;
  try {
    geo = typeof inc.geometry === "string" ? JSON.parse(inc.geometry) : inc.geometry;
  } catch {
    return null;
  }
  if (!geo) return null;
  if (geo.type === "Point") return geo.coordinates;
  if (geo.type === "Polygon" && geo.coordinates?.[0]?.[0]) {
    const ring = geo.coordinates[0];
    const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
    const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
    return [lng, lat];
  }
  if (geo.type === "MultiPolygon" && geo.coordinates?.[0]?.[0]?.[0]) {
    const ring = geo.coordinates[0][0];
    const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
    const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
    return [lng, lat];
  }
  return null;
}

function fitToData(map: any, msgs: MapMessage[], incs: Incident[]) {
  let minLng = Infinity, minLat = Infinity, maxLng = -Infinity, maxLat = -Infinity;
  let count = 0;
  const extend = (lng: number, lat: number) => {
    if (isNaN(lng) || isNaN(lat)) return;
    if (lng < minLng) minLng = lng;
    if (lng > maxLng) maxLng = lng;
    if (lat < minLat) minLat = lat;
    if (lat > maxLat) maxLat = lat;
    count++;
  };
  for (const m of msgs) extend(parseFloat(String(m.lng)), parseFloat(String(m.lat)));
  for (const inc of incs) {
    const c = incidentCentroid(inc);
    if (c) extend(c[0], c[1]);
  }
  if (count < 2) {
    map.fitBounds(NSW_BOUNDS, { padding: 40, duration: 0 });
    return;
  }
  map.fitBounds(
    [
      [minLng, minLat],
      [maxLng, maxLat],
    ],
    { padding: 60, duration: 0, maxZoom: 11 },
  );
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

interface Filters {
  showMessages: boolean;
  showIncidents: boolean;
  stations: StationVisibility;
}

interface MapProps {
  messages: MapMessage[];
  incidents: Incident[];
  stations: StationCollection;
  filters: Filters;
  flyTo: { lng: number; lat: number; zoom?: number } | null;
  onConnectedChange: (c: boolean) => void;
}

function stationPopupHTML(p: StationFeature["properties"]): string {
  const meta = STATION_CATEGORIES.find((c) => c.key === p.category);
  const colorBg = meta?.color || "#6b7280";
  const opLine = p.operator
    ? `<div style="font-size:11px;color:#666;margin-bottom:6px;">${escapeHtml(p.operator)}</div>`
    : "";
  const addr = p.address ? escapeHtml(p.address) : "";
  const where =
    [p.suburb, p.postcode].filter(Boolean).map((x) => escapeHtml(String(x))).join(" ");
  const addressBlock =
    addr || where
      ? `<div style="font-size:12px;color:#333;line-height:1.45;margin-top:4px;">${addr}${addr && where ? "<br/>" : ""}${where}</div>`
      : "";
  return `
    <div style="font-family:system-ui,sans-serif;min-width:220px;max-width:280px;">
      <div style="display:flex;align-items:center;gap:6px;margin-bottom:4px;">
        <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${colorBg};"></span>
        <strong style="font-size:13px;color:#111;">${escapeHtml(p.name)}</strong>
      </div>
      ${opLine}
      ${addressBlock}
    </div>
  `;
}

function MapComponent({
  messages,
  incidents,
  stations,
  filters,
  flyTo,
  onConnectedChange,
}: MapProps) {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<any>(null);
  const mbglRef = useRef<any>(null);
  const liveMarkersRef = useRef<any[]>([]);
  const initialMarkersRef = useRef<any[]>([]);
  const incidentMarkersRef = useRef<any[]>([]);
  const incidentSourceIds = useRef<string[]>([]);

  // Init map (run once).
  //
  // CAREFUL: this effect uses a dynamic `import()` inside React 19 StrictMode,
  // which runs every effect mount → cleanup → mount in dev. Without a cancel
  // flag, the cleanup fires *before* the import promise resolves, both promises
  // then construct a Map in the same container, and the second Map ignores its
  // constructor options (default center [0,0] → "top of Greenland" in a tall
  // viewport). Always check `cancelled` after the await, and close over the
  // Map locally so cleanup can `.remove()` whichever instance this run created.
  useEffect(() => {
    let cancelled = false;
    let map: any = null;
    let resizeObserver: ResizeObserver | null = null;

    import("maplibre-gl").then((maplibregl) => {
      if (cancelled || !mapContainer.current) return;

      map = new maplibregl.Map({
        container: mapContainer.current,
        style: MAP_STYLE_URL,
        center: [150.34, -33.85],
        zoom: 5,
        minZoom: MIN_ZOOM,
        maxZoom: MAX_ZOOM,
        maxBounds: MAX_BOUNDS,
        attributionControl: { compact: true },
      });

      map.addControl(
        new maplibregl.NavigationControl({ showCompass: false }),
        "top-right",
      );

      mapRef.current = map;
      mbglRef.current = maplibregl;

      map.on("load", () => {
        if (cancelled || !map) return;
        drawStations(map, maplibregl, stations, filters.stations);
        drawInitialMessages(map, maplibregl, messages);
        drawIncidents(map, maplibregl, incidents);
        requestAnimationFrame(() => {
          if (cancelled || !map) return;
          map.resize();
          fitToData(map, messages, incidents);
        });
      });

      resizeObserver = new ResizeObserver(() => map?.resize());
      resizeObserver.observe(mapContainer.current);
    });

    return () => {
      cancelled = true;
      resizeObserver?.disconnect();
      resizeObserver = null;
      map?.remove();
      map = null;
      mapRef.current = null;
      mbglRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Toggle visibility on filter change
  useEffect(() => {
    const m = mapRef.current;
    if (!m) return;
    const setVis = (markers: any[], show: boolean) => {
      markers.forEach((mk) => {
        const el: HTMLElement = mk.getElement();
        el.style.display = show ? "" : "none";
      });
    };
    // Station layers
    for (const cat of STATION_CATEGORIES) {
      const layerId = STATION_LAYER_IDS[cat.key];
      if (m.getLayer(layerId)) {
        m.setLayoutProperty(
          layerId,
          "visibility",
          filters.stations[cat.key] ? "visible" : "none",
        );
      }
    }
    setVis(initialMarkersRef.current, filters.showMessages);
    setVis(liveMarkersRef.current, filters.showMessages);
    setVis(incidentMarkersRef.current, filters.showIncidents);
    incidentSourceIds.current.forEach((id) => {
      const fill = m.getLayer(`${id}-fill`);
      const line = m.getLayer(`${id}-line`);
      if (fill) m.setLayoutProperty(`${id}-fill`, "visibility", filters.showIncidents ? "visible" : "none");
      if (line) m.setLayoutProperty(`${id}-line`, "visibility", filters.showIncidents ? "visible" : "none");
    });
  }, [filters]);

  // Fly to
  useEffect(() => {
    if (!flyTo || !mapRef.current) return;
    mapRef.current.flyTo({ center: [flyTo.lng, flyTo.lat], zoom: flyTo.zoom ?? 11, duration: 1200 });
  }, [flyTo]);

  // SSE: live messages → live markers
  useEffect(() => {
    const es = new EventSource("/api/public/events");
    es.onopen = () => onConnectedChange(true);
    es.onerror = () => onConnectedChange(false);
    es.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as MapMessage & { lat?: any; lng?: any };
        if (msg.lat == null || msg.lng == null) return;
        const lat = parseFloat(String(msg.lat));
        const lng = parseFloat(String(msg.lng));
        if (isNaN(lat) || isNaN(lng)) return;

        const m = mapRef.current;
        const mbgl = mbglRef.current;
        if (!m || !mbgl) return;

        const el = makeMessageEl(msg.capcode_colour || "#F7241D", true);
        if (!filters.showMessages) el.style.display = "none";
        const marker = new mbgl.Marker(el)
          .setLngLat([lng, lat])
          .setPopup(new mbgl.Popup({ offset: 12, maxWidth: "400px" }).setHTML(popupHTMLMessage(msg)))
          .addTo(m);
        liveMarkersRef.current.push(marker);

        // Cap memory
        while (liveMarkersRef.current.length > MAX_LIVE_MARKERS) {
          const old = liveMarkersRef.current.shift();
          old?.remove();
        }
      } catch {}
    };
    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function makeMessageEl(color: string, pulse = false): HTMLDivElement {
    const el = document.createElement("div");
    el.className = pulse ? "rfs-marker-pulse" : "";
    el.style.width = "14px";
    el.style.height = "14px";
    el.style.borderRadius = "50%";
    el.style.backgroundColor = color;
    el.style.border = "2px solid white";
    el.style.boxShadow = "0 1px 4px rgba(0,0,0,0.35)";
    el.style.cursor = "pointer";
    return el;
  }

  function drawStations(
    map: any,
    mbgl: any,
    collection: StationCollection,
    visibility: StationVisibility,
  ) {
    if (!collection || !collection.features?.length) return;
    if (map.getSource(STATION_SOURCE_ID)) return;

    map.addSource(STATION_SOURCE_ID, {
      type: "geojson",
      data: collection,
    });

    for (const cat of STATION_CATEGORIES) {
      const layerId = STATION_LAYER_IDS[cat.key];
      map.addLayer({
        id: layerId,
        type: "circle",
        source: STATION_SOURCE_ID,
        filter: ["==", ["get", "category"], cat.key],
        layout: { visibility: visibility[cat.key] ? "visible" : "none" },
        paint: {
          "circle-radius": [
            "interpolate",
            ["linear"],
            ["zoom"],
            6, 2.5,
            10, 4,
            14, 6,
          ],
          "circle-color": cat.color,
          "circle-stroke-color": "#ffffff",
          "circle-stroke-width": 1.25,
          "circle-opacity": 0.92,
        },
      });

      // Hover cursor
      map.on("mouseenter", layerId, () => {
        map.getCanvas().style.cursor = "pointer";
      });
      map.on("mouseleave", layerId, () => {
        map.getCanvas().style.cursor = "";
      });

      // Click → popup
      map.on("click", layerId, (e: any) => {
        const f = e.features?.[0];
        if (!f) return;
        new mbgl.Popup({ offset: 10, maxWidth: "320px" })
          .setLngLat(f.geometry.coordinates)
          .setHTML(stationPopupHTML(f.properties))
          .addTo(map);
      });
    }
  }

  function drawInitialMessages(map: any, mbgl: any, msgs: MapMessage[]) {
    initialMarkersRef.current.forEach((m) => m.remove());
    initialMarkersRef.current = [];
    for (const msg of msgs) {
      const lat = parseFloat(String(msg.lat));
      const lng = parseFloat(String(msg.lng));
      if (isNaN(lat) || isNaN(lng)) continue;
      const el = makeMessageEl(msg.capcode_colour || "#F7241D");
      const marker = new mbgl.Marker(el)
        .setLngLat([lng, lat])
        .setPopup(new mbgl.Popup({ offset: 10, maxWidth: "400px" }).setHTML(popupHTMLMessage(msg)))
        .addTo(map);
      initialMarkersRef.current.push(marker);
    }
  }

  function drawIncidents(map: any, mbgl: any, incs: Incident[]) {
    for (const inc of incs) {
      let geo: any = null;
      try {
        geo = typeof inc.geometry === "string" ? JSON.parse(inc.geometry) : inc.geometry;
      } catch {}
      if (!geo) continue;

      let coords: [number, number] | null = null;
      if (geo.type === "Point") {
        coords = geo.coordinates;
      } else if (geo.type === "Polygon" && geo.coordinates?.[0]?.[0]) {
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
      el.style.width = "22px";
      el.style.height = "22px";
      el.style.borderRadius = "50%";
      el.style.backgroundColor = color;
      el.style.border = "3px solid white";
      el.style.boxShadow = "0 2px 6px rgba(0,0,0,0.3)";
      el.style.cursor = "pointer";

      const marker = new mbgl.Marker(el)
        .setLngLat(coords)
        .setPopup(new mbgl.Popup({ offset: 12, maxWidth: "350px" }).setHTML(popupHTMLIncident(inc, color)))
        .addTo(map);
      incidentMarkersRef.current.push(marker);

      if (geo.type === "Polygon" || geo.type === "MultiPolygon") {
        const sourceId = `incident-${inc.id}`;
        try {
          map.addSource(sourceId, {
            type: "geojson",
            data: { type: "Feature", geometry: geo, properties: {} },
          });
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
          incidentSourceIds.current.push(sourceId);
        } catch {}
      }
    }
  }

  return (
    <>
      {/*
        IMPORTANT: do NOT use `absolute inset-0` here. MapLibre adds the
        `maplibregl-map` class to this element, whose stylesheet sets
        `position: relative` — same CSS specificity as `.absolute`, loaded
        after Tailwind, so it wins. The container then has no defined size
        and collapses to 0×0. Use explicit width/height instead.
      */}
      <div
        ref={mapContainer}
        style={{ width: "100%", height: "100%" }}
      />
      <style>{`
        .rfs-marker-pulse::after {
          content: "";
          position: absolute;
          inset: -4px;
          border-radius: 50%;
          background: inherit;
          opacity: 0.5;
          animation: rfs-marker-ping 1.6s cubic-bezier(0,0,0.2,1) infinite;
          pointer-events: none;
        }
        @keyframes rfs-marker-ping {
          0% { transform: scale(1); opacity: 0.55; }
          75%, 100% { transform: scale(2.6); opacity: 0; }
        }
      `}</style>
    </>
  );
}

export default function MapPage({ loaderData }: Route.ComponentProps) {
  const incidents: Incident[] = loaderData?.incidents || [];
  const messages: MapMessage[] = loaderData?.messages || [];
  const stations: StationCollection =
    loaderData?.stations || ({ type: "FeatureCollection", features: [] } as StationCollection);
  const [selected, setSelected] = useState<Incident | null>(null);
  const [filters, setFilters] = useState<Filters>({
    showMessages: true,
    showIncidents: true,
    stations: { rfs: true, frnsw: true, police: false, ambulance: false, ses: false },
  });
  const [flyTo, setFlyTo] = useState<{ lng: number; lat: number; zoom?: number } | null>(null);
  const [connected, setConnected] = useState(false);

  const stationCounts = useMemo(() => {
    const counts: Record<StationCategoryKey, number> = {
      rfs: 0, frnsw: 0, police: 0, ambulance: 0, ses: 0,
    };
    for (const f of stations.features || []) {
      const c = f.properties?.category as StationCategoryKey;
      if (c && c in counts) counts[c]++;
    }
    return counts;
  }, [stations]);

  const sortedIncidents = useMemo(
    () =>
      [...incidents].sort((a, b) => {
        const order = (cat: string | null) => {
          const c = (cat || "").toLowerCase();
          if (c.includes("emergency")) return 0;
          if (c.includes("watch") || c.includes("act")) return 1;
          if (c.includes("advice")) return 2;
          return 3;
        };
        return order(a.category) - order(b.category);
      }),
    [incidents],
  );

  const handleIncidentClick = (inc: Incident) => {
    setSelected(selected?.id === inc.id ? null : inc);
    try {
      const geo = typeof inc.geometry === "string" ? JSON.parse(inc.geometry) : inc.geometry;
      let coords: [number, number] | null = null;
      if (geo?.type === "Point") coords = geo.coordinates;
      else if (geo?.type === "Polygon" && geo.coordinates?.[0]?.[0]) {
        const ring = geo.coordinates[0];
        const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
        const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
        coords = [lng, lat];
      } else if (geo?.type === "MultiPolygon" && geo.coordinates?.[0]?.[0]?.[0]) {
        const ring = geo.coordinates[0][0];
        const lng = ring.reduce((s: number, c: number[]) => s + c[0], 0) / ring.length;
        const lat = ring.reduce((s: number, c: number[]) => s + c[1], 0) / ring.length;
        coords = [lng, lat];
      }
      if (coords) setFlyTo({ lng: coords[0], lat: coords[1], zoom: 10 });
    } catch {}
  };

  return (
    <div className="flex overflow-hidden bg-gray-50 h-[calc(100dvh-4rem)]">
      {/* Sidebar */}
      <aside className="w-80 lg:w-96 bg-white border-r border-gray-200 flex flex-col shrink-0 hidden md:flex">
        {/* Header */}
        <div className="px-5 py-5 border-b border-gray-100">
          <div className="flex items-center gap-2 mb-2">
            <div
              className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold tracking-[0.12em] ${
                connected ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
              }`}
            >
              <span className="relative flex w-1.5 h-1.5">
                {connected && <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />}
                <span className={`relative inline-flex w-1.5 h-1.5 rounded-full ${connected ? "bg-emerald-500" : "bg-gray-400"}`} />
              </span>
              {connected ? "LIVE" : "OFFLINE"}
            </div>
            <span className="text-[10px] text-gray-400 font-bold uppercase tracking-wider">NSW</span>
          </div>
          <h1 className={`${heading} text-xl font-extrabold text-gray-900 leading-tight tracking-tight`}>
            Emergency map
          </h1>
          <p className="text-xs text-gray-400 mt-1">
            {incidents.length} major incidents · {messages.length} messages with coords
          </p>
        </div>

        {/* Layer toggles */}
        <div className="px-5 py-3 border-b border-gray-100 space-y-2">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-1.5">Layers</p>
          {[
            { key: "showMessages" as const, label: "Pager messages", count: messages.length, dot: "#F7241D" },
            { key: "showIncidents" as const, label: "Major incidents", count: incidents.length, dot: "#FF9500" },
          ].map((row) => (
            <label key={row.key} className="flex items-center gap-2.5 cursor-pointer text-sm group">
              <input
                type="checkbox"
                checked={filters[row.key]}
                onChange={(e) => setFilters((f) => ({ ...f, [row.key]: e.target.checked }))}
                className="w-4 h-4 rounded accent-[#F7241D]"
              />
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: row.dot }} />
              <span className="text-gray-700 group-hover:text-gray-900">{row.label}</span>
              <span className="ml-auto text-xs text-gray-400 tabular-nums">{row.count}</span>
            </label>
          ))}
        </div>

        {/* Station layers */}
        <div className="px-5 py-3 border-b border-gray-100 space-y-2">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-1.5">
            Stations
          </p>
          {STATION_CATEGORIES.map((cat) => (
            <label
              key={cat.key}
              className="flex items-center gap-2.5 cursor-pointer text-sm group"
            >
              <input
                type="checkbox"
                checked={filters.stations[cat.key]}
                onChange={(e) =>
                  setFilters((f) => ({
                    ...f,
                    stations: { ...f.stations, [cat.key]: e.target.checked },
                  }))
                }
                className="w-4 h-4 rounded"
                style={{ accentColor: cat.color }}
              />
              <span
                className="w-2 h-2 rounded-full"
                style={{ backgroundColor: cat.color }}
              />
              <span className="text-gray-700 group-hover:text-gray-900">{cat.label}</span>
              <span className="ml-auto text-xs text-gray-400 tabular-nums">
                {stationCounts[cat.key]}
              </span>
            </label>
          ))}
        </div>

        {/* Legend */}
        <div className="px-5 py-3 border-b border-gray-100">
          <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider mb-2">Incident status</p>
          <div className="grid grid-cols-2 gap-1.5">
            {CATEGORY_LABELS.map((c) => (
              <div key={c.label} className="flex items-center gap-1.5 text-[11px] text-gray-600">
                <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: c.color }} />
                {c.label}
              </div>
            ))}
          </div>
        </div>

        {/* Incident list */}
        <div className="flex-1 overflow-y-auto">
          <div className="px-5 py-2.5 sticky top-0 bg-white border-b border-gray-100 z-10">
            <p className="text-[10px] font-bold text-gray-400 uppercase tracking-wider">Active incidents</p>
          </div>
          {sortedIncidents.length === 0 ? (
            <div className="px-5 py-12 text-center text-sm text-gray-400">No active incidents</div>
          ) : (
            sortedIncidents.map((inc) => {
              let d: any = {};
              try {
                d = typeof inc.data === "string" ? JSON.parse(inc.data) : inc.data || {};
              } catch {}
              const active = selected?.id === inc.id;
              return (
                <button
                  key={inc.id}
                  onClick={() => handleIncidentClick(inc)}
                  className={`w-full text-left px-5 py-3 border-b border-gray-50 hover:bg-gray-50 transition-colors relative ${
                    active ? "bg-red-50" : ""
                  }`}
                >
                  {active && (
                    <span className="absolute left-0 top-0 bottom-0 w-0.5 bg-[#F7241D]" />
                  )}
                  <div className="flex items-start gap-2.5">
                    <div
                      className="w-2.5 h-2.5 rounded-full mt-1.5 shrink-0 ring-2 ring-white shadow-sm"
                      style={{ backgroundColor: categoryColor(inc.category) }}
                    />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-semibold text-gray-900 leading-tight truncate">{inc.title}</p>
                      <p className="text-xs text-gray-400 mt-0.5 truncate">
                        {inc.category || "Unknown"}
                        {d?.location ? ` · ${d.location}` : ""}
                      </p>
                    </div>
                  </div>
                </button>
              );
            })
          )}
        </div>
      </aside>

      {/* Map */}
      <div className="flex-1 relative">
        <MapComponent
          messages={messages}
          incidents={incidents}
          stations={stations}
          filters={filters}
          flyTo={flyTo}
          onConnectedChange={setConnected}
        />

        {/* Mobile floating status (no sidebar visible) */}
        <div className="md:hidden absolute top-4 left-4 right-4 z-10 flex items-center gap-2 px-3 py-2 bg-white/95 backdrop-blur-md rounded-xl shadow-lg border border-gray-100">
          <div
            className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold tracking-[0.12em] ${
              connected ? "bg-emerald-50 text-emerald-700" : "bg-gray-100 text-gray-500"
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${connected ? "bg-emerald-500 animate-pulse" : "bg-gray-400"}`} />
            {connected ? "LIVE" : "OFFLINE"}
          </div>
          <span className="text-xs text-gray-500 font-medium">
            {incidents.length} incidents · {messages.length} pages
          </span>
        </div>

        {/* Detail card */}
        {selected && (
          <div className="absolute bottom-4 left-4 right-4 md:left-auto md:right-4 md:w-[380px] bg-white rounded-xl shadow-xl border border-gray-100 p-5 z-10 animate-in slide-in-from-bottom-4 fade-in-0 duration-300">
            <div className="flex items-start justify-between mb-3">
              <h3 className={`${heading} font-bold text-gray-900 leading-tight pr-4`}>{selected.title}</h3>
              <button onClick={() => setSelected(null)} className="text-gray-400 hover:text-gray-600 shrink-0 text-xl leading-none">
                ×
              </button>
            </div>
            <span
              className="inline-block px-2.5 py-0.5 rounded-full text-[11px] font-bold text-white mb-3"
              style={{ backgroundColor: categoryColor(selected.category) }}
            >
              {selected.category || "Unknown"}
            </span>
            <p className="text-xs text-gray-400">Updated {new Date(selected.created_at).toLocaleString("en-AU")}</p>
          </div>
        )}
      </div>
    </div>
  );
}
