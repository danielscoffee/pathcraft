// TypeScript mirrors of the Go HTTP response structs (internal/http/types.go
// and pkg/pathcraft/engine/types.go). Keep field names in sync with the
// `json:` tags on the Go side.

export interface MapConfig {
  center_lat: number
  center_lon: number
  zoom: number
  tile_url: string
}

export interface RouteMode {
  id: string
  label: string
  kind: 'standard' | 'gtfs' | string
  endpoint: string
}

export interface SnapResult {
  id: number
  distance: number
  lat: number
  lon: number
}

export interface Coordinate {
  lat: number
  lon: number
}

export interface JourneyLeg {
  mode: 'walk' | 'transit' | 'transfer' | string
  from_name?: string
  to_name?: string
  from_stop_id?: string
  to_stop_id?: string
  trip_id?: string
  route_id?: string
  route_name?: string
  route_long_name?: string
  distance_meters?: number
  duration_seconds: number
  nodes?: number[]
  coordinates?: Coordinate[]
}

export interface Journey {
  mode: string
  departure_time: string
  arrival_time: string
  total_duration_seconds: number
  transit_duration_seconds?: number
  walking_distance_meters?: number
  origin_stop_id?: string
  destination_stop_id?: string
  transit_path?: JourneyLeg[]
  legs: JourneyLeg[]
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
