import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchJourney, fetchNearest, fetchRoute } from '../api/client'
import type { Journey, RouteMode, SnapResult } from '../api/types'
import { estimateMinutes, formatNumber, lineLengthMeters, normalizeClockTime } from '../lib/geo'
import { journeyToGeoJSON, totalCoordinateCount } from '../lib/journey'

export type StatusTone = 'info' | 'ok' | 'err'

export interface Status {
  text: string
  tone: StatusTone
}

export interface RouteStats {
  distance: string
  time: string
  nodes: string
  solve: string
}

export interface SolvedRoute {
  geojson: GeoJSON.FeatureCollection
  modeID: string
  /** Present when the solved mode was a GTFS journey. */
  journey?: Journey
  /** Monotonic counter so the map can remount layers per solve. */
  generation: number
}

const EMPTY_STATS: RouteStats = { distance: '—', time: '—', nodes: '—', solve: '—' }

export function useRouting(mode: RouteMode | undefined, busTime: string) {
  const [from, setFrom] = useState<SnapResult | null>(null)
  const [to, setTo] = useState<SnapResult | null>(null)
  const [route, setRoute] = useState<SolvedRoute | null>(null)
  const [stats, setStats] = useState<RouteStats>(EMPTY_STATS)
  const [status, setStatus] = useState<Status>({ text: 'Ready. Click point A.', tone: 'info' })
  const generation = useRef(0)

  // Refs mirror the latest endpoint inputs so map-click callbacks never
  // capture stale mode/time values.
  const modeRef = useRef(mode)
  const busTimeRef = useRef(busTime)
  useEffect(() => {
    modeRef.current = mode
    busTimeRef.current = busTime
  }, [mode, busTime])

  const reset = useCallback((announce = true) => {
    setFrom(null)
    setTo(null)
    setRoute(null)
    setStats(EMPTY_STATS)
    if (announce) setStatus({ text: 'Ready. Click point A.', tone: 'info' })
  }, [])

  const solve = useCallback(async (a: SnapResult, b: SnapResult) => {
    const activeMode = modeRef.current
    if (!activeMode) {
      setStatus({ text: 'No routing mode available.', tone: 'err' })
      return
    }
    setStatus({ text: `Solving ${activeMode.label}…`, tone: 'info' })
    const t0 = performance.now()
    try {
      if (activeMode.kind === 'gtfs') {
        const time = normalizeClockTime(busTimeRef.current || '05:00:00')
        const journey = await fetchJourney(activeMode.endpoint || '/journey', a, b, time)
        const geojson = journeyToGeoJSON(journey)
        const elapsed = performance.now() - t0
        generation.current += 1
        setRoute({ geojson, modeID: activeMode.id, journey, generation: generation.current })
        setStats({
          distance: journey.walking_distance_meters
            ? `${formatNumber(journey.walking_distance_meters)} m walk`
            : '—',
          time: `${formatNumber((journey.total_duration_seconds ?? 0) / 60)} min`,
          nodes: String(totalCoordinateCount(geojson)),
          solve: `${formatNumber(elapsed)} ms`,
        })
        setStatus({
          text:
            journey.mode === 'multimodal'
              ? 'Bus journey found.'
              : 'No faster bus found; showing direct walk.',
          tone: 'ok',
        })
      } else {
        const geojson = await fetchRoute(activeMode.endpoint || '/route', activeMode.id, a, b)
        const elapsed = performance.now() - t0
        const first = geojson.features?.[0]
        const coords = first && first.geometry.type === 'LineString' ? first.geometry.coordinates : []
        const distance = lineLengthMeters(coords)
        generation.current += 1
        setRoute({ geojson, modeID: activeMode.id, generation: generation.current })
        setStats({
          distance: `${formatNumber(distance)} m`,
          time: `${formatNumber(estimateMinutes(activeMode.id, distance))} min`,
          nodes: String(coords.length),
          solve: `${formatNumber(elapsed)} ms`,
        })
        setStatus({ text: `${activeMode.label} route found.`, tone: 'ok' })
      }
    } catch (err) {
      setRoute(null)
      setStats(EMPTY_STATS)
      const message = err instanceof Error ? err.message : String(err)
      setStatus({ text: `${activeMode.label} failed: ${message}`, tone: 'err' })
    }
  }, [])

  const placePoint = useCallback(
    async (lat: number, lon: number) => {
      // Third click starts a fresh pair, mirroring the OSRM-style UX.
      let role: 'from' | 'to' = 'from'
      if (from && !to) role = 'to'
      if (from && to) reset(false)

      setStatus({ text: role === 'from' ? 'Snapping A…' : 'Snapping B…', tone: 'info' })
      try {
        const snap = await fetchNearest(lat, lon)
        if (role === 'from') {
          setFrom(snap)
          setStatus({ text: 'Click point B.', tone: 'info' })
        } else {
          setTo(snap)
          await solve(from!, snap)
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        setStatus({ text: `Snap failed: ${message}`, tone: 'err' })
      }
    },
    [from, to, reset, solve],
  )

  /** Re-solve in place (mode or departure time changed). */
  const resolve = useCallback(() => {
    if (from && to) void solve(from, to)
    else if (modeRef.current) {
      setStatus({
        text: `Mode: ${modeRef.current.label}. Click point ${from ? 'B' : 'A'}.`,
        tone: 'info',
      })
    }
  }, [from, to, solve])

  return { from, to, route, stats, status, setStatus, placePoint, reset, resolve }
}
