import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchJourney, fetchNearest, fetchRoute } from '../api/client'
import type { Journey, RouteMode, SnapResult } from '../api/types'
import { estimateMinutes, lineLengthMeters, normalizeClockTime } from '../lib/geo'
import { journeyToGeoJSON, totalCoordinateCount } from '../lib/journey'

export type StatusTone = 'info' | 'ok' | 'err'

export interface Status {
  text: string
  tone: StatusTone
}

/** Outcome of solving one mode between the current A/B pair. */
export interface ModeResult {
  ok: boolean
  geojson?: GeoJSON.FeatureCollection
  /** Present for GTFS modes. */
  journey?: Journey
  durationSeconds?: number
  distanceMeters?: number
  nodeCount?: number
  solveMs: number
  error?: string
}

export interface SolvedRoute {
  geojson: GeoJSON.FeatureCollection
  modeID: string
  journey?: Journey
  /** Bumps once per solve round so the map can remount/refit. */
  generation: number
}

async function solveMode(
  mode: RouteMode,
  a: SnapResult,
  b: SnapResult,
  busTime: string,
): Promise<ModeResult> {
  const t0 = performance.now()
  try {
    if (mode.kind === 'gtfs') {
      const time = normalizeClockTime(busTime || '05:00:00')
      const journey = await fetchJourney(mode.endpoint || '/journey', a, b, time)
      const geojson = journeyToGeoJSON(journey)
      return {
        ok: true,
        geojson,
        journey,
        durationSeconds: journey.total_duration_seconds ?? 0,
        distanceMeters: journey.walking_distance_meters,
        nodeCount: totalCoordinateCount(geojson),
        solveMs: performance.now() - t0,
      }
    }
    const geojson = await fetchRoute(mode.endpoint || '/route', mode.id, a, b)
    const first = geojson.features?.[0]
    const coords = first && first.geometry.type === 'LineString' ? first.geometry.coordinates : []
    const distanceMeters = lineLengthMeters(coords)
    return {
      ok: true,
      geojson,
      durationSeconds: estimateMinutes(mode.id, distanceMeters) * 60,
      distanceMeters,
      nodeCount: coords.length,
      solveMs: performance.now() - t0,
    }
  } catch (err) {
    return {
      ok: false,
      solveMs: performance.now() - t0,
      error: err instanceof Error ? err.message : String(err),
    }
  }
}

/**
 * Google-Maps-style directions state: two snapped endpoints, and on every
 * complete pair ALL modes solve in parallel so mode tabs can show ETAs and
 * switching tabs is instant (no refetch).
 */
export function useRouting(modes: RouteMode[], busTime: string) {
  const [from, setFrom] = useState<SnapResult | null>(null)
  const [to, setTo] = useState<SnapResult | null>(null)
  const [results, setResults] = useState<Record<string, ModeResult>>({})
  const [solving, setSolving] = useState(false)
  const [generation, setGeneration] = useState(0)
  const [status, setStatus] = useState<Status>({ text: 'Click the map to set A.', tone: 'info' })

  const modesRef = useRef(modes)
  const busTimeRef = useRef(busTime)
  useEffect(() => {
    modesRef.current = modes
    busTimeRef.current = busTime
  }, [modes, busTime])

  // Guards against late results from a superseded solve round.
  const round = useRef(0)

  const solveAll = useCallback(async (a: SnapResult, b: SnapResult, onlyGTFS = false) => {
    const activeModes = modesRef.current.filter((m) => !onlyGTFS || m.kind === 'gtfs')
    if (activeModes.length === 0) return
    const thisRound = ++round.current

    setSolving(true)
    setStatus({ text: 'Routing all modes…', tone: 'info' })
    const settled = await Promise.all(
      activeModes.map(async (mode) => [mode.id, await solveMode(mode, a, b, busTimeRef.current)] as const),
    )
    if (thisRound !== round.current) return // a newer round superseded this one

    setResults((prev) => {
      const next = onlyGTFS ? { ...prev } : {}
      for (const [id, result] of settled) next[id] = result
      return next
    })
    setGeneration((g) => g + 1)
    setSolving(false)

    const okCount = settled.filter(([, r]) => r.ok).length
    setStatus(
      okCount === 0
        ? { text: `No route found: ${settled[0][1].error ?? 'unknown error'}`, tone: 'err' }
        : { text: `${okCount}/${settled.length} modes solved.`, tone: 'ok' },
    )
  }, [])

  const reset = useCallback((announce = true) => {
    round.current++
    setFrom(null)
    setTo(null)
    setResults({})
    setSolving(false)
    if (announce) setStatus({ text: 'Click the map to set A.', tone: 'info' })
  }, [])

  const clearPoint = useCallback((role: 'from' | 'to') => {
    round.current++
    setResults({})
    setSolving(false)
    if (role === 'from') setFrom(null)
    else setTo(null)
    setStatus({ text: `Click the map to set ${role === 'from' ? 'A' : 'B'}.`, tone: 'info' })
  }, [])

  const swap = useCallback(() => {
    setFrom(to)
    setTo(from)
    if (from && to) void solveAll(to, from)
  }, [from, to, solveAll])

  const placePoint = useCallback(
    async (lat: number, lon: number) => {
      let role: 'from' | 'to' = 'from'
      if (from && !to) role = 'to'
      if (from && to) reset(false)

      setStatus({ text: role === 'from' ? 'Snapping A to the road network…' : 'Snapping B…', tone: 'info' })
      try {
        const snap = await fetchNearest(lat, lon)
        if (role === 'from') {
          setFrom(snap)
          setStatus({ text: 'Now click the destination.', tone: 'info' })
        } else {
          setTo(snap)
          await solveAll(from!, snap)
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        setStatus({ text: `Snap failed: ${message}`, tone: 'err' })
      }
    },
    [from, to, reset, solveAll],
  )

  /** Re-solve GTFS modes when the departure time changes. */
  const resolveGTFS = useCallback(() => {
    if (from && to) void solveAll(from, to, true)
  }, [from, to, solveAll])

  return {
    from,
    to,
    results,
    solving,
    generation,
    status,
    setStatus,
    placePoint,
    clearPoint,
    swap,
    reset,
    resolveGTFS,
  }
}
