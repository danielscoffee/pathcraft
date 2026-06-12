import type { Journey, MapConfig, RouteMode, SnapResult, TripDetail } from './types'

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

const coordQuery = (from: SnapResult, to: SnapResult) =>
  `from_lat=${from.lat}&from_lon=${from.lon}&to_lat=${to.lat}&to_lon=${to.lon}`

export const fetchRoute = (endpoint: string, modeID: string, from: SnapResult, to: SnapResult) =>
  getJSON<GeoJSON.FeatureCollection>(
    `${endpoint}?${coordQuery(from, to)}&mode=${encodeURIComponent(modeID)}`,
  )

export const fetchJourney = (endpoint: string, from: SnapResult, to: SnapResult, time: string) =>
  getJSON<Journey>(`${endpoint}?${coordQuery(from, to)}&time=${encodeURIComponent(time)}`)

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
