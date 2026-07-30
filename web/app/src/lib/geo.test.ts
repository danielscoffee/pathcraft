import { describe, expect, it } from 'vitest'
import { formatDistance, formatDuration, haversineMeters, lineLengthMeters, normalizeClockTime } from './geo'

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

describe('normalizeClockTime', () => {
  it('appends seconds to HH:MM input', () => {
    expect(normalizeClockTime('05:00')).toBe('05:00:00')
  })

  it('passes HH:MM:SS through unchanged', () => {
    expect(normalizeClockTime('05:00:30')).toBe('05:00:30')
  })
})

describe('formatDuration', () => {
  it('formats sub-hour durations as minutes', () => {
    expect(formatDuration(720)).toBe('12 min')
  })

  it('rounds and floors to at least one minute', () => {
    expect(formatDuration(20)).toBe('1 min')
  })

  it('formats hour-plus durations with zero-padded minutes', () => {
    expect(formatDuration(3840)).toBe('1 h 04 min')
  })

  it('formats exact hours', () => {
    expect(formatDuration(7200)).toBe('2 h 00 min')
  })
})

describe('formatDistance', () => {
  it('shows meters under one kilometer', () => {
    expect(formatDistance(850.4)).toBe('850 m')
  })

  it('shows one decimal under ten kilometers', () => {
    expect(formatDistance(1328.9)).toBe('1.3 km')
  })

  it('rounds to whole kilometers from ten up', () => {
    expect(formatDistance(12480)).toBe('12 km')
  })
})
