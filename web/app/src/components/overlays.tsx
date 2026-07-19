import { useEffect, useRef } from 'react'
import { useMap } from 'react-leaflet'
import L from 'leaflet'
import { fetchNodes, fetchStreetGraph, fetchTransitStops } from '../api/client'
import type { RouteLayerProps, TripDetail } from '../api/types'
import type { Status } from '../hooks/useRouting'
import { highwayStyle } from '../lib/mapStyle'
import { popupContent } from '../lib/popup'

const MIN_ZOOM_NODES = 16 // below this, too many nodes to draw
const NODES_MAX = 200 // hard cap per fetch
const NODES_MIN_DEGREE = 3 // intersections only

interface OverlayProps {
  enabled: boolean
  onStatus: (status: Status) => void
}

/** Debug overlay: the full routable street graph, colored by highway type. */
export function StreetGraphLayer({
  enabled,
  onStatus,
  onStreetTypes,
}: OverlayProps & { onStreetTypes: (types: string[]) => void }) {
  const map = useMap()
  const cache = useRef<GeoJSON.FeatureCollection | null>(null)
  const layer = useRef<L.GeoJSON | null>(null)

  useEffect(() => {
    if (!enabled) {
      layer.current?.remove()
      layer.current = null
      onStreetTypes([])
      return
    }

    let cancelled = false
    const load = cache.current
      ? Promise.resolve(cache.current)
      : fetchStreetGraph().then((data) => (cache.current = data))

    onStatus({ text: 'Loading street graph…', tone: 'info' })
    load
      .then((data) => {
        if (cancelled) return
        layer.current = L.geoJSON(data, {
          style: (feature) => {
            const style = highwayStyle((feature?.properties as RouteLayerProps)?.highway)
            return { color: style.color, weight: style.weight, opacity: 0.7 }
          },
          onEachFeature: (feature, lyr) => {
            const props = (feature.properties ?? {}) as RouteLayerProps
            const tip = document.createElement('span')
            tip.textContent = `${props.name || '(unnamed)'} — ${props.highway || 'unknown'}`
            lyr.bindTooltip(tip, { sticky: true, direction: 'top' })
          },
        }).addTo(map)

        const seen = new Set<string>()
        for (const f of data.features) {
          const hw = (f.properties as RouteLayerProps)?.highway
          if (hw) seen.add(hw)
        }
        onStreetTypes([...seen])
        onStatus({ text: `Street graph: ${data.features.length} edges shown.`, tone: 'info' })
      })
      .catch((err: Error) => {
        if (!cancelled) onStatus({ text: `Graph load failed: ${err.message}`, tone: 'err' })
      })

    return () => {
      cancelled = true
      layer.current?.remove()
      layer.current = null
    }
  }, [enabled, map, onStatus, onStreetTypes])

  return null
}

/**
 * Debug overlay: OSM intersection nodes inside the viewport (OSRM-style).
 * Refetches on pan/zoom with a debounce; renders nothing below MIN_ZOOM_NODES.
 */
export function NodesLayer({
  enabled,
  onPick,
  onStatus,
}: OverlayProps & { onPick: (lat: number, lon: number) => void }) {
  const map = useMap()
  const layer = useRef<L.GeoJSON | null>(null)
  const abort = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!enabled) {
      layer.current?.remove()
      layer.current = null
      return
    }

    const refresh = () => {
      if (map.getZoom() < MIN_ZOOM_NODES) {
        layer.current?.remove()
        layer.current = null
        onStatus({ text: `Zoom in (≥ ${MIN_ZOOM_NODES}) to render nodes.`, tone: 'info' })
        return
      }
      const b = map.getBounds()
      const bbox = [b.getWest(), b.getSouth(), b.getEast(), b.getNorth()]
        .map((v) => v.toFixed(6))
        .join(',')

      abort.current?.abort()
      abort.current = new AbortController()

      fetchNodes(bbox, NODES_MAX, NODES_MIN_DEGREE, abort.current.signal)
        .then((fc) => {
          layer.current?.remove()
          layer.current = L.geoJSON(fc, {
            pointToLayer: (feature, latlng) => {
              const props = (feature.properties ?? {}) as RouteLayerProps
              const marker = L.circleMarker(latlng, {
                radius: Math.min(10, 4 + (props.degree ?? 0)),
                color: '#9c3415',
                weight: 2,
                fillColor: '#c2451e',
                fillOpacity: 0.85,
              })
              const tip = document.createElement('span')
              tip.textContent = `${props.id} (deg ${props.degree})`
              marker.bindTooltip(tip, { direction: 'top', offset: [0, -8] })
              marker.on('click', () => onPick(latlng.lat, latlng.lng))
              return marker
            },
          }).addTo(map)
          const n = fc.features.length
          onStatus({
            text: `${n} node${n === 1 ? '' : 's'} in view${n === NODES_MAX ? ' (capped)' : ''}.`,
            tone: 'info',
          })
        })
        .catch((err: Error) => {
          if (err.name !== 'AbortError') {
            onStatus({ text: `Nodes fetch failed: ${err.message}`, tone: 'err' })
          }
        })
    }

    let timer: ReturnType<typeof setTimeout> | undefined
    const debounced = () => {
      clearTimeout(timer)
      timer = setTimeout(refresh, 200)
    }

    refresh()
    map.on('moveend zoomend', debounced)
    return () => {
      map.off('moveend zoomend', debounced)
      clearTimeout(timer)
      abort.current?.abort()
      layer.current?.remove()
      layer.current = null
    }
  }, [enabled, map, onPick, onStatus])

  return null
}

/** Debug overlay: GTFS bus stops. */
export function StopsLayer({ enabled, onStatus }: OverlayProps) {
  const map = useMap()
  const cache = useRef<GeoJSON.FeatureCollection | null>(null)
  const layer = useRef<L.GeoJSON | null>(null)

  useEffect(() => {
    if (!enabled) {
      layer.current?.remove()
      layer.current = null
      return
    }

    let cancelled = false
    const load = cache.current
      ? Promise.resolve(cache.current)
      : fetchTransitStops().then((data) => (cache.current = data))

    onStatus({ text: 'Loading bus stops…', tone: 'info' })
    load
      .then((data) => {
        if (cancelled) return
        layer.current = L.geoJSON(data, {
          pointToLayer: (_feature, latlng) =>
            L.marker(latlng, {
              icon: L.divIcon({
                className: 'pc-stop-marker',
                html: '<div class="pc-stop-dot">🚌</div>',
                iconSize: [18, 18],
                iconAnchor: [9, 9],
              }),
            }),
          onEachFeature: (feature, lyr) => {
            const props = (feature.properties ?? {}) as { name?: string; id?: string }
            lyr.bindPopup(popupContent('Bus stop', [props.name ?? '', `ID: ${props.id ?? ''}`]))
          },
        }).addTo(map)
        onStatus({ text: `Bus stops: ${data.features.length} shown.`, tone: 'ok' })
      })
      .catch((err: Error) => {
        if (!cancelled) onStatus({ text: `Bus stops failed: ${err.message}`, tone: 'err' })
      })

    return () => {
      cancelled = true
      layer.current?.remove()
      layer.current = null
    }
  }, [enabled, map, onStatus])

  return null
}

/** Optional GTFS trip overlay: shape polyline plus its scheduled stops. */
export function TripLayer({ trip }: { trip: TripDetail | null }) {
  const map = useMap()

  useEffect(() => {
    if (!trip) return
    const path = L.geoJSON(trip.path_geojson, {
      style: { color: '#1d4ed8', weight: 4, opacity: 0.9 },
    }).addTo(map)
    const stops = L.layerGroup(
      trip.stop_times.map((st) =>
        L.circleMarker([st.lat, st.lon], {
          radius: 6,
          color: '#1e3a8a',
          weight: 2,
          fillColor: '#60a5fa',
          fillOpacity: 0.95,
        }).bindPopup(
          popupContent(st.stop_name, [
            `Stop: ${st.stop_id}`,
            `Seq: ${st.stop_sequence}`,
            `Arr: ${st.arrival_time}`,
            `Dep: ${st.departure_time}`,
          ]),
        ),
      ),
    ).addTo(map)

    const bounds = path.getBounds()
    if (bounds.isValid()) map.fitBounds(bounds.pad(0.2))

    return () => {
      path.remove()
      stops.remove()
    }
  }, [map, trip])

  return null
}
