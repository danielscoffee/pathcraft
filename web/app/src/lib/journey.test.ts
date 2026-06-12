import { describe, expect, it } from 'vitest'
import type { Journey } from '../api/types'
import { journeyToGeoJSON, totalCoordinateCount } from './journey'

const journey: Journey = {
  mode: 'multimodal',
  departure_time: '05:00:00',
  arrival_time: '05:20:00',
  total_duration_seconds: 1200,
  legs: [
    {
      mode: 'walk',
      from_name: 'Origin',
      to_name: 'Start Stop',
      distance_meters: 120,
      duration_seconds: 90,
      coordinates: [
        { lat: -8.05428, lon: -34.8813 },
        { lat: -8.0544, lon: -34.881 },
      ],
    },
    {
      mode: 'transit',
      trip_id: 'SHUTTLE_1',
      route_id: 'SHUTTLE',
      from_stop_id: 'START_STOP',
      to_stop_id: 'END_STOP',
      duration_seconds: 600,
      coordinates: [
        { lat: -8.0544, lon: -34.881 },
        { lat: -8.05455, lon: -34.8807 },
        { lat: -8.0548, lon: -34.8803 },
      ],
    },
    {
      // Degenerate leg: fewer than two coordinates must be dropped.
      mode: 'transfer',
      duration_seconds: 60,
      coordinates: [{ lat: -8.0548, lon: -34.8803 }],
    },
  ],
}

describe('journeyToGeoJSON', () => {
  it('converts legs to LineString features in [lon, lat] order', () => {
    const fc = journeyToGeoJSON(journey)
    expect(fc.features).toHaveLength(2)

    const walk = fc.features[0]
    expect(walk.geometry.type).toBe('LineString')
    if (walk.geometry.type === 'LineString') {
      expect(walk.geometry.coordinates[0]).toEqual([-34.8813, -8.05428])
    }
    expect(walk.properties?.mode).toBe('walk')
    expect(walk.properties?.sequence).toBe(1)
  })

  it('carries transit metadata into feature properties', () => {
    const fc = journeyToGeoJSON(journey)
    const transit = fc.features[1]
    expect(transit.properties?.mode).toBe('transit')
    expect(transit.properties?.trip_id).toBe('SHUTTLE_1')
    expect(transit.properties?.from).toBe('START_STOP')
    expect(transit.properties?.to).toBe('END_STOP')
  })

  it('handles journeys without legs', () => {
    const fc = journeyToGeoJSON({ ...journey, legs: [] })
    expect(fc.features).toHaveLength(0)
  })
})

describe('totalCoordinateCount', () => {
  it('counts coordinates across all LineString features', () => {
    expect(totalCoordinateCount(journeyToGeoJSON(journey))).toBe(5)
  })
})
