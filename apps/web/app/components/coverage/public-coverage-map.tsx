import { useEffect, useRef, useState } from "react";
import type { Feature, FeatureCollection, Point } from "geojson";
import type { GeoJSONSource, Map as MlMap, MapLayerMouseEvent, Marker, Popup } from "maplibre-gl";
// Same workaround as the public map: maplibre's ESM build looks for its worker
// next to its own module, which Vite renames, so hand it the bundled URL.
import maplibreWorkerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";
import { HEAT_RAMP } from "~/lib/coverage";
import {
  HEARD_COLOR,
  NOT_HEARD_COLOR,
  RECEIVER_COLOR,
  type PublicCoverage,
  type PublicCoverageReceiver,
} from "~/lib/public-coverage";

const MAP_STYLE_URL = "https://tiles.openfreemap.org/styles/positron";
const MIN_ZOOM = 3;
const MAX_ZOOM = 13;
const MAX_BOUNDS: [[number, number], [number, number]] = [
  [102, -48],
  [162, -7],
];
const NSW_BOUNDS: [[number, number], [number, number]] = [
  [140.9, -37.6],
  [153.7, -28.1],
];

const SRC_HEAT = "pc-heat";
const SRC_TX = "pc-tx";
const SRC_RX = "pc-rx";
const L_HEAT = "pc-heat";
const L_TX = "pc-tx";
const L_RX_LABEL = "pc-rx-label";

const EMPTY: FeatureCollection = { type: "FeatureCollection", features: [] };

function heatFeatures(data: PublicCoverage | null): FeatureCollection<Point> {
  if (!data || data.heat.length === 0) return EMPTY as FeatureCollection<Point>;
  // Normalise against the 95th percentile so one busy cell doesn't wash out
  // everything else (same as the admin map).
  const weights = data.heat.map((h) => h[2]).sort((a, b) => a - b);
  const p95 = Math.max(1, weights[Math.min(weights.length - 1, Math.floor(weights.length * 0.95))]);
  return {
    type: "FeatureCollection",
    features: data.heat
      .filter(([lng, lat]) => Number.isFinite(lng) && Number.isFinite(lat))
      .map(([lng, lat, w]) => ({
        type: "Feature",
        geometry: { type: "Point", coordinates: [lng, lat] },
        properties: { wn: Math.min(1, w / p95) },
      })),
  };
}

function transmitterFeatures(data: PublicCoverage | null): FeatureCollection<Point> {
  if (!data) return EMPTY as FeatureCollection<Point>;
  const features: Feature<Point>[] = [];
  for (const t of data.transmitters) {
    if (!Number.isFinite(t.lat) || !Number.isFinite(t.lng)) continue;
    features.push({
      type: "Feature",
      geometry: { type: "Point", coordinates: [t.lng, t.lat] },
      properties: { heard: t.heard ? 1 : 0, district: t.district ?? "" },
    });
  }
  return { type: "FeatureCollection", features };
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function receiverLabelFeatures(groups: PublicCoverageReceiver[][]): FeatureCollection<Point> {
  return {
    type: "FeatureCollection",
    features: groups.map((g) => ({
      type: "Feature",
      geometry: { type: "Point", coordinates: [g[0].lng, g[0].lat] },
      properties: { name: g.map((r) => r.name).join(" / ") },
    })),
  };
}

function addLayers(map: MlMap, compact: boolean) {
  map.addSource(SRC_HEAT, { type: "geojson", data: EMPTY });
  map.addSource(SRC_TX, { type: "geojson", data: EMPTY });
  map.addSource(SRC_RX, { type: "geojson", data: EMPTY });

  const heatColor: unknown[] = ["interpolate", ["linear"], ["heatmap-density"]];
  for (const [stop, color] of HEAT_RAMP) heatColor.push(stop, color);

  map.addLayer({
    id: L_HEAT,
    type: "heatmap",
    source: SRC_HEAT,
    paint: {
      "heatmap-weight": ["interpolate", ["linear"], ["get", "wn"], 0, 0.05, 1, 1],
      "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 4, 0.9, 8, 1.4, 12, 2.2],
      "heatmap-radius": compact
        ? ["interpolate", ["linear"], ["zoom"], 4, 3, 7, 7, 10, 14]
        : ["interpolate", ["linear"], ["zoom"], 4, 4, 7, 10, 10, 20, 13, 34],
      "heatmap-color": heatColor as never,
      "heatmap-opacity": 0.8,
    },
  });

  map.addLayer({
    id: L_TX,
    type: "circle",
    source: SRC_TX,
    // Heard sites draw over silent ones.
    layout: { "circle-sort-key": ["get", "heard"] },
    paint: {
      "circle-radius": compact
        ? ["interpolate", ["linear"], ["zoom"], 4, 1.8, 7, 3]
        : ["interpolate", ["linear"], ["zoom"], 4, 2.5, 7, 4, 10, 6, 13, 8],
      "circle-color": ["match", ["get", "heard"], 1, HEARD_COLOR, NOT_HEARD_COLOR] as never,
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": compact ? 0.5 : 1,
      "circle-opacity": ["match", ["get", "heard"], 1, 0.95, 0.75] as never,
    },
  });

  // Receiver names as a symbol layer rather than DOM labels, so maplibre's
  // collision detection keeps neighbouring receivers (Budgewoi, Newcastle,
  // Hornsby) legible at state-wide zoom. Uses the positron style's glyphs;
  // if they fail to load the pins (DOM markers) still show, with a title.
  if (!compact) {
    map.addLayer({
      id: L_RX_LABEL,
      type: "symbol",
      source: SRC_RX,
      layout: {
        "text-field": ["get", "name"],
        "text-font": ["Noto Sans Bold"],
        "text-size": 11.5,
        "text-variable-anchor": ["left", "right", "top", "bottom"],
        "text-radial-offset": 0.9,
        "text-justify": "auto",
        "text-padding": 2,
      },
      paint: {
        "text-color": "#111827",
        "text-halo-color": "rgba(255,255,255,0.95)",
        "text-halo-width": 1.6,
      },
    });
  }
}

/** Receivers sharing a location (e.g. Hornsby and Hornsby 2) get one pin. */
function groupReceivers(receivers: PublicCoverageReceiver[]): PublicCoverageReceiver[][] {
  const groups = new Map<string, PublicCoverageReceiver[]>();
  for (const r of receivers) {
    if (!Number.isFinite(r.lat) || !Number.isFinite(r.lng)) continue;
    const key = `${r.lat.toFixed(2)},${r.lng.toFixed(2)}`;
    const g = groups.get(key);
    if (g) g.push(r);
    else groups.set(key, [r]);
  }
  return [...groups.values()];
}

function receiverMarkerEl(group: PublicCoverageReceiver[], compact: boolean): HTMLDivElement {
  const el = document.createElement("div");
  const pin = document.createElement("div");
  const size = compact ? 10 : 13;
  pin.style.width = `${size}px`;
  pin.style.height = `${size}px`;
  pin.style.transform = "rotate(45deg)";
  pin.style.borderRadius = "3px";
  pin.style.background = RECEIVER_COLOR;
  pin.style.border = `${compact ? 1.5 : 2}px solid #fff`;
  pin.style.boxShadow = "0 1px 4px rgba(0,0,0,0.4)";
  pin.style.flexShrink = "0";
  el.appendChild(pin);

  el.title = `Receiver: ${group.map((r) => r.name).join(" / ")}`;
  return el;
}

function fitToData(map: MlMap, data: PublicCoverage | null, padding: number) {
  const pts: Array<[number, number]> = [];
  for (const t of data?.transmitters ?? []) pts.push([t.lng, t.lat]);
  for (const r of data?.receivers ?? []) pts.push([r.lng, r.lat]);
  const valid = pts.filter(([lng, lat]) => Number.isFinite(lng) && Number.isFinite(lat));
  if (valid.length < 2) {
    map.fitBounds(NSW_BOUNDS, { padding, duration: 0 });
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
    { padding, duration: 0, maxZoom: 9 },
  );
}

/**
 * The public coverage map: incident heat, RFS paging sites (heard / not heard
 * yet) and receiver pins. `compact` is the non-interactive home-page card.
 * Client-only: the maplibre import runs inside an effect, so SSR renders the
 * empty container and the map appears after hydration.
 */
export function PublicCoverageMap({ data, compact = false }: { data: PublicCoverage | null; compact?: boolean }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MlMap | null>(null);
  const mlRef = useRef<typeof import("maplibre-gl") | null>(null);
  const markersRef = useRef<Marker[]>([]);
  const fittedRef = useRef(false);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  // Init once. The cancel flag guards StrictMode's mount -> cleanup -> mount;
  // see the note in routes/public/map.tsx.
  useEffect(() => {
    let cancelled = false;
    let map: MlMap | null = null;
    let resizeObserver: ResizeObserver | null = null;
    let hover: Popup | null = null;

    import("maplibre-gl")
      .then((maplibregl) => {
        if (cancelled || !containerRef.current) return;
        maplibregl.setWorkerUrl(maplibreWorkerUrl);
        try {
          map = new maplibregl.Map({
            container: containerRef.current,
            style: MAP_STYLE_URL,
            bounds: NSW_BOUNDS,
            minZoom: MIN_ZOOM,
            maxZoom: MAX_ZOOM,
            maxBounds: MAX_BOUNDS,
            interactive: !compact,
            attributionControl: { compact: true },
          });
        } catch {
          // No WebGL, most likely. The rest of the page still works.
          setFailed(true);
          return;
        }
        if (!compact) {
          map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");
          map.scrollZoom.disable();
          // Re-enable wheel zoom once the reader has engaged with the map, so
          // scrolling past it on a phone or laptop doesn't hijack the page.
          const m = map;
          m.once("click", () => m.scrollZoom.enable());
          m.once("dragstart", () => m.scrollZoom.enable());
        }
        mapRef.current = map;
        mlRef.current = maplibregl;

        map.on("load", () => {
          if (cancelled || !map) return;
          const m = map;
          addLayers(m, compact);
          // maplibre opens the compact attribution until the first
          // interaction, which never comes on the non-interactive card and
          // covers half a phone-sized map. Start it collapsed; the (i)
          // button still opens it.
          containerRef.current?.querySelector(".maplibregl-ctrl-attrib")?.classList.remove("maplibregl-compact-show");

          if (!compact) {
            m.on("mouseenter", L_TX, () => {
              m.getCanvas().style.cursor = "pointer";
            });
            m.on("mousemove", L_TX, (e: MapLayerMouseEvent) => {
              const f = e.features?.[0];
              if (!f) return;
              const p = f.properties as { heard?: number; district?: string };
              const coords = (f.geometry as Point).coordinates as [number, number];
              if (!hover) hover = new maplibregl.Popup({ closeButton: false, closeOnClick: false, offset: 8, maxWidth: "240px" });
              hover
                .setLngLat(coords)
                .setHTML(
                  `<div style="font:12px/1.35 system-ui,sans-serif"><strong>${escapeHtml(p.district || "RFS paging site")}</strong>` +
                    `<div style="color:${p.heard ? HEARD_COLOR : "#6b7280"}">${p.heard ? "Heard by our receivers" : "Not heard yet"}</div></div>`,
                )
                .addTo(m);
            });
            m.on("mouseleave", L_TX, () => {
              m.getCanvas().style.cursor = "";
              hover?.remove();
            });
          }

          requestAnimationFrame(() => {
            if (cancelled) return;
            m.resize();
            setReady(true);
          });
        });

        resizeObserver = new ResizeObserver(() => map?.resize());
        resizeObserver.observe(containerRef.current);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
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
      fittedRef.current = false;
      setReady(false);
    };
  }, [compact]);

  useEffect(() => {
    const map = mapRef.current;
    const ml = mlRef.current;
    if (!ready || !map || !ml) return;
    (map.getSource(SRC_HEAT) as GeoJSONSource | undefined)?.setData(heatFeatures(data));
    (map.getSource(SRC_TX) as GeoJSONSource | undefined)?.setData(transmitterFeatures(data));
    const groups = groupReceivers(data?.receivers ?? []);
    (map.getSource(SRC_RX) as GeoJSONSource | undefined)?.setData(receiverLabelFeatures(groups));

    markersRef.current.forEach((mk) => mk.remove());
    markersRef.current = [];
    for (const group of groups) {
      const el = receiverMarkerEl(group, compact);
      const marker = new ml.Marker({ element: el, anchor: "center" })
        .setLngLat([group[0].lng, group[0].lat])
        .addTo(map);
      markersRef.current.push(marker);
    }

    // Fit once; the 30/90 toggle keeps the reader's camera.
    if (!fittedRef.current && data) {
      fittedRef.current = true;
      fitToData(map, data, compact ? 16 : 40);
    }
  }, [ready, data, compact]);

  return (
    <div className="relative h-full w-full">
      {/* Explicit size, not `absolute inset-0`: maplibre's `.maplibregl-map`
          sets position: relative and would collapse the container. */}
      <div ref={containerRef} style={{ width: "100%", height: "100%" }} />
      {(failed || !data) && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
          <p className="max-w-xs rounded-lg bg-white/95 px-4 py-3 text-center text-sm text-gray-600 shadow-sm ring-1 ring-gray-200">
            {failed
              ? compact
                ? "The map couldn't load in this browser."
                : "The map couldn't load in this browser. The figures and district list still hold."
              : "Coverage data isn't available right now. Try again in a few minutes."}
          </p>
        </div>
      )}
    </div>
  );
}
