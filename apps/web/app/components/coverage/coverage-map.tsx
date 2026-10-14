import { useEffect, useRef, useState } from "react";
import type { Feature, FeatureCollection, LineString, Point } from "geojson";
import type { GeoJSONSource, Map as MlMap, MapLayerMouseEvent, Marker, Popup } from "maplibre-gl";
// Same workaround as the public map: maplibre's ESM build looks for its worker
// next to its own module, which Vite renames, so hand it the bundled URL.
import maplibreWorkerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";
import {
  HEAT_RAMP,
  LINK_COLOR,
  RECEIVER_COLOR,
  STATUS_ORDER,
  STATUS_STYLES,
  type CoverageReceiver,
  type CoverageResponse,
} from "~/lib/coverage";

type MaplibreModule = typeof import("maplibre-gl");

const MAP_STYLE_URL = "https://tiles.openfreemap.org/styles/positron";
const MIN_ZOOM = 3;
const MAX_ZOOM = 16;
const MAX_BOUNDS: [[number, number], [number, number]] = [
  [102, -48],
  [162, -7],
];
const NSW_BOUNDS: [[number, number], [number, number]] = [
  [140.9, -37.6],
  [153.7, -28.1],
];

const SRC_HEAT = "cov-heat";
const SRC_LINKS = "cov-links";
const SRC_TX = "cov-tx";
const L_HEAT = "cov-heat";
const L_HEAT_POINTS = "cov-heat-points";
const L_LINKS_PROBABLE = "cov-links-probable";
const L_LINKS_POSSIBLE = "cov-links-possible";
const L_TX = "cov-tx";
const L_TX_SELECTED = "cov-tx-selected";

export interface CoverageLayers {
  heat: boolean;
  transmitters: boolean;
  receivers: boolean;
  links: boolean;
}

interface Props {
  data: CoverageResponse | null;
  layers: CoverageLayers;
  selectedAgencyId: number | null;
  selectedTransmitterId: number | null;
  /** The map re-fits to the data once each time this changes (e.g. receiver/channel). */
  fitToken: string;
  onTransmitterClick: (id: number) => void;
}

const EMPTY: FeatureCollection = { type: "FeatureCollection", features: [] };

/** emph: 1 = in the selected district, 0 = no selection, -1 = dimmed. */
function emphasis(agencyId: number | null, selected: number | null): number {
  if (selected == null) return 0;
  return agencyId === selected ? 1 : -1;
}

function transmitterFeatures(data: CoverageResponse | null, selectedAgencyId: number | null): FeatureCollection<Point> {
  if (!data) return EMPTY as FeatureCollection<Point>;
  const features: Feature<Point>[] = [];
  for (const t of data.transmitters) {
    if (!Number.isFinite(t.lat) || !Number.isFinite(t.lng)) continue;
    const eirp = t.eirp_w && t.eirp_w > 0 ? t.eirp_w : 50;
    const emph = emphasis(t.agency_id, selectedAgencyId);
    features.push({
      type: "Feature",
      geometry: { type: "Point", coordinates: [t.lng, t.lat] },
      properties: {
        id: t.id,
        name: t.name,
        status: t.status,
        // ~3 px at 1 W, ~6.4 px at 50 W, ~9 px at 1 kW (before zoom scaling).
        r: Math.max(2.5, 3 + 2 * Math.log10(Math.max(1, eirp))),
        emph,
        // Higher draws on top: emphasised, then heard sites over silent ones.
        sort: (emph + 1) * 10 + (STATUS_ORDER.length - STATUS_ORDER.indexOf(t.status)),
      },
    });
  }
  return { type: "FeatureCollection", features };
}

function linkFeatures(data: CoverageResponse | null, selectedAgencyId: number | null): FeatureCollection<LineString> {
  if (!data) return EMPTY as FeatureCollection<LineString>;
  const rx = new Map(data.receivers.map((r) => [r.id, r]));
  const tx = new Map(data.transmitters.map((t) => [t.id, t]));
  const features: Feature<LineString>[] = [];
  for (const l of data.links) {
    const r = rx.get(l.system_id);
    const t = tx.get(l.transmitter_id);
    if (!r || !t) continue;
    features.push({
      type: "Feature",
      geometry: { type: "LineString", coordinates: [[r.lng, r.lat], [t.lng, t.lat]] },
      properties: { kind: l.kind, pages: l.pages, emph: emphasis(l.agency_id, selectedAgencyId) },
    });
  }
  return { type: "FeatureCollection", features };
}

function heatFeatures(data: CoverageResponse | null): FeatureCollection<Point> {
  if (!data || data.heat.length === 0) return EMPTY as FeatureCollection<Point>;
  // Normalise against the 95th percentile so one busy cell doesn't wash out
  // everything else.
  const weights = data.heat.map((h) => h[2]).sort((a, b) => a - b);
  const p95 = Math.max(1, weights[Math.min(weights.length - 1, Math.floor(weights.length * 0.95))]);
  return {
    type: "FeatureCollection",
    features: data.heat.map(([lng, lat, w]) => ({
      type: "Feature",
      geometry: { type: "Point", coordinates: [lng, lat] },
      properties: { w, wn: Math.min(1, w / p95) },
    })),
  };
}

function setVisibility(map: MlMap, layerIds: string[], visible: boolean) {
  for (const id of layerIds) {
    if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", visible ? "visible" : "none");
  }
}

function setSourceData(map: MlMap, id: string, fc: FeatureCollection) {
  const src = map.getSource(id) as GeoJSONSource | undefined;
  src?.setData(fc);
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function addLayers(map: MlMap, layers: CoverageLayers) {
  const vis = (on: boolean) => (on ? "visible" : "none") as "visible" | "none";

  map.addSource(SRC_HEAT, { type: "geojson", data: EMPTY });
  map.addSource(SRC_LINKS, { type: "geojson", data: EMPTY });
  map.addSource(SRC_TX, { type: "geojson", data: EMPTY });

  const heatColor: unknown[] = ["interpolate", ["linear"], ["heatmap-density"]];
  for (const [stop, color] of HEAT_RAMP) heatColor.push(stop, color);

  map.addLayer({
    id: L_HEAT,
    type: "heatmap",
    source: SRC_HEAT,
    layout: { visibility: vis(layers.heat) },
    paint: {
      "heatmap-weight": ["interpolate", ["linear"], ["get", "wn"], 0, 0.05, 1, 1],
      "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 4, 0.9, 8, 1.4, 12, 2.2],
      "heatmap-radius": ["interpolate", ["linear"], ["zoom"], 4, 4, 7, 10, 10, 20, 13, 34],
      "heatmap-color": heatColor as never,
      // Hand over to individual cells once zoomed in far enough to see them.
      "heatmap-opacity": ["interpolate", ["linear"], ["zoom"], 10, 0.85, 13, 0.15],
    },
  });

  map.addLayer({
    id: L_HEAT_POINTS,
    type: "circle",
    source: SRC_HEAT,
    minzoom: 10,
    layout: { visibility: vis(layers.heat) },
    paint: {
      "circle-radius": ["interpolate", ["linear"], ["get", "wn"], 0, 3, 1, 9],
      "circle-color": "#6d28d9",
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": 1,
      "circle-opacity": ["interpolate", ["linear"], ["zoom"], 10, 0, 12, 0.75],
      "circle-stroke-opacity": ["interpolate", ["linear"], ["zoom"], 10, 0, 12, 0.9],
    },
  });

  const linkWidth = ["interpolate", ["linear"], ["get", "pages"], 1, 1, 10, 1.75, 100, 3, 1000, 5] as never;
  const linkOpacity = (base: number) =>
    ["match", ["get", "emph"], -1, 0.12, base] as never;

  map.addLayer({
    id: L_LINKS_POSSIBLE,
    type: "line",
    source: SRC_LINKS,
    filter: ["==", ["get", "kind"], "possible"],
    layout: { visibility: vis(layers.links), "line-cap": "butt" },
    paint: {
      "line-color": LINK_COLOR,
      "line-width": linkWidth,
      "line-opacity": linkOpacity(0.55),
      "line-dasharray": [2, 2],
    },
  });

  map.addLayer({
    id: L_LINKS_PROBABLE,
    type: "line",
    source: SRC_LINKS,
    filter: ["==", ["get", "kind"], "probable"],
    layout: { visibility: vis(layers.links), "line-cap": "round" },
    paint: {
      "line-color": LINK_COLOR,
      "line-width": linkWidth,
      "line-opacity": linkOpacity(0.8),
    },
  });

  const statusMatch = (key: "fill" | "stroke") => {
    const expr: unknown[] = ["match", ["get", "status"]];
    for (const s of STATUS_ORDER) expr.push(s, STATUS_STYLES[s][key]);
    expr.push("#9ca3af");
    return expr as never;
  };

  map.addLayer({
    id: L_TX,
    type: "circle",
    source: SRC_TX,
    layout: { visibility: vis(layers.transmitters), "circle-sort-key": ["get", "sort"] },
    paint: {
      "circle-radius": [
        "interpolate", ["linear"], ["zoom"],
        5, ["*", ["get", "r"], ["match", ["get", "emph"], 1, 0.9, 0.6]],
        9, ["*", ["get", "r"], ["match", ["get", "emph"], 1, 1.4, 1]],
        13, ["*", ["get", "r"], ["match", ["get", "emph"], 1, 2, 1.5]],
      ],
      "circle-color": statusMatch("fill"),
      "circle-stroke-color": ["match", ["get", "emph"], 1, "#111827", statusMatch("stroke")] as never,
      "circle-stroke-width": ["match", ["get", "emph"], 1, 1.75, ["match", ["get", "status"], "unmapped", 1.25, 1]] as never,
      "circle-opacity": ["match", ["get", "emph"], -1, 0.18, ["match", ["get", "status"], "unmapped", 0.7, 0.95]] as never,
      "circle-stroke-opacity": ["match", ["get", "emph"], -1, 0.25, 1] as never,
    },
  });

  map.addLayer({
    id: L_TX_SELECTED,
    type: "circle",
    source: SRC_TX,
    filter: ["==", ["get", "id"], -1],
    layout: { visibility: vis(layers.transmitters) },
    paint: {
      "circle-radius": ["interpolate", ["linear"], ["zoom"], 5, ["+", ["get", "r"], 4], 13, ["+", ["*", ["get", "r"], 2], 6]],
      "circle-color": "rgba(0,0,0,0)",
      "circle-stroke-color": "#111827",
      "circle-stroke-width": 2.5,
    },
  });
}

/** Receivers sharing a location (e.g. Hornsby and Hornsby 2) get one pin. */
function groupReceivers(receivers: CoverageReceiver[]): CoverageReceiver[][] {
  const groups = new Map<string, CoverageReceiver[]>();
  for (const r of receivers) {
    if (!Number.isFinite(r.lat) || !Number.isFinite(r.lng)) continue;
    const key = `${r.lat.toFixed(3)},${r.lng.toFixed(3)}`;
    const g = groups.get(key);
    if (g) g.push(r);
    else groups.set(key, [r]);
  }
  return [...groups.values()];
}

function receiverMarkerEl(group: CoverageReceiver[]): HTMLDivElement {
  const el = document.createElement("div");
  el.style.display = "flex";
  el.style.alignItems = "center";
  el.style.gap = "4px";
  el.style.pointerEvents = "auto";

  const pin = document.createElement("div");
  pin.style.width = "14px";
  pin.style.height = "14px";
  pin.style.transform = "rotate(45deg)";
  pin.style.borderRadius = "3px";
  pin.style.background = RECEIVER_COLOR;
  pin.style.border = "2px solid #fff";
  pin.style.boxShadow = "0 1px 4px rgba(0,0,0,0.4)";
  pin.style.flexShrink = "0";

  const label = document.createElement("div");
  label.textContent = group.map((r) => r.name).join(" / ");
  label.style.font = "600 11px/1.2 system-ui, sans-serif";
  label.style.color = "#111827";
  label.style.background = "rgba(255,255,255,0.88)";
  label.style.padding = "1px 5px";
  label.style.borderRadius = "4px";
  label.style.whiteSpace = "nowrap";
  label.style.boxShadow = "0 1px 2px rgba(0,0,0,0.15)";

  el.title = group
    .map((r) => {
      const reach = r.reach_km != null ? `, reach ${Math.round(r.reach_km)} km` : "";
      return `${r.name}: ${r.pages.toLocaleString()} pages, ${r.districts_heard} districts${reach}`;
    })
    .join("\n");

  el.appendChild(pin);
  el.appendChild(label);
  return el;
}

export function CoverageMap({ data, layers, selectedAgencyId, selectedTransmitterId, fitToken, onTransmitterClick }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MlMap | null>(null);
  const mlRef = useRef<MaplibreModule | null>(null);
  const markersRef = useRef<Marker[]>([]);
  const fittedTokenRef = useRef<string | null>(null);
  const fittedAgencyRef = useRef<number | null>(null);
  const onTxClickRef = useRef(onTransmitterClick);
  onTxClickRef.current = onTransmitterClick;
  const layersRef = useRef(layers);
  layersRef.current = layers;
  const [ready, setReady] = useState(false);

  // Init once, client-side only (the dynamic import never runs during SSR).
  // The cancel flag guards StrictMode's mount -> cleanup -> mount in dev; see
  // the note in routes/public/map.tsx.
  useEffect(() => {
    let cancelled = false;
    let map: MlMap | null = null;
    let resizeObserver: ResizeObserver | null = null;
    let hover: Popup | null = null;

    import("maplibre-gl").then((maplibregl) => {
      if (cancelled || !containerRef.current) return;
      maplibregl.setWorkerUrl(maplibreWorkerUrl);

      map = new maplibregl.Map({
        container: containerRef.current,
        style: MAP_STYLE_URL,
        bounds: NSW_BOUNDS,
        minZoom: MIN_ZOOM,
        maxZoom: MAX_ZOOM,
        maxBounds: MAX_BOUNDS,
        attributionControl: { compact: true },
      });
      map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");
      mapRef.current = map;
      mlRef.current = maplibregl;

      map.on("load", () => {
        if (cancelled || !map) return;
        addLayers(map, layersRef.current);

        const m = map;
        m.on("click", L_TX, (e: MapLayerMouseEvent) => {
          const id = Number(e.features?.[0]?.properties?.id);
          if (Number.isFinite(id)) onTxClickRef.current(id);
        });
        m.on("mouseenter", L_TX, () => {
          m.getCanvas().style.cursor = "pointer";
        });
        m.on("mousemove", L_TX, (e: MapLayerMouseEvent) => {
          const f = e.features?.[0];
          if (!f) return;
          const p = f.properties as { name?: string; status?: keyof typeof STATUS_STYLES };
          const style = p.status ? STATUS_STYLES[p.status] : null;
          const coords = (f.geometry as Point).coordinates as [number, number];
          if (!hover) {
            hover = new maplibregl.Popup({ closeButton: false, closeOnClick: false, offset: 10, maxWidth: "260px" });
          }
          hover
            .setLngLat(coords)
            .setHTML(
              `<div style="font:12px/1.35 system-ui,sans-serif"><strong>${escapeHtml(p.name ?? "")}</strong>` +
                (style ? `<div style="color:#555">${escapeHtml(style.label)}</div>` : "") +
                `</div>`,
            )
            .addTo(m);
        });
        m.on("mouseleave", L_TX, () => {
          m.getCanvas().style.cursor = "";
          hover?.remove();
        });

        requestAnimationFrame(() => {
          if (cancelled) return;
          m.resize();
          setReady(true);
        });
      });

      resizeObserver = new ResizeObserver(() => map?.resize());
      resizeObserver.observe(containerRef.current);
    });

    return () => {
      cancelled = true;
      resizeObserver?.disconnect();
      hover?.remove();
      markersRef.current.forEach((mk) => mk.remove());
      markersRef.current = [];
      map?.remove();
      map = null;
      mapRef.current = null;
      mlRef.current = null;
      fittedTokenRef.current = null;
      fittedAgencyRef.current = null;
      setReady(false);
    };
  }, []);

  // Data + selection -> sources.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    setSourceData(map, SRC_HEAT, heatFeatures(data));
  }, [ready, data]);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    setSourceData(map, SRC_TX, transmitterFeatures(data, selectedAgencyId));
    setSourceData(map, SRC_LINKS, linkFeatures(data, selectedAgencyId));
  }, [ready, data, selectedAgencyId]);

  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    map.setFilter(L_TX_SELECTED, ["==", ["get", "id"], selectedTransmitterId ?? -1]);
  }, [ready, selectedTransmitterId]);

  // Receiver pins (DOM markers: few of them, and no dependence on style glyphs).
  useEffect(() => {
    const map = mapRef.current;
    const ml = mlRef.current;
    if (!ready || !map || !ml) return;
    markersRef.current.forEach((mk) => mk.remove());
    markersRef.current = [];
    for (const group of groupReceivers(data?.receivers ?? [])) {
      const el = receiverMarkerEl(group);
      el.style.display = layersRef.current.receivers ? "flex" : "none";
      // Anchor on the pin (left edge), not the centre of pin + label.
      const marker = new ml.Marker({ element: el, anchor: "left", offset: [-9, 0] })
        .setLngLat([group[0].lng, group[0].lat])
        .addTo(map);
      markersRef.current.push(marker);
    }
  }, [ready, data]);

  // Layer toggles.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map) return;
    setVisibility(map, [L_HEAT, L_HEAT_POINTS], layers.heat);
    setVisibility(map, [L_LINKS_PROBABLE, L_LINKS_POSSIBLE], layers.links);
    setVisibility(map, [L_TX, L_TX_SELECTED], layers.transmitters);
    markersRef.current.forEach((mk) => {
      mk.getElement().style.display = layers.receivers ? "flex" : "none";
    });
  }, [ready, layers]);

  // Fit to receivers + attributed transmitters once per receiver/channel choice.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !data || fittedTokenRef.current === fitToken) return;
    fittedTokenRef.current = fitToken;
    const pts: Array<[number, number]> = data.receivers.map((r) => [r.lng, r.lat]);
    for (const t of data.transmitters) {
      if (t.status === "probable" || t.status === "possible") pts.push([t.lng, t.lat]);
    }
    fitPoints(map, pts, 10);
  }, [ready, data, fitToken]);

  // Selecting a district fits to its transmitters.
  useEffect(() => {
    const map = mapRef.current;
    if (!ready || !map || !data) return;
    if (selectedAgencyId == null) {
      fittedAgencyRef.current = null;
      return;
    }
    if (fittedAgencyRef.current === selectedAgencyId) return;
    fittedAgencyRef.current = selectedAgencyId;
    const pts: Array<[number, number]> = data.transmitters
      .filter((t) => t.agency_id === selectedAgencyId)
      .map((t) => [t.lng, t.lat]);
    if (pts.length > 0) fitPoints(map, pts, 11);
  }, [ready, data, selectedAgencyId]);

  return (
    // Explicit size, not `absolute inset-0`: maplibre's `.maplibregl-map`
    // sets position: relative and would collapse the container.
    <div ref={containerRef} style={{ width: "100%", height: "100%" }} />
  );
}

function fitPoints(map: MlMap, pts: Array<[number, number]>, maxZoom: number) {
  const valid = pts.filter(([lng, lat]) => Number.isFinite(lng) && Number.isFinite(lat));
  if (valid.length === 0) {
    map.fitBounds(NSW_BOUNDS, { padding: 40, duration: 0 });
    return;
  }
  if (valid.length === 1) {
    map.flyTo({ center: valid[0], zoom: Math.min(maxZoom, 10), duration: 800 });
    return;
  }
  let minLng = Infinity, minLat = Infinity, maxLng = -Infinity, maxLat = -Infinity;
  for (const [lng, lat] of valid) {
    minLng = Math.min(minLng, lng);
    maxLng = Math.max(maxLng, lng);
    minLat = Math.min(minLat, lat);
    maxLat = Math.max(maxLat, lat);
  }
  map.fitBounds(
    [
      [minLng, minLat],
      [maxLng, maxLat],
    ],
    { padding: 60, duration: 800, maxZoom },
  );
}
