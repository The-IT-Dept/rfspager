import { Layers } from "lucide-react";
import { useState, useEffect, useRef, useMemo } from "react";

import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "~/components/ui/sheet";
import type { Route } from "./+types/map";

const API_BASE = process.env.API_URL || "http://localhost:8080";
const heading = "font-[family-name:var(--font-heading)]";

const MAX_LIVE_MARKERS = 300;
// How many located pages the loader asks for; surfaced in the layer note.
const MESSAGE_LIMIT = 200;

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
      fetch(`${API_BASE}/api/public/messages/map?limit=${MESSAGE_LIMIT}`),
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
  onLiveCount: (n: number) => void;
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
  onLiveCount,
}: MapProps) {
  const mapContainer = useRef<HTMLDivElement>(null);
  const mapRef = useRef<any>(null);
  // The SSE handler is registered once; read toggles through a ref so a live
  // marker respects the current state, not the state at mount.
  const filtersRef = useRef(filters);
  filtersRef.current = filters;
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
        if (!filtersRef.current.showMessages) el.style.display = "none";
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
        onLiveCount(liveMarkersRef.current.length);
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
  const [liveCount, setLiveCount] = useState(0);
  const [sheetOpen, setSheetOpen] = useState(false);

  const stationCounts = useMemo(() => {
    const counts: Record<StationCategoryKey, number> = { rfs: 0, frnsw: 0, police: 0, ambulance: 0, ses: 0 };
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
    setSheetOpen(false);
    try {
      const geo = typeof inc.geometry === "string" ? JSON.parse(inc.geometry) : inc.geometry;
      let coords: [number, number] | null = null;
      if (geo?.type === "Point") coords = geo.coordinates;
      else if (geo?.type === "Polygon" && geo.coordinates?.[0]?.[0]) coords = ringCentre(geo.coordinates[0]);
      else if (geo?.type === "MultiPolygon" && geo.coordinates?.[0]?.[0]?.[0]) coords = ringCentre(geo.coordinates[0][0]);
      if (coords) setFlyTo({ lng: coords[0], lat: coords[1], zoom: 10 });
    } catch {}
  };

  const controls = (
    <MapControls
      filters={filters}
      setFilters={setFilters}
      pageCount={messages.length + liveCount}
      liveCount={liveCount}
      incidentCount={incidents.length}
      stationCounts={stationCounts}
    />
  );
  const list = <IncidentList incidents={sortedIncidents} selectedId={selected?.id ?? null} onPick={handleIncidentClick} />;

  return (
    // The map is a canvas, so this page keeps a viewport-fit shell on purpose;
    // the rail scrolls its own controls and list, with min-h-0 so it can.
    <div className="flex h-[calc(100dvh-4rem)] min-h-0 overflow-hidden bg-gray-50">
      <aside className="hidden w-80 min-h-0 shrink-0 flex-col border-r border-gray-200 bg-white md:flex lg:w-96">
        <div className="px-6 pb-4 pt-7">
          <h1 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Emergency map</h1>
          <p className="mt-2 text-sm leading-relaxed text-gray-500">
            Pages that carry a location, major incidents from the RFS feed, and emergency service stations across NSW.
          </p>
        </div>
        <StatusLine connected={connected} className="border-y border-gray-200 px-6 py-3" />
        <div className="min-h-0 flex-1 overflow-y-auto">
          {controls}
          {list}
        </div>
      </aside>

      <div className="relative min-w-0 flex-1">
        <MapComponent
          messages={messages}
          incidents={incidents}
          stations={stations}
          filters={filters}
          flyTo={flyTo}
          onConnectedChange={setConnected}
          onLiveCount={setLiveCount}
        />

        <div className="absolute left-3 right-3 top-3 z-10 flex items-center justify-between gap-2 md:hidden">
          <StatusLine connected={connected} className="rounded-lg border border-gray-200 bg-white/95 px-3 py-2 shadow-sm backdrop-blur" compact />
          <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
            <SheetTrigger className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg border border-gray-200 bg-white/95 px-3 text-sm font-semibold text-gray-800 shadow-sm backdrop-blur outline-none focus-visible:ring-2 focus-visible:ring-[#F7241D]/40">
              <Layers className="size-4" />
              Layers
            </SheetTrigger>
            <SheetContent side="bottom" className="gap-0 rounded-t-2xl p-0">
              <SheetHeader className="border-b border-gray-100 px-5 py-4">
                <SheetTitle className={`${heading} text-base font-bold text-gray-900`}>Emergency map</SheetTitle>
              </SheetHeader>
              <div className="max-h-[70dvh] overflow-y-auto pb-[max(1rem,env(safe-area-inset-bottom))]">
                {controls}
                {list}
              </div>
            </SheetContent>
          </Sheet>
        </div>

        {selected && <IncidentCard incident={selected} onClose={() => setSelected(null)} />}
      </div>
    </div>
  );
}

function ringCentre(ring: number[][]): [number, number] {
  const lng = ring.reduce((s, c) => s + c[0], 0) / ring.length;
  const lat = ring.reduce((s, c) => s + c[1], 0) / ring.length;
  return [lng, lat];
}

function StatusLine({ connected, className = "", compact = false }: { connected: boolean; className?: string; compact?: boolean }) {
  return (
    <div className={`flex items-center gap-2.5 ${className}`}>
      <span className="relative flex h-2.5 w-2.5 shrink-0" aria-hidden>
        {connected && <span className="absolute inline-flex h-full w-full rounded-full bg-[#F7241D] opacity-60 motion-safe:animate-ping" />}
        <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${connected ? "bg-[#F7241D]" : "bg-gray-300"}`} />
      </span>
      <p className="min-w-0 truncate text-sm text-gray-500">
        <span className="font-semibold text-gray-900">{connected ? "Live" : "Connecting"}</span>
        {!compact && <span> {connected ? "New pages with a location drop onto the map as they're decoded." : "Opening the live stream."}</span>}
      </p>
    </div>
  );
}

function MapControls({
  filters,
  setFilters,
  pageCount,
  liveCount,
  incidentCount,
  stationCounts,
}: {
  filters: Filters;
  setFilters: React.Dispatch<React.SetStateAction<Filters>>;
  pageCount: number;
  liveCount: number;
  incidentCount: number;
  stationCounts: Record<StationCategoryKey, number>;
}) {
  const row = "flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm text-gray-800 hover:bg-gray-50";
  const box = "size-4 rounded border-gray-300 accent-[#F7241D]";
  return (
    <div className="border-b border-gray-200 px-3 py-4">
      <p className={`${heading} px-3 text-sm font-bold text-gray-900`}>Layers</p>
      <div className="mt-1 space-y-px">
        <label className={row}>
          <input type="checkbox" className={box} checked={filters.showMessages} onChange={(e) => setFilters((f) => ({ ...f, showMessages: e.target.checked }))} />
          <span className="h-2.5 w-2.5 rounded-full bg-[#F7241D]" aria-hidden />
          <span className="flex-1">Pager messages</span>
          <span className="text-xs tabular-nums text-gray-500">{pageCount.toLocaleString()}</span>
        </label>
        <p className="px-3 pb-1 text-xs leading-relaxed text-gray-500">
          The latest {MESSAGE_LIMIT} pages that carried a location{liveCount > 0 ? `, plus ${liveCount} that arrived while you've been watching` : ", plus anything that arrives while you watch"}.
        </p>
        <label className={row}>
          <input type="checkbox" className={box} checked={filters.showIncidents} onChange={(e) => setFilters((f) => ({ ...f, showIncidents: e.target.checked }))} />
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: CATEGORY_LABELS[1].color }} aria-hidden />
          <span className="flex-1">Major incidents</span>
          <span className="text-xs tabular-nums text-gray-500">{incidentCount.toLocaleString()}</span>
        </label>
      </div>

      <p className={`${heading} mt-5 px-3 text-sm font-bold text-gray-900`}>Stations</p>
      <div className="mt-1 space-y-px">
        {STATION_CATEGORIES.map((cat) => (
          <label key={cat.key} className={row}>
            <input
              type="checkbox"
              className="size-4 rounded border-gray-300"
              style={{ accentColor: cat.color }}
              checked={filters.stations[cat.key]}
              onChange={(e) => setFilters((f) => ({ ...f, stations: { ...f.stations, [cat.key]: e.target.checked } }))}
            />
            <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: cat.color }} aria-hidden />
            <span className="flex-1">{cat.label}</span>
            <span className="text-xs tabular-nums text-gray-500">{stationCounts[cat.key].toLocaleString()}</span>
          </label>
        ))}
      </div>

      <p className={`${heading} mt-5 px-3 text-sm font-bold text-gray-900`}>Incident status</p>
      <ul className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1.5 px-3">
        {CATEGORY_LABELS.map((c) => (
          <li key={c.label} className="flex items-center gap-2 text-sm text-gray-700">
            <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: c.color }} aria-hidden />
            {c.label}
          </li>
        ))}
      </ul>
    </div>
  );
}

function IncidentList({ incidents, selectedId, onPick }: { incidents: Incident[]; selectedId: number | null; onPick: (i: Incident) => void }) {
  return (
    <div className="pb-6">
      <div className="flex items-baseline justify-between px-6 pb-2 pt-4">
        <p className={`${heading} text-sm font-bold text-gray-900`}>Major incidents</p>
        <p className="text-xs tabular-nums text-gray-500">{incidents.length.toLocaleString()}</p>
      </div>
      {incidents.length === 0 ? (
        <p className="px-6 py-10 text-center text-sm text-gray-500">The RFS feed has no major incidents right now.</p>
      ) : (
        <ul>
          {incidents.map((inc) => {
            const location = incidentLocation(inc);
            const active = selectedId === inc.id;
            return (
              <li key={inc.id}>
                <button
                  type="button"
                  onClick={() => onPick(inc)}
                  aria-pressed={active}
                  className={`relative flex w-full items-start gap-3 px-6 py-2.5 text-left outline-none transition-colors hover:bg-gray-50 focus-visible:bg-gray-50 ${active ? "bg-red-50 hover:bg-red-50" : ""}`}
                >
                  {active && <span aria-hidden className="absolute inset-y-0 left-0 w-[3px] bg-[#F7241D]" />}
                  <span className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: categoryColor(inc.category) }} aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-semibold text-gray-900">{inc.title}</span>
                    <span className="block truncate text-xs text-gray-500">
                      {inc.category || "Uncategorised"}
                      {location ? ` near ${location}` : ""}
                    </span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function incidentLocation(inc: Incident): string | null {
  try {
    const d = typeof inc.data === "string" ? JSON.parse(inc.data) : inc.data;
    return typeof d?.location === "string" && d.location.trim() ? d.location.trim() : null;
  } catch {
    return null;
  }
}

function IncidentCard({ incident, onClose }: { incident: Incident; onClose: () => void }) {
  const location = incidentLocation(incident);
  const updated = new Date(incident.created_at).toLocaleString("en-AU", {
    timeZone: "Australia/Sydney",
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  return (
    <div className="absolute bottom-4 left-3 right-3 z-10 rounded-xl border border-gray-200 bg-white p-5 shadow-lg animate-in fade-in-0 slide-in-from-bottom-2 duration-200 motion-reduce:animate-none md:left-auto md:right-4 md:w-[380px]">
      <div className="flex items-start justify-between gap-4">
        <h2 className={`${heading} text-base font-bold leading-snug text-gray-900`}>{incident.title}</h2>
        <button type="button" onClick={onClose} aria-label="Close" className="-mr-1 -mt-1 rounded-md p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700">
          <svg className="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden>
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-gray-600">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: categoryColor(incident.category) }} aria-hidden />
          {incident.category || "Uncategorised"}
        </span>
        {location && <span>{location}</span>}
      </div>
      <p className="mt-2 text-xs text-gray-500" suppressHydrationWarning>
        Updated {updated}
      </p>
    </div>
  );
}
