import type { ModeRouteResult, RouteMode } from '../api/types'

export function modeResultToGeoJSON(result: ModeRouteResult): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = []
  for (const [index, segment] of (result.segments ?? []).entries()) {
    const positions = (segment.positions ?? []).filter(
      (position) => position.length >= 2 && position.every(Number.isFinite),
    )
    if (positions.length < 2) continue
    features.push({
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: positions },
      properties: {
        ...(segment.meta ?? {}),
        mode: segment.kind ?? result.mode,
        label: segment.label ?? '',
        color: segment.color ?? '',
        dashed: segment.dashed ?? false,
        sequence: index + 1,
        distance_meters: segment.distance_meters ?? 0,
        duration_seconds: segment.duration_seconds ?? 0,
      },
    })
  }
  return { type: 'FeatureCollection', features }
}

export const totalPositionCount = (result: ModeRouteResult): number =>
  (result.segments ?? []).reduce((count, segment) => count + (segment.positions?.length ?? 0), 0)

export const findModeOption = (mode: RouteMode, name: string) =>
  (mode.options ?? []).find((option) => option.name === name)
