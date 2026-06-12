import { describe, expect, it } from 'vitest'
import { estimateMinutes, haversineMeters, lineLengthMeters, normalizeClockTime } from './geo'

describe('haversineMeters', () => {
  it('measures Copenhagen Central → Nørreport at roughly 1.3 km', () => {
    const d = haversineMeters(55.6726, 12.5648, 55.6833, 12.5717)
    expect(d).toBeGreaterThan(1200)
    expect(d).toBeLessThan(1500)
  })

  it('returns zero for identical points', () => {
    expect(haversineMeters(-8.054, -34.88, -8.054, -34.88)).toBe(0)
  })
})

describe('lineLengthMeters', () => {
  it('sums segment lengths of a [lon, lat] coordinate array', () => {
    const coords = [
      [-34.8813, -8.05428],
      [-34.881, -8.0544],
      [-34.8807, -8.05455],
    ]
    const total = lineLengthMeters(coords)
    expect(total).toBeGreaterThan(50)
    expect(total).toBeLessThan(120)
  })

  it('returns zero for fewer than two points', () => {
    expect(lineLengthMeters([])).toBe(0)
    expect(lineLengthMeters([[-34.88, -8.05]])).toBe(0)
  })
})

describe('estimateMinutes', () => {
  it('walks 840 m in about 10 minutes', () => {
    expect(estimateMinutes('walk', 840)).toBeCloseTo(10, 0)
  })

  it('falls back to walking speed for unknown modes', () => {
    expect(estimateMinutes('hovercraft', 840)).toBeCloseTo(estimateMinutes('walk', 840))
  })
})

describe('normalizeClockTime', () => {
  it('appends seconds to HH:MM input', () => {
    expect(normalizeClockTime('05:00')).toBe('05:00:00')
  })

  it('passes HH:MM:SS through unchanged', () => {
    expect(normalizeClockTime('05:00:30')).toBe('05:00:30')
  })
})
