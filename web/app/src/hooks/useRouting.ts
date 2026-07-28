import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchModeRoute, fetchNearest } from '../api/client'
import type { ModeRouteResult, RouteMode, SnapResult } from '../api/types'
import { normalizeClockTime } from '../lib/geo'
import { modeResultToGeoJSON, totalPositionCount } from '../lib/modeResult'

export type StatusTone = 'info' | 'ok' | 'err'

export interface Status {
  text: string
  tone: StatusTone
}

/** Outcome of solving one registered mode between the current A/B pair. */
export interface ModeResult {
  ok: boolean
  geojson?: GeoJSON.FeatureCollection
  route?: ModeRouteResult
  durationSeconds?: number
  distanceMeters?: number
  nodeCount?: number
  solveMs: number
  error?: string
}

export interface SolvedRoute {
  geojson: GeoJSON.FeatureCollection
  modeID: string
  /** Bumps once per solve round so the map can remount/refit. */
  generation: number
}

const timeOption = (mode: RouteMode) => (mode.options ?? []).find((option) => option.kind === 'time')

async function solveMode(
  mode: RouteMode,
  a: SnapResult,
  b: SnapResult,
  departureTime: string,
): Promise<ModeResult> {
  const started = performance.now()
  try {
    const option = timeOption(mode)
    const options: Record<string, string> = {}
    if (option) {
      options[option.name] = normalizeClockTime(departureTime || option.default || '05:00:00')
    }
    const route = await fetchModeRoute(mode.id, a, b, options)
    return {
      ok: true,
      geojson: modeResultToGeoJSON(route),
      route,
      durationSeconds: route.duration_seconds ?? 0,
      distanceMeters: route.distance_meters ?? 0,
      nodeCount: totalPositionCount(route),
      solveMs: performance.now() - started,
    }
  } catch (err) {
    return {
      ok: false,
      solveMs: performance.now() - started,
      error: err instanceof Error ? err.message : String(err),
    }
  }
}

export function solveModesProgressively(
  modes: RouteMode[],
  solve: (mode: RouteMode) => Promise<ModeResult>,
  publish: (id: string, result: ModeResult) => void,
) {
  return Promise.all(
    modes.map(async (mode) => {
      const result = await solve(mode)
      publish(mode.id, result)
      return [mode.id, result] as const
    }),
  )
}

/** Every complete A/B pair resolves registered modes in parallel. */
export function useRouting(modes: RouteMode[], departureTime: string) {
  const [from, setFrom] = useState<SnapResult | null>(null)
  const [to, setTo] = useState<SnapResult | null>(null)
  const [results, setResults] = useState<Record<string, ModeResult>>({})
  const [solving, setSolving] = useState(false)
  const [generation, setGeneration] = useState(0)
  const [status, setStatus] = useState<Status>({ text: 'Click the map to set A.', tone: 'info' })

  const modesRef = useRef(modes)
  const departureTimeRef = useRef(departureTime)
  useEffect(() => {
    modesRef.current = modes
    departureTimeRef.current = departureTime
  }, [modes, departureTime])

  // Guards against late results from a superseded solve round.
  const round = useRef(0)

  const solveAll = useCallback(async (a: SnapResult, b: SnapResult, onlyTimed = false) => {
    const activeModes = modesRef.current.filter((mode) => !onlyTimed || Boolean(timeOption(mode)))
    if (activeModes.length === 0) return
    const thisRound = ++round.current

    setSolving(true)
    if (!onlyTimed) setResults({})
    setStatus({ text: 'Routing all modes…', tone: 'info' })
    const settled = await solveModesProgressively(
      activeModes,
      (mode) => solveMode(mode, a, b, departureTimeRef.current),
      (id, result) => {
        if (thisRound === round.current) {
          setResults((previous) => ({ ...previous, [id]: result }))
        }
      },
    )
    if (thisRound !== round.current) return

    setGeneration((value) => value + 1)
    setSolving(false)

    const okCount = settled.filter(([, result]) => result.ok).length
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

  const resolveTimed = useCallback(() => {
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
    resolveTimed,
  }
}
