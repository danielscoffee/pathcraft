import type { Journey } from '../api/types'

/** Convert journey legs into a FeatureCollection of LineStrings for the map. */
export function journeyToGeoJSON(journey: Journey): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = []
  for (const [idx, leg] of (journey.legs ?? []).entries()) {
    const coordinates = (leg.coordinates ?? []).map((c) => [c.lon, c.lat])
    if (coordinates.length < 2) continue
    features.push({
      type: 'Feature',
      geometry: { type: 'LineString', coordinates },
      properties: {
        mode: leg.mode || 'walk',
        sequence: idx + 1,
        trip_id: leg.trip_id ?? '',
        route_id: leg.route_id ?? '',
        route_name: leg.route_name ?? '',
        route_long_name: leg.route_long_name ?? '',
        from: leg.from_name || leg.from_stop_id || '',
        to: leg.to_name || leg.to_stop_id || '',
        distance_meters: leg.distance_meters ?? 0,
        duration_seconds: leg.duration_seconds ?? 0,
      },
    })
  }
  return { type: 'FeatureCollection', features }
}

export function totalCoordinateCount(fc: GeoJSON.FeatureCollection): number {
  return fc.features.reduce((n, f) => {
    if (f.geometry.type === 'LineString') return n + f.geometry.coordinates.length
    return n
  }, 0)
}
