import type { MapConfig, ModeRouteResult, RouteMode, SnapResult, TripDetail } from './types'

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { signal })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text.trim() || res.statusText)
  }
  return res.json() as Promise<T>
}

export const fetchConfig = () => getJSON<MapConfig>('/config')

export const fetchModes = async (): Promise<RouteMode[]> => {
  const data = await getJSON<{ modes: RouteMode[] }>('/modes')
  if (!Array.isArray(data.modes) || data.modes.length === 0) {
    throw new Error('empty mode catalog')
  }
  return data.modes
}

export const fetchNearest = (lat: number, lon: number) =>
  getJSON<SnapResult>(`/nearest?lat=${lat}&lon=${lon}`)

export const fetchModeRoute = (
  modeID: string,
  from: SnapResult,
  to: SnapResult,
  options: Record<string, string> = {},
) => {
  const query = new URLSearchParams({
    mode: modeID,
    from: `${from.lon},${from.lat}`,
    to: `${to.lon},${to.lat}`,
    ...options,
  })
  return getJSON<ModeRouteResult>(`/mode-route?${query}`)
}

export const fetchStreetGraph = () => getJSON<GeoJSON.FeatureCollection>('/graph')

export const fetchNodes = (bbox: string, limit: number, minDegree: number, signal: AbortSignal) =>
  getJSON<GeoJSON.FeatureCollection>(
    `/nodes?bbox=${bbox}&limit=${limit}&min_degree=${minDegree}`,
    signal,
  )

export const fetchTransitStops = () => getJSON<GeoJSON.FeatureCollection>('/transit/stops')

export const fetchTripIDs = async (): Promise<string[]> => {
  const data = await getJSON<{ trip_ids: string[] | null }>('/transit/trips')
  return data.trip_ids ?? []
}

export const fetchTrip = (tripID: string) =>
  getJSON<TripDetail>(`/transit/trip?trip_id=${encodeURIComponent(tripID)}`)
