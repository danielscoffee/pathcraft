import { describe, expect, it } from 'vitest'
import type { ModeRouteResult, RouteMode } from '../api/types'
import {
  defaultModeOptions,
  modeResultToGeoJSON,
  supportsGeographicMap,
  totalPositionCount,
} from './modeResult'

const result: ModeRouteResult = {
  mode: 'air',
  duration_seconds: 42,
  distance_meters: 9000,
  segments: [
    {
      kind: 'air',
      label: 'Direct flight',
      color: '#7c3aed',
      dashed: true,
      positions: [
        [12.56, 55.67, 100],
        [12.57, 55.68, 1500],
        [12.58, 55.69, 200],
      ],
      duration_seconds: 42,
      distance_meters: 9000,
      meta: { vehicle: 'test-plane' },
    },
  ],
}

const mode: RouteMode = {
  id: 'air',
  label: 'Air',
  icon: 'air',
  color: '#7c3aed',
  crs: 'EPSG:4326',
  dimensions: [2, 3],
  axes: ['longitude', 'latitude', 'altitude_m'],
  options: [{ name: 'departure_time', label: 'Depart', kind: 'time', default: '05:00:00' }],
}

describe('modeResultToGeoJSON', () => {
  it('preserves N-dimensional positions and plugin presentation metadata', () => {
    const geojson = modeResultToGeoJSON(result)
    expect(geojson.features).toHaveLength(1)
    const feature = geojson.features[0]
    expect(feature.geometry.type).toBe('LineString')
    if (feature.geometry.type === 'LineString') {
      expect(feature.geometry.coordinates[1]).toEqual([12.57, 55.68, 1500])
    }
    expect(feature.properties?.color).toBe('#7c3aed')
    expect(feature.properties?.dashed).toBe(true)
    expect(feature.properties?.vehicle).toBe('test-plane')
  })

  it('counts positions across route segments', () => {
    expect(totalPositionCount(result)).toBe(3)
  })
})

describe('mode manifest consumption', () => {
  it('builds option defaults without knowing mode names', () => {
    expect(defaultModeOptions(mode)).toEqual({ departure_time: '05:00:00' })
  })

  it('selects modes compatible with the current geographic map renderer', () => {
    expect(supportsGeographicMap(mode)).toBe(true)
    expect(
      supportsGeographicMap({
        id: 'space',
        label: 'Space',
        crs: 'spacecraft-local',
        dimensions: [3],
      }),
    ).toBe(false)
  })
})
