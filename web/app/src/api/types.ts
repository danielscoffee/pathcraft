// TypeScript mirrors of the Go HTTP response structs (internal/http/types.go
// and pkg/pathcraft/engine/types.go). Keep field names in sync with the
// `json:` tags on the Go side.

export interface MapConfig {
  center_lat: number
  center_lon: number
  zoom: number
  tile_url: string
}

export interface ModeOption {
  name: string
  label: string
  kind: string
  default?: string
  required?: boolean
}

export interface RouteMode {
  id: string
  label: string
  icon?: string
  color?: string
  crs?: string
  dimensions?: number[]
  axes?: string[]
  options?: ModeOption[]
}

export interface ModeRouteSegment {
  kind?: string
  label?: string
  color?: string
  dashed?: boolean
  positions: number[][]
  distance_meters?: number
  duration_seconds?: number
  meta?: Record<string, unknown>
}

export interface ModeRouteResult {
  mode: string
  duration_seconds?: number
  distance_meters?: number
  segments: ModeRouteSegment[]
  meta?: Record<string, unknown>
}

export interface SnapResult {
  id: number
  distance: number
  lat: number
  lon: number
}

export interface TripStopTime {
  trip_id: string
  route_id: string
  stop_id: string
  stop_name: string
  arrival_time: string
  departure_time: string
  stop_sequence: number
  lat: number
  lon: number
}

export interface TripDetail {
  trip_id: string
  route_id: string
  stop_times: TripStopTime[]
  path_geojson: GeoJSON.FeatureCollection
}

export interface RouteLayerProps {
  mode?: string
  label?: string
  color?: string
  dashed?: boolean
  highway?: string
  name?: string
  sequence?: number
  trip_id?: string
  route_id?: string
  route_name?: string
  route_long_name?: string
  from?: string
  to?: string
  distance_meters?: number
  duration_seconds?: number
  id?: number
  degree?: number
}
