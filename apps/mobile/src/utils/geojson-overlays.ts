export interface GeoJsonFeature {
  id?: string | number
  geometry?: {
    type?: string
    coordinates?: unknown
  }
  properties?: any
}

export interface MapCoordinate {
  latitude: number
  longitude: number
}

export type GeoJsonOverlay =
  | {
      id: string | number
      feature: GeoJsonFeature
      type: "point"
      coordinates: MapCoordinate
    }
  | {
      id: string | number
      feature: GeoJsonFeature
      type: "polyline"
      coordinates: MapCoordinate[]
    }
  | {
      id: string | number
      feature: GeoJsonFeature
      type: "polygon"
      coordinates: MapCoordinate[]
      holes?: MapCoordinate[][]
    }

function toCoordinate(value: unknown): MapCoordinate | null {
  if (
    !Array.isArray(value) ||
    value.length < 2 ||
    typeof value[0] !== "number" ||
    typeof value[1] !== "number"
  ) {
    return null
  }

  return { longitude: value[0], latitude: value[1] }
}

function toLine(value: unknown): MapCoordinate[] | null {
  if (!Array.isArray(value)) return null

  const coordinates = value.map(toCoordinate)
  return coordinates.every((coordinate): coordinate is MapCoordinate => coordinate !== null)
    ? coordinates
    : null
}

/**
 * Converts the GeoJSON geometry shapes accepted by react-native-maps into its
 * latitude/longitude overlays without pulling in the abandoned
 * react-native-geojson package and its vulnerable UUID dependency.
 */
export function makeGeoJsonOverlays(features: GeoJsonFeature[]): GeoJsonOverlay[] {
  return features.flatMap((feature, featureIndex) => {
    const geometry = feature.geometry
    if (!geometry) return []

    const id = feature.id ?? `${geometry.type ?? "geometry"}-${featureIndex}`
    switch (geometry.type) {
      case "Point": {
        const coordinate = toCoordinate(geometry.coordinates)
        return coordinate ? [{ id, feature, type: "point" as const, coordinates: coordinate }] : []
      }
      case "MultiPoint":
        return Array.isArray(geometry.coordinates)
          ? geometry.coordinates.flatMap((coordinate, index) => {
              const point = toCoordinate(coordinate)
              return point ? [{ id: `${id}-${index}`, feature, type: "point" as const, coordinates: point }] : []
            })
          : []
      case "LineString": {
        const coordinates = toLine(geometry.coordinates)
        return coordinates ? [{ id, feature, type: "polyline" as const, coordinates }] : []
      }
      case "MultiLineString":
        return Array.isArray(geometry.coordinates)
          ? geometry.coordinates.flatMap((line, index) => {
              const coordinates = toLine(line)
              return coordinates
                ? [{ id: `${id}-${index}`, feature, type: "polyline" as const, coordinates }]
                : []
            })
          : []
      case "Polygon":
        return makePolygonOverlay(feature, id, geometry.coordinates)
      case "MultiPolygon":
        return Array.isArray(geometry.coordinates)
          ? geometry.coordinates.flatMap((polygon, index) => makePolygonOverlay(feature, `${id}-${index}`, polygon))
          : []
      default:
        return []
    }
  })
}

function makePolygonOverlay(
  feature: GeoJsonFeature,
  id: string | number,
  value: unknown,
): GeoJsonOverlay[] {
  if (!Array.isArray(value) || value.length === 0) return []

  const rings = value.map(toLine)
  if (!rings.every((ring): ring is MapCoordinate[] => ring !== null)) return []

  const [coordinates, ...holes] = rings
  return [
    {
      id,
      feature,
      type: "polygon",
      coordinates,
      ...(holes.length > 0 ? { holes } : {}),
    },
  ]
}
