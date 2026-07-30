import { useEffect } from 'react'
import { GeoJSON, MapContainer, Marker, TileLayer, useMap, useMapEvents } from 'react-leaflet'
import L from 'leaflet'
import type { MapConfig, RouteLayerProps, SnapResult, TripDetail } from '../api/types'
import type { SolvedRoute, Status } from '../hooks/useRouting'
import { formatNumber } from '../lib/geo'
import { DEFAULT_ROUTE_COLOR } from '../lib/mapStyle'
import { popupContent } from '../lib/popup'
import { NodesLayer, StopsLayer, StreetGraphLayer, TripLayer } from './overlays'

const markerIcon = (role: 'from' | 'to') =>
  L.divIcon({
    className: 'pc-marker',
    html: `<div class="pc-marker-dot pc-marker-${role}"><span>${role === 'from' ? 'A' : 'B'}</span></div>`,
    iconSize: [22, 22],
    iconAnchor: [11, 11],
  })

const FROM_ICON = markerIcon('from')
const TO_ICON = markerIcon('to')

function ClickHandler({ onClick }: { onClick: (lat: number, lon: number) => void }) {
  useMapEvents({
    click: (e) => onClick(e.latlng.lat, e.latlng.lng),
  })
  return null
}

/** Paper-styled zoom buttons + metric scale, bottom-right (GMaps placement). */
function ZoomScaleControls() {
  const map = useMap()
  useEffect(() => {
    const zoom = L.control.zoom({ position: 'bottomright' }).addTo(map)
    const scale = L.control.scale({ position: 'bottomright', metric: true, imperial: false }).addTo(map)
    return () => {
      zoom.remove()
      scale.remove()
    }
  }, [map])
  return null
}

/** Fit the viewport to the freshly solved route. */
function FitRoute({ route }: { route: SolvedRoute | null }) {
  const map = useMap()
  useEffect(() => {
    if (!route || route.geojson.features.length === 0) return
    try {
      const bounds = L.geoJSON(route.geojson).getBounds()
      if (bounds.isValid()) map.fitBounds(bounds.pad(0.15))
    } catch {
      // Bounds on degenerate geometry are best-effort only.
    }
  }, [map, route])
  return null
}

function routePopup(props: RouteLayerProps): HTMLElement | null {
  const title = props.label || props.mode
  if (!title) return null
  const lines = [
    props.from || props.to ? `${props.from ?? ''} → ${props.to ?? ''}` : '',
    props.distance_meters || props.duration_seconds
      ? `${formatNumber(props.distance_meters ?? 0)} m · ${formatNumber((props.duration_seconds ?? 0) / 60)} min`
      : '',
  ].filter(Boolean)
  return popupContent(title, lines)
}

export interface MapViewProps {
  config: MapConfig
  from: SnapResult | null
  to: SnapResult | null
  route: SolvedRoute | null
  trip: TripDetail | null
  showStreets: boolean
  showNodes: boolean
  showStops: boolean
  onMapClick: (lat: number, lon: number) => void
  onStatus: (status: Status) => void
  onStreetTypes: (types: string[]) => void
}

export default function MapView({
  config,
  from,
  to,
  route,
  trip,
  showStreets,
  showNodes,
  showStops,
  onMapClick,
  onStatus,
  onStreetTypes,
}: MapViewProps) {
  return (
    <MapContainer
      center={[config.center_lat, config.center_lon]}
      zoom={config.zoom}
      className="h-screen w-full"
      zoomControl={false}
    >
      <TileLayer
        url={config.tile_url}
        attribution="&copy; OpenStreetMap contributors"
        maxZoom={19}
      />
      <ClickHandler onClick={onMapClick} />
      <ZoomScaleControls />
      <FitRoute route={route} />

      {from && <Marker position={[from.lat, from.lon]} icon={FROM_ICON} />}
      {to && <Marker position={[to.lat, to.lon]} icon={TO_ICON} />}

      {route && (
        <GeoJSON
          key={`${route.generation}:${route.modeID}`}
          data={route.geojson}
          style={(feature) => {
            const props = (feature?.properties ?? {}) as RouteLayerProps
            return {
              color: props.color || DEFAULT_ROUTE_COLOR,
              weight: 5,
              opacity: 0.92,
              dashArray: props.dashed ? '8 6' : undefined,
            }
          }}
          onEachFeature={(feature, layer) => {
            const html = routePopup((feature.properties ?? {}) as RouteLayerProps)
            if (html) layer.bindPopup(html)
          }}
        />
      )}

      <StreetGraphLayer
        enabled={showStreets}
        chunks={config.graph_chunks}
        onStatus={onStatus}
        onStreetTypes={onStreetTypes}
      />
      <NodesLayer enabled={showNodes} onPick={onMapClick} onStatus={onStatus} />
      <StopsLayer enabled={showStops} onStatus={onStatus} />
      <TripLayer trip={trip} />
    </MapContainer>
  )
}
